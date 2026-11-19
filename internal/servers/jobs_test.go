package servers_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Hexeption/nebla/internal/actions"
	"github.com/Hexeption/nebla/internal/ent/ipallocation"
	"github.com/Hexeption/nebla/internal/ent/server"
	"github.com/Hexeption/nebla/internal/ext"
	"github.com/Hexeption/nebla/internal/pve"
	"github.com/Hexeption/nebla/internal/pve/fake"
	"github.com/Hexeption/nebla/internal/servers"
	"github.com/Hexeption/nebla/internal/testutil"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/require"
)

func spath(id int64) string { return "/v1/servers/" + sid(id) }

func act(e *testutil.ServerEnv, id int64, op string, b any) *http.Response {
	return e.C.Send(http.MethodPost, spath(id)+"/actions/"+op, b)
}

type actionBody struct {
	Action struct {
		ID        int64  `json:"id"`
		Command   string `json:"command"`
		Status    string `json:"status"`
		Progress  int    `json:"progress"`
		Resources []struct {
			ID   int64  `json:"id"`
			Type string `json:"type"`
		} `json:"resources"`
	} `json:"action"`
}

// startAction posts an action and returns its id.
func startAction(t *testing.T, e *testutil.ServerEnv, id int64, op string, b any) int64 {
	t.Helper()
	out, _ := expect[actionBody](t, act(e, id, op, b), http.StatusCreated)
	return out.Action.ID
}

func waitOK(t *testing.T, e *testutil.ServerEnv, id int64) actions.Action {
	t.Helper()
	a := testutil.WaitAction(t, e.App.Ent, id)
	require.Equal(t, actions.StatusSuccess, a.Status, "%+v", a.Error)
	return a
}

func status(t *testing.T, e *testutil.ServerEnv, id int64) string {
	t.Helper()
	row, err := e.App.Ent.Server.Get(context.Background(), id)
	require.NoError(t, err)
	return string(row.Status)
}

func setStatus(t *testing.T, e *testutil.ServerEnv, id int64, st string) {
	t.Helper()
	require.NoError(t, e.App.Ent.Server.UpdateOneID(id).SetStatus(server.Status(st)).Exec(context.Background()))
}

// settle finishes a queued create by hand: action succeeded, a VMID and status st.
func settle(t *testing.T, e *testutil.ServerEnv, s testutil.CreatedServer, st string) {
	t.Helper()
	require.NoError(t, actions.Succeed(context.Background(), e.App.Ent, s.ActionID))
	require.NoError(t, e.App.Ent.Server.UpdateOneID(s.ServerID).SetVmid(100+int(s.ServerID)).SetStatus(server.Status(st)).Exec(context.Background()))
}

// queuedArgs decodes the River args of a queued action's job.
func queuedArgs[T any](t *testing.T, e *testutil.ServerEnv, actionID int64) T {
	t.Helper()
	a, err := actions.Get(context.Background(), e.App.Ent, e.Project, actionID)
	require.NoError(t, err)
	require.NotNil(t, a.RiverJobID)
	j, err := e.App.Jobs.Client.JobGet(context.Background(), *a.RiverJobID)
	require.NoError(t, err)
	var v T
	require.NoError(t, json.Unmarshal(j.EncodedArgs, &v))
	return v
}

func jobOf[T river.JobArgs](args T, attempt int) *river.Job[T] {
	return &river.Job[T]{JobRow: &rivertype.JobRow{ID: 1, Attempt: attempt, MaxAttempts: servers.MaxAttempts + 2}, Args: args}
}

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func opts(e *testutil.ServerEnv) servers.WorkerOptions {
	return servers.WorkerOptions{SQL: e.App.SQL, Ent: e.App.Ent, Catalog: e.App.Catalog, Logger: quiet(), PollInterval: 10 * time.Millisecond}
}

// builtServer creates a server and runs its create job by hand, leaving it running.
func builtServer(t *testing.T, e *testutil.ServerEnv, w *servers.CreateServerWorker, name string) testutil.CreatedServer {
	t.Helper()
	s := e.CreateServer(name, nil)
	require.NoError(t, w.Work(context.Background(), job(s, 1)))
	require.Equal(t, servers.StatusRunning, status(t, e, s.ServerID))
	return s
}

func vmidOf(t *testing.T, e *testutil.ServerEnv, id int64) int {
	t.Helper()
	row, err := e.App.Ent.Server.Get(context.Background(), id)
	require.NoError(t, err)
	require.NotNil(t, row.Vmid)
	return *row.Vmid
}

func TestPowerOffSetsOff(t *testing.T) {
	ctx := context.Background()
	e := workerEnv(t, nil)
	s := e.CreateServer("web", nil)
	waitOK(t, e, s.ActionID)
	vmid := vmidOf(t, e, s.ServerID)

	a := waitOK(t, e, startAction(t, e, s.ServerID, "poweroff", nil))
	require.Equal(t, "stop_server", a.Command)
	require.Equal(t, servers.StatusOff, status(t, e, s.ServerID))
	vm, _ := e.App.PVE.VM(vmid)
	require.Equal(t, "stopped", vm.Status)

	// Already off: the job sees it in Proxmox and makes no call.
	stops := countCalls(e, fmt.Sprintf("POST /nodes/pve1/qemu/%d/status/stop", vmid))
	waitOK(t, e, startAction(t, e, s.ServerID, "poweroff", nil))
	require.Equal(t, stops, countCalls(e, fmt.Sprintf("POST /nodes/pve1/qemu/%d/status/stop", vmid)))

	waitOK(t, e, startAction(t, e, s.ServerID, "poweron", nil))
	require.Equal(t, servers.StatusRunning, status(t, e, s.ServerID))
	waitOK(t, e, startAction(t, e, s.ServerID, "poweron", nil))

	// The DB says running but Proxmox has it stopped: poweron must really start it.
	e.App.PVE.SetVMStatus(vmid, "stopped")
	starts := countCalls(e, fmt.Sprintf("POST /nodes/pve1/qemu/%d/status/start", vmid))
	waitOK(t, e, startAction(t, e, s.ServerID, "poweron", nil))
	require.Equal(t, starts+1, countCalls(e, fmt.Sprintf("POST /nodes/pve1/qemu/%d/status/start", vmid)))
	vm, _ = e.App.PVE.VM(vmid)
	require.Equal(t, "running", vm.Status)

	waitOK(t, e, startAction(t, e, s.ServerID, "reboot", nil))
	waitOK(t, e, startAction(t, e, s.ServerID, "reset", nil))
	require.Equal(t, servers.StatusRunning, status(t, e, s.ServerID))
	a = waitOK(t, e, startAction(t, e, s.ServerID, "shutdown", nil))
	require.Equal(t, "shutdown_server", a.Command)
	require.Equal(t, servers.StatusOff, status(t, e, s.ServerID))
	vm, _ = e.App.PVE.VM(vmid)
	require.Equal(t, "stopped", vm.Status)

	// Proxmox refusing the op fails the action with the real status kept.
	a = testutil.WaitAction(t, e.App.Ent, startAction(t, e, s.ServerID, "reboot", nil))
	require.Equal(t, actions.StatusError, a.Status)
	require.Equal(t, "pve_error", a.Error.Code)
	require.Contains(t, a.Error.Message, "not running")
	require.Equal(t, servers.StatusOff, status(t, e, s.ServerID))

	setStatus(t, e, s.ServerID, servers.StatusMissing)
	expectErr(t, act(e, s.ServerID, "poweron", nil), http.StatusConflict, "server_missing")

	ro := e.App.TokenClient(e.Admin.CreateToken(e.Project, "read"))
	expectErr(t, ro.Send(http.MethodPost, spath(s.ServerID)+"/actions/poweron", nil), http.StatusForbidden, "forbidden")
	n, err := e.App.Ent.Action.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 10, n)
}

func TestPowerShutdownTimeout(t *testing.T) {
	ctx := context.Background()
	e, cw := directEnv(t)
	s := builtServer(t, e, cw, "web")
	o := opts(e)
	o.PowerWait = 200 * time.Millisecond
	w := servers.NewPowerServerWorker(o)
	id := startAction(t, e, s.ServerID, "shutdown", nil)
	e.App.PVE.SetTaskDelay(5 * time.Second)
	err := w.Work(ctx, jobOf(queuedArgs[servers.PowerServerArgs](t, e, id), 1))
	var cancel *river.JobCancelError
	require.True(t, errors.As(err, &cancel), "%v", err)
	a, err := actions.Get(ctx, e.App.Ent, e.Project, id)
	require.NoError(t, err)
	require.Equal(t, actions.StatusError, a.Status)
	require.Equal(t, "pve_error", a.Error.Code)
	require.Contains(t, a.Error.Message, "did not finish within")
	require.Equal(t, servers.StatusRunning, status(t, e, s.ServerID), "status follows Proxmox")
}

func TestSecondActionLocked(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	s := e.CreateServer("web", nil)
	p := spath(s.ServerID)
	// The create action is still running.
	expectErr(t, act(e, s.ServerID, "poweroff", nil), http.StatusConflict, "locked")
	expectErr(t, e.C.Send(http.MethodPut, p, map[string]any{"name": "x"}), http.StatusConflict, "locked")
	settle(t, e, s, servers.StatusRunning)

	id := startAction(t, e, s.ServerID, "poweroff", nil)
	for _, op := range []string{"poweroff", "poweron", "shutdown", "reboot", "reset"} {
		expectErr(t, act(e, s.ServerID, op, nil), http.StatusConflict, "locked")
	}
	expectErr(t, act(e, s.ServerID, "rebuild", map[string]any{"image": "ubuntu"}), http.StatusConflict, "locked")
	expectErr(t, act(e, s.ServerID, "change_type", map[string]any{"server_type": "small", "upgrade_disk": false}), http.StatusConflict, "locked")
	expectErr(t, e.C.Send(http.MethodPut, p, map[string]any{"labels": map[string]string{}}), http.StatusConflict, "locked")
	expectErr(t, e.C.Send(http.MethodDelete, p, nil), http.StatusConflict, "locked")
	require.NoError(t, actions.Succeed(ctx, e.App.Ent, id))
	expect[serverJSON](t, e.C.Send(http.MethodPut, p, map[string]any{"name": "x"}), http.StatusOK)

	// Two concurrent requests: exactly one gets the lock.
	codes := make([]int, 2)
	var wg sync.WaitGroup
	for i := range 2 {
		wg.Go(func() {
			res := act(e, s.ServerID, "reboot", nil)
			res.Body.Close()
			codes[i] = res.StatusCode
		})
	}
	wg.Wait()
	slices.Sort(codes)
	require.Equal(t, []int{http.StatusCreated, http.StatusConflict}, codes)
}

func TestChangeTypeRequiresStopped(t *testing.T) {
	ctx := context.Background()
	rec := &recorder{}
	e := workerEnv(t, rec)
	e.Admin.CreateServerType("medium", 4, 4096, 40)
	s := e.CreateServer("web", nil)
	waitOK(t, e, s.ActionID)
	ct := map[string]any{"server_type": "medium", "upgrade_disk": true}
	expectErr(t, act(e, s.ServerID, "change_type", ct), http.StatusConflict, "server_not_stopped")
	waitOK(t, e, startAction(t, e, s.ServerID, "poweroff", nil))

	a := waitOK(t, e, startAction(t, e, s.ServerID, "change_type", ct))
	require.Equal(t, "change_server_type", a.Command)
	row, err := e.App.Ent.Server.Get(ctx, s.ServerID)
	require.NoError(t, err)
	require.Equal(t, servers.StatusOff, string(row.Status))
	require.Equal(t, 4, row.Cores)
	require.Equal(t, 4096, row.MemoryMB)
	require.Equal(t, 40, row.DiskGB)
	vm, _ := e.App.PVE.VM(*row.Vmid)
	require.Equal(t, "4", vm.Config["cores"])
	require.Equal(t, "4096", vm.Config["memory"])
	require.Contains(t, vm.Config["scsi0"], "size=40G")
	got, _ := expect[serverJSON](t, e.C.Get(spath(s.ServerID)), http.StatusOK)
	require.Equal(t, "medium", got.ServerType.Name)
	require.Equal(t, 40, got.DiskGB)

	require.Eventually(t, func() bool {
		return slices.ContainsFunc(rec.all(), func(ev ext.Event) bool { return ev.Kind == ext.ServerTypeChanged })
	}, testWait, testTick)
	i := slices.IndexFunc(rec.all(), func(ev ext.Event) bool { return ev.Kind == ext.ServerTypeChanged })
	ev := rec.all()[i]
	require.Equal(t, s.ServerID, ev.ServerID)
	require.Equal(t, "medium", ev.Data["server_type"])
	require.Equal(t, 4, ev.Data["cores"])
}

func TestChangeTypeNoShrink(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.Admin.CreateServerType("tiny", 1, 1024, 10)
	s := e.CreateServer("web", nil)
	settle(t, e, s, servers.StatusOff)
	expectErr(t, act(e, s.ServerID, "change_type", map[string]any{"server_type": "tiny", "upgrade_disk": true}),
		http.StatusUnprocessableEntity, "disk_shrink_not_allowed")
	expectErr(t, act(e, s.ServerID, "change_type", map[string]any{"server_type": "nope", "upgrade_disk": false}),
		http.StatusNotFound, "not_found")

	id := startAction(t, e, s.ServerID, "change_type", map[string]any{"server_type": "tiny", "upgrade_disk": false})
	args := queuedArgs[servers.ChangeTypeArgs](t, e, id)
	require.False(t, args.UpgradeDisk)
	row, err := e.App.Ent.Server.Get(ctx, s.ServerID)
	require.NoError(t, err)
	require.Equal(t, 1, args.To.Cores)
	require.Equal(t, 20, args.To.DiskGB, "the disk keeps its size")
	require.Equal(t, 1024, args.To.MemoryMB)
	// A downsize holds the larger spec until it succeeds.
	require.Equal(t, e.Type, row.ServerTypeID)
	require.Equal(t, []int{2, 2048, 20}, []int{row.Cores, row.MemoryMB, row.DiskGB})
}

func TestChangeTypeFailureReverts(t *testing.T) {
	ctx := context.Background()
	e, cw := directEnv(t)
	e.Admin.CreateServerType("medium", 4, 4096, 40)
	s := builtServer(t, e, cw, "web")
	vmid := vmidOf(t, e, s.ServerID)
	e.App.PVE.SetVMStatus(vmid, "stopped")
	setStatus(t, e, s.ServerID, servers.StatusOff)
	id := startAction(t, e, s.ServerID, "change_type", map[string]any{"server_type": "medium", "upgrade_disk": true})
	e.App.PVE.FailNext("/resize", http.StatusBadRequest)
	w := servers.NewChangeTypeWorker(opts(e))
	var cancel *river.JobCancelError
	require.True(t, errors.As(w.Work(ctx, jobOf(queuedArgs[servers.ChangeTypeArgs](t, e, id), 1)), &cancel))
	a, err := actions.Get(ctx, e.App.Ent, e.Project, id)
	require.NoError(t, err)
	require.Equal(t, actions.StatusError, a.Status)
	require.Equal(t, "pve_error", a.Error.Code)
	row, err := e.App.Ent.Server.Get(ctx, s.ServerID)
	require.NoError(t, err)
	require.Equal(t, e.Type, row.ServerTypeID)
	require.Equal(t, []int{2, 2048, 20}, []int{row.Cores, row.MemoryMB, row.DiskGB}, "snapshot reverted")
	vm, _ := e.App.PVE.VM(vmid)
	require.Equal(t, "2", vm.Config["cores"], "proxmox config reverted")
	require.Equal(t, "2048", vm.Config["memory"])
}

// TestChangeTypeQuotaRace: the reserved delta and a create race for the last vCPUs.
func TestChangeTypeQuotaRace(t *testing.T) {
	ctx := context.Background()
	for range 3 {
		e := newEnv(t)
		e.Admin.CreateServerType("big", 3, 2048, 20)
		s := e.CreateServer("web", nil)
		settle(t, e, s, servers.StatusOff)
		// 2 + 2 fits; 3 + 2 does not.
		e.Admin.SetQuota(e.Project, 0, 4, 0, 0, 0)
		start := make(chan struct{})
		codes := make([]int, 2)
		var wg sync.WaitGroup
		wg.Go(func() {
			<-start
			res := act(e, s.ServerID, "change_type", map[string]any{"server_type": "big", "upgrade_disk": false})
			res.Body.Close()
			codes[0] = res.StatusCode
		})
		wg.Go(func() {
			<-start
			res := post(e, map[string]any{"name": "other"})
			res.Body.Close()
			codes[1] = res.StatusCode
		})
		close(start)
		wg.Wait()
		require.Contains(t, [][]int{{http.StatusCreated, http.StatusUnprocessableEntity}, {http.StatusUnprocessableEntity, http.StatusCreated}}, codes)
		u, err := servers.Usage(ctx, e.App.Ent, e.Project)
		require.NoError(t, err)
		require.LessOrEqual(t, u.VCPU, 4)
	}

	// In order: the create takes the slot, so the change's +1 vCPU is refused.
	e := newEnv(t)
	e.Admin.CreateServerType("big", 3, 2048, 20)
	s := e.CreateServer("web", nil)
	settle(t, e, s, servers.StatusOff)
	e.Admin.SetQuota(e.Project, 0, 4, 0, 0, 0)
	e.CreateServer("other", nil)
	expectErr(t, act(e, s.ServerID, "change_type", map[string]any{"server_type": "big", "upgrade_disk": false}),
		http.StatusUnprocessableEntity, "quota_exceeded")

	// A pending downsize frees nothing: a create cannot take the vCPU it would release.
	e = newEnv(t)
	e.Admin.CreateServerType("big", 3, 2048, 20)
	e.Admin.CreateServerType("one", 1, 1024, 20)
	s = e.CreateServer("web", map[string]any{"server_type": "big"})
	settle(t, e, s, servers.StatusOff)
	e.Admin.SetQuota(e.Project, 0, 4, 0, 0, 0)
	id := startAction(t, e, s.ServerID, "change_type", map[string]any{"server_type": "one", "upgrade_disk": false})
	expectErr(t, post(e, map[string]any{"name": "other"}), http.StatusUnprocessableEntity, "quota_exceeded")
	// The downsize fails (the queued row has no VM in Proxmox) and the old spec stays.
	w := servers.NewChangeTypeWorker(opts(e))
	var cancel *river.JobCancelError
	require.True(t, errors.As(w.Work(ctx, jobOf(queuedArgs[servers.ChangeTypeArgs](t, e, id), 1)), &cancel))
	u, err := servers.Usage(ctx, e.App.Ent, e.Project)
	require.NoError(t, err)
	require.Equal(t, 3, u.VCPU)
	require.LessOrEqual(t, u.VCPU, 4)
	row, err := e.App.Ent.Server.Get(ctx, s.ServerID)
	require.NoError(t, err)
	require.Equal(t, 3, row.Cores)
}

func TestRebuildKeepsIPAndMAC(t *testing.T) {
	ctx := context.Background()
	rec := &recorder{}
	e := workerEnv(t, rec)
	e.App.PVE.AddTemplate("pve1", 9001, "ceph")
	debian := e.Admin.CreateImage("debian", e.Location, 9001)
	s := e.CreateServer("web", nil)
	waitOK(t, e, s.ActionID)
	before, err := e.App.Ent.Server.Get(ctx, s.ServerID)
	require.NoError(t, err)
	oldVM, _ := e.App.PVE.VM(*before.Vmid)

	a := waitOK(t, e, startAction(t, e, s.ServerID, "rebuild", map[string]any{"image": "debian"}))
	require.Equal(t, "rebuild_server", a.Command)
	after, err := e.App.Ent.Server.Get(ctx, s.ServerID)
	require.NoError(t, err)
	require.NotEqual(t, *before.Vmid, *after.Vmid, "a new VMID")
	_, ok := e.App.PVE.VM(*before.Vmid)
	require.False(t, ok, "old VM destroyed")
	vm, ok := e.App.PVE.VM(*after.Vmid)
	require.True(t, ok)
	require.Equal(t, oldVM.Config["net0"], vm.Config["net0"], "same MAC and bridge")
	require.Contains(t, vm.Config["net0"], before.MAC)
	require.Equal(t, oldVM.IPSets, vm.IPSets)
	require.Equal(t, []string{"10.0.0.10"}, vm.IPSets["ipfilter-net0"])
	require.Equal(t, "web", vm.Name)
	require.Equal(t, "running", vm.Status, "it was running before")
	require.True(t, hasTag(vm.Tags, "nebla") && hasTag(vm.Tags, tag(s.ServerID)), vm.Tags)
	require.Equal(t, servers.StatusRunning, string(after.Status))
	require.Equal(t, debian, after.ImageID)
	require.Equal(t, 9001, after.TemplateVmid)
	require.Equal(t, 2, after.IsoGeneration)
	require.Equal(t, "local:iso/nb-srv-"+sid(s.ServerID)+"-2.iso", *after.IsoVolid)
	require.Equal(t, *after.IsoVolid+",media=cdrom", vm.Config["ide2"])
	_, ok = e.App.PVE.Volume(isoVolid(s.ServerID))
	require.False(t, ok, "old ISO deleted")
	_, ok = e.App.PVE.Volume(*after.IsoVolid)
	require.True(t, ok)
	require.Nil(t, before.CloneUpid, "clone proofs are spent once the VM is tagged")
	require.Nil(t, before.CloneNonce)
	require.Nil(t, after.CloneUpid)
	require.Nil(t, after.CloneNonce)
	require.Contains(t, vm.Config["description"], "nebla "+tag(s.ServerID))
	require.Nil(t, after.RebuildVmid)
	require.Nil(t, after.RebuildCloneUpid)
	require.Nil(t, after.RebuildCloneNonce)
	require.Len(t, taggedVMs(t, e, s.ServerID), 1)
	n, err := e.App.Ent.IPAllocation.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	got, _ := expect[serverJSON](t, e.C.Get(spath(s.ServerID)), http.StatusOK)
	require.Equal(t, "10.0.0.10", got.PublicNet.IPv4.IP)
	require.Equal(t, "debian", got.Image.Name)
	require.Len(t, rec.all(), 1, "rebuild publishes nothing")

	// An off server stays off.
	waitOK(t, e, startAction(t, e, s.ServerID, "poweroff", nil))
	waitOK(t, e, startAction(t, e, s.ServerID, "rebuild", map[string]any{"image": "ubuntu"}))
	require.Equal(t, servers.StatusOff, status(t, e, s.ServerID))
	vm, _ = e.App.PVE.VM(vmidOf(t, e, s.ServerID))
	require.Equal(t, "stopped", vm.Status)
	require.Equal(t, 1, vmCount(t, e))
	expectErr(t, act(e, s.ServerID, "rebuild", map[string]any{"image": "nope"}), http.StatusNotFound, "not_found")
}

func rebuildEnv(t *testing.T) (*testutil.ServerEnv, testutil.CreatedServer, *servers.RebuildServerWorker) {
	t.Helper()
	e, cw := directEnv(t)
	e.App.PVE.AddTemplate("pve1", 9001, "ceph")
	e.Admin.CreateImage("debian", e.Location, 9001)
	s := builtServer(t, e, cw, "web")
	return e, s, servers.NewRebuildServerWorker(opts(e))
}

func TestRebuildResumesAfterCrash(t *testing.T) {
	ctx := context.Background()
	e, s, w := rebuildEnv(t)
	old := vmidOf(t, e, s.ServerID)
	id := startAction(t, e, s.ServerID, "rebuild", map[string]any{"image": "debian"})
	args := queuedArgs[servers.RebuildServerArgs](t, e, id)
	crashCtx, crash := context.WithCancel(ctx)
	servers.SetRebuildAfterStep(w, func(n int) {
		if n == servers.StepClone {
			crash()
		}
	})
	require.Error(t, w.Work(crashCtx, jobOf(args, 1)))
	row, err := e.App.Ent.Server.Get(ctx, s.ServerID)
	require.NoError(t, err)
	require.Equal(t, servers.StatusRebuilding, string(row.Status))
	require.Equal(t, old, *row.Vmid, "old VM is still the server's")
	require.NotNil(t, row.RebuildVmid)
	_, ok := e.App.PVE.VM(old)
	require.True(t, ok, "old VM kept until the new one exists")

	servers.SetRebuildAfterStep(w, nil)
	require.NoError(t, w.Work(ctx, jobOf(args, 2)))
	require.Equal(t, 1, vmCount(t, e), "no leaked VM")
	require.Equal(t, 1, countCalls(e, "POST /nodes/pve1/qemu/9001/clone"))
	require.Equal(t, 2, countCalls(e, "GET /cluster/nextid"), "one for create, one for rebuild")
	require.Equal(t, *row.RebuildVmid, vmidOf(t, e, s.ServerID))
	waitAct, err := actions.Get(ctx, e.App.Ent, e.Project, id)
	require.NoError(t, err)
	require.Equal(t, actions.StatusSuccess, waitAct.Status)
	require.Equal(t, servers.StatusRunning, status(t, e, s.ServerID))
}

func TestRebuildCrashAfterSwap(t *testing.T) {
	ctx := context.Background()
	e, s, w := rebuildEnv(t)
	old := vmidOf(t, e, s.ServerID)
	id := startAction(t, e, s.ServerID, "rebuild", map[string]any{"image": "debian"})
	args := queuedArgs[servers.RebuildServerArgs](t, e, id)
	crashCtx, crash := context.WithCancel(ctx)
	servers.SetRebuildAfterStep(w, func(n int) {
		if n == servers.StepSwap {
			crash()
		}
	})
	require.Error(t, w.Work(crashCtx, jobOf(args, 1)))
	row, err := e.App.Ent.Server.Get(ctx, s.ServerID)
	require.NoError(t, err)
	require.NotEqual(t, old, *row.Vmid, "swapped to the new VM")
	require.Equal(t, old, *row.RebuildVmid, "old VM still to retire")
	require.Equal(t, 2, vmCount(t, e))

	servers.SetRebuildAfterStep(w, nil)
	require.NoError(t, w.Work(ctx, jobOf(args, 2)))
	_, ok := e.App.PVE.VM(old)
	require.False(t, ok)
	require.Equal(t, 1, vmCount(t, e))
	row, err = e.App.Ent.Server.Get(ctx, s.ServerID)
	require.NoError(t, err)
	require.Nil(t, row.RebuildVmid)
	vm, _ := e.App.PVE.VM(*row.Vmid)
	require.Equal(t, "running", vm.Status)
}

func TestRebuildFailureKeepsOldVM(t *testing.T) {
	ctx := context.Background()
	e, s, w := rebuildEnv(t)
	before, err := e.App.Ent.Server.Get(ctx, s.ServerID)
	require.NoError(t, err)
	id := startAction(t, e, s.ServerID, "rebuild", map[string]any{"image": "debian"})
	e.App.PVE.FailNext("/resize", http.StatusInternalServerError)
	var cancel *river.JobCancelError
	require.True(t, errors.As(w.Work(ctx, jobOf(queuedArgs[servers.RebuildServerArgs](t, e, id), 1)), &cancel))
	a, err := actions.Get(ctx, e.App.Ent, e.Project, id)
	require.NoError(t, err)
	require.Equal(t, actions.StatusError, a.Status)
	require.Equal(t, "pve_error", a.Error.Code)
	require.Contains(t, a.Error.Message, "resize disk")
	require.NotContains(t, a.Error.Message, "cleanup incomplete")
	require.Equal(t, 1, vmCount(t, e), "new VM destroyed")
	vm, ok := e.App.PVE.VM(*before.Vmid)
	require.True(t, ok)
	require.Equal(t, "running", vm.Status)
	after, err := e.App.Ent.Server.Get(ctx, s.ServerID)
	require.NoError(t, err)
	require.Equal(t, servers.StatusRunning, string(after.Status))
	require.Equal(t, *before.Vmid, *after.Vmid)
	require.Equal(t, before.ImageID, after.ImageID)
	require.Equal(t, 1, after.IsoGeneration)
	require.Nil(t, after.RebuildVmid)
	_, ok = e.App.PVE.Volume("local:iso/nb-srv-" + sid(s.ServerID) + "-2.iso")
	require.False(t, ok)
	_, ok = e.App.PVE.Volume(isoVolid(s.ServerID))
	require.True(t, ok, "old ISO kept")

	// The server is usable again.
	id = startAction(t, e, s.ServerID, "rebuild", map[string]any{"image": "debian"})
	require.NoError(t, w.Work(ctx, jobOf(queuedArgs[servers.RebuildServerArgs](t, e, id), 1)))
	require.Equal(t, 1, vmCount(t, e))
}

func TestDeleteReleasesIP(t *testing.T) {
	ctx := context.Background()
	rec := &recorder{}
	e := workerEnv(t, rec)
	s := e.CreateServer("web", nil)
	waitOK(t, e, s.ActionID)
	vmid := vmidOf(t, e, s.ServerID)
	del, _ := expect[actionBody](t, e.C.Send(http.MethodDelete, spath(s.ServerID), nil), http.StatusOK)
	a := waitOK(t, e, del.Action.ID)
	require.Equal(t, "delete_server", a.Command)
	_, err := e.App.Ent.Server.Get(ctx, s.ServerID)
	require.Error(t, err, "row deleted")
	n, err := e.App.Ent.IPAllocation.Query().Count(ctx)
	require.NoError(t, err)
	require.Zero(t, n)
	_, ok := e.App.PVE.VM(vmid)
	require.False(t, ok)
	_, ok = e.App.PVE.Volume(isoVolid(s.ServerID))
	require.False(t, ok)
	expectErr(t, e.C.Get(spath(s.ServerID)), http.StatusNotFound, "not_found")
	got, _ := expect[actionJSON](t, e.C.Get("/v1/actions/"+sid(del.Action.ID)), http.StatusOK)
	require.Equal(t, "success", got.Status, "the action outlives the server")
	require.Eventually(t, func() bool {
		return slices.ContainsFunc(rec.all(), func(ev ext.Event) bool { return ev.Kind == ext.ServerDeleted && ev.ServerID == s.ServerID })
	}, testWait, testTick)

	// The freed IP is handed out again.
	s2 := e.CreateServer("web2", nil)
	got2, _ := expect[serverJSON](t, e.C.Get(spath(s2.ServerID)), http.StatusOK)
	require.Equal(t, "10.0.0.10", got2.PublicNet.IPv4.IP)
}

func TestDeleteAfterIncompleteCreate(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	w := servers.NewDeleteServerWorker(opts(e))
	pc, err := e.App.Catalog.ProxmoxFor(ctx, e.Location)
	require.NoError(t, err)

	// Create failed and could not prove its untagged clone: the saved nonce proves it now.
	s := e.CreateServer("web", nil)
	require.NoError(t, actions.Fail(ctx, e.App.Ent, s.ActionID, "pve_error", "x; cleanup incomplete"))
	e.App.PVE.AddVM("pve1", 150, "web")
	require.NoError(t, pc.SetConfig(ctx, "pve1", 150, map[string]string{"description": servers.CloneDescription(s.ServerID, "n1")}))
	e.App.PVE.SetVMStatus(150, "running")
	require.NoError(t, e.App.Ent.Server.UpdateOneID(s.ServerID).SetVmid(150).SetCloneNonce("n1").SetStatus("error").Exec(ctx))
	del, _ := expect[actionBody](t, e.C.Send(http.MethodDelete, spath(s.ServerID), nil), http.StatusOK)
	require.NoError(t, w.Work(ctx, jobOf(queuedArgs[servers.DeleteServerArgs](t, e, del.Action.ID), 1)))
	_, ok := e.App.PVE.VM(150)
	require.False(t, ok, "proven clone destroyed")
	n, err := e.App.Ent.Server.Query().Count(ctx)
	require.NoError(t, err)
	require.Zero(t, n)

	// A VM at the vmid with no tags and no proof is someone else's: left alone, ours counts as gone.
	s = e.CreateServer("web2", nil)
	require.NoError(t, actions.Succeed(ctx, e.App.Ent, s.ActionID))
	e.App.PVE.AddVM("pve1", 151, "foreign")
	require.NoError(t, e.App.Ent.Server.UpdateOneID(s.ServerID).SetVmid(151).SetStatus("missing").Exec(ctx))
	del, _ = expect[actionBody](t, e.C.Send(http.MethodDelete, spath(s.ServerID), nil), http.StatusOK)
	require.NoError(t, w.Work(ctx, jobOf(queuedArgs[servers.DeleteServerArgs](t, e, del.Action.ID), 1)))
	foreign, ok := e.App.PVE.VM(151)
	require.True(t, ok)
	require.Equal(t, "foreign", foreign.Name)
	require.Empty(t, foreign.Tags)
	n, err = e.App.Ent.IPAllocation.Query().Count(ctx)
	require.NoError(t, err)
	require.Zero(t, n)
	a, err := actions.Get(ctx, e.App.Ent, e.Project, del.Action.ID)
	require.NoError(t, err)
	require.Equal(t, actions.StatusSuccess, a.Status)
}

func TestDeleteFailureKeepsServer(t *testing.T) {
	ctx := context.Background()
	e, cw := directEnv(t)
	s := builtServer(t, e, cw, "web")
	vm, _ := e.App.PVE.VM(vmidOf(t, e, s.ServerID))
	del, _ := expect[actionBody](t, e.C.Send(http.MethodDelete, spath(s.ServerID), nil), http.StatusOK)
	e.App.PVE.FailNext(fmt.Sprintf("DELETE /nodes/%s/qemu/%d", vm.Node, vm.VMID), http.StatusInternalServerError)
	w := servers.NewDeleteServerWorker(opts(e))
	var cancel *river.JobCancelError
	require.True(t, errors.As(w.Work(ctx, jobOf(queuedArgs[servers.DeleteServerArgs](t, e, del.Action.ID), 1)), &cancel))
	a, err := actions.Get(ctx, e.App.Ent, e.Project, del.Action.ID)
	require.NoError(t, err)
	require.Equal(t, actions.StatusError, a.Status)
	require.Equal(t, servers.StatusOff, status(t, e, s.ServerID), "the VM was stopped, then destroy failed")
	n, err := e.App.Ent.IPAllocation.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n, "IPs kept while the VM exists")
}

func TestCleanupVMProofs(t *testing.T) {
	ctx := context.Background()
	f := fake.New(t)
	testutil.SeedPVE(f)
	pc := pve.New(f.Conn())
	desc := servers.CloneDescription(7, "n0nce")
	unknown := "UPID:pve1:00000000:unknown"

	// An unreadable task is no verdict: the nonce decides.
	f.AddVM("pve1", 150, "web")
	require.NoError(t, pc.SetConfig(ctx, "pve1", 150, map[string]string{"description": desc}))
	require.True(t, servers.CleanupVM(ctx, quiet(), pc, 7, "pve1", new(150), servers.CloneProof{UPID: &unknown, Nonce: new("n0nce")}, "local", ""))
	_, ok := f.VM(150)
	require.False(t, ok, "nonce proved it ours")

	// No nonce to fall back on: not gone, untouched.
	f.AddVM("pve1", 151, "web")
	require.False(t, servers.CleanupVM(ctx, quiet(), pc, 7, "pve1", new(151), servers.CloneProof{UPID: &unknown}, "local", ""))
	_, ok = f.VM(151)
	require.True(t, ok)

	// A substring of the description is not a match.
	f.AddVM("pve1", 152, "web")
	require.NoError(t, pc.SetConfig(ctx, "pve1", 152, map[string]string{"description": "copied: " + desc}))
	require.True(t, servers.CleanupVM(ctx, quiet(), pc, 7, "pve1", new(152), servers.CloneProof{Nonce: new("n0nce")}, "local", ""))
	v, ok := f.VM(152)
	require.True(t, ok, "foreign VM left alone")
	require.Empty(t, v.Tags)

	// Our clone task failed: the VM there is not ours.
	f.AddVM("pve1", 153, "other")
	f.FailNext("/status/start", http.StatusInternalServerError)
	up, err := pc.Power(ctx, "pve1", 153, pve.PowerStart)
	require.NoError(t, err)
	failed := string(up)
	require.True(t, servers.CleanupVM(ctx, quiet(), pc, 7, "pve1", new(153), servers.CloneProof{UPID: &failed}, "local", ""))
	_, ok = f.VM(153)
	require.True(t, ok)
}

func TestCreateServerNonceSubstringIsCollision(t *testing.T) {
	ctx := context.Background()
	e, w := directEnv(t)
	s := e.CreateServer("web", nil)
	pc, err := e.App.Catalog.ProxmoxFor(ctx, e.Location)
	require.NoError(t, err)
	e.App.PVE.AddVM("pve1", 100, "foreign")
	require.NoError(t, pc.SetConfig(ctx, "pve1", 100, map[string]string{"description": "x " + servers.CloneDescription(s.ServerID, "ours") + " y"}))
	require.NoError(t, e.App.Ent.Server.UpdateOneID(s.ServerID).SetVmid(100).SetCloneNonce("ours").SetCreateStep(1).Exec(ctx))
	require.NoError(t, w.Work(ctx, job(s, 1)))
	require.NotEqual(t, 100, vmidOf(t, e, s.ServerID))
	foreign, _ := e.App.PVE.VM(100)
	require.Empty(t, foreign.Tags)
	require.True(t, strings.HasPrefix(foreign.Config["description"], "x "))
}

func TestRebuildFailureAfterSwapKeepsNewVM(t *testing.T) {
	ctx := context.Background()
	e, s, w := rebuildEnv(t)
	old := vmidOf(t, e, s.ServerID)
	id := startAction(t, e, s.ServerID, "rebuild", map[string]any{"image": "debian"})
	e.App.PVE.FailNext("/status/start", http.StatusInternalServerError)
	var cancel *river.JobCancelError
	require.True(t, errors.As(w.Work(ctx, jobOf(queuedArgs[servers.RebuildServerArgs](t, e, id), 1)), &cancel))
	a, err := actions.Get(ctx, e.App.Ent, e.Project, id)
	require.NoError(t, err)
	require.Equal(t, actions.StatusError, a.Status)
	require.Contains(t, a.Error.Message, "start vm")
	row, err := e.App.Ent.Server.Get(ctx, s.ServerID)
	require.NoError(t, err)
	require.NotEqual(t, old, *row.Vmid)
	vm, ok := e.App.PVE.VM(*row.Vmid)
	require.True(t, ok, "the swapped-in VM is never destroyed")
	require.Equal(t, "stopped", vm.Status)
	require.Equal(t, servers.StatusOff, string(row.Status), "status from Proxmox")
	_, ok = e.App.PVE.VM(old)
	require.False(t, ok)
	require.Equal(t, 1, vmCount(t, e))
	require.Nil(t, row.RebuildVmid)
	require.Zero(t, row.RebuildStep)
}

// TestDeleteSparesReusedVMID: our VM vanished and Proxmox handed its vmid to someone else.
func TestDeleteSparesReusedVMID(t *testing.T) {
	ctx := context.Background()
	e, cw := directEnv(t)
	pc, err := e.App.Catalog.ProxmoxFor(ctx, e.Location)
	require.NoError(t, err)
	w := servers.NewDeleteServerWorker(opts(e))
	deleteIt := func(s testutil.CreatedServer) {
		t.Helper()
		del, _ := expect[actionBody](t, e.C.Send(http.MethodDelete, spath(s.ServerID), nil), http.StatusOK)
		require.NoError(t, w.Work(ctx, jobOf(queuedArgs[servers.DeleteServerArgs](t, e, del.Action.ID), 1)))
		waitOK(t, e, del.Action.ID)
		_, err := e.App.Ent.Server.Get(ctx, s.ServerID)
		require.Error(t, err)
		n, err := e.App.Ent.IPAllocation.Query().Where(ipallocation.ServerID(s.ServerID)).Count(ctx)
		require.NoError(t, err)
		require.Zero(t, n)
	}

	// (1) Another server's tagged VM, even with a successful clone task saved for ours.
	a := builtServer(t, e, cw, "a")
	vmid := vmidOf(t, e, a.ServerID)
	e.App.PVE.DeleteVM(vmid)
	e.App.PVE.AddVM("pve1", vmid, "b")
	require.NoError(t, pc.SetConfig(ctx, "pve1", vmid, map[string]string{"tags": "nebla;nb-srv-999"}))
	e.App.PVE.AddVM("pve1", 990, "x")
	up, err := pc.Power(ctx, "pve1", 990, pve.PowerStart)
	require.NoError(t, err)
	require.NoError(t, pc.WaitTask(ctx, "pve1", up))
	require.NoError(t, e.App.Ent.Server.UpdateOneID(a.ServerID).SetCloneUpid(string(up)).Exec(ctx))
	deleteIt(a)
	other, ok := e.App.PVE.VM(vmid)
	require.True(t, ok, "another server's VM survives")
	require.Equal(t, "nebla;nb-srv-999", other.Tags)

	// (2) An untagged VM: the spent clone proof no longer vouches for it.
	b := builtServer(t, e, cw, "c")
	vmid = vmidOf(t, e, b.ServerID)
	e.App.PVE.DeleteVM(vmid)
	e.App.PVE.AddVM("pve1", vmid, "stranger")
	deleteIt(b)
	stranger, ok := e.App.PVE.VM(vmid)
	require.True(t, ok, "untagged VM survives")
	require.Empty(t, stranger.Tags)
}

// noCloudinitDrive asserts no config key of vm holds a Proxmox cloud-init drive.
func noCloudinitDrive(t *testing.T, vm fake.FakeVM) {
	t.Helper()
	for k, v := range vm.Config {
		require.NotContains(t, v, "cloudinit", "config %s", k)
	}
}

func TestCreateAndRebuildReplaceTemplateCloudinitDrives(t *testing.T) {
	ctx := context.Background()
	e := workerEnv(t, nil)
	e.App.PVE.SetVMConfig(testutil.TemplateVMID, "ide2", "ceph:vm-9000-cloudinit,media=cdrom")
	e.App.PVE.SetVMConfig(testutil.TemplateVMID, "ide0", "ceph:vm-9000-cloudinit,media=cdrom")
	e.App.PVE.AddTemplate("pve1", 9001, "ceph")
	e.App.PVE.SetVMConfig(9001, "sata1", "ceph:vm-9001-cloudinit,media=cdrom")
	e.Admin.CreateImage("debian", e.Location, 9001)

	s := e.CreateServer("web", nil)
	waitOK(t, e, s.ActionID)
	row, err := e.App.Ent.Server.Get(ctx, s.ServerID)
	require.NoError(t, err)
	vm, ok := e.App.PVE.VM(*row.Vmid)
	require.True(t, ok)
	noCloudinitDrive(t, vm)
	require.Equal(t, *row.IsoVolid+",media=cdrom", vm.Config["ide2"])

	waitOK(t, e, startAction(t, e, s.ServerID, "rebuild", map[string]any{"image": "debian"}))
	row, err = e.App.Ent.Server.Get(ctx, s.ServerID)
	require.NoError(t, err)
	vm, ok = e.App.PVE.VM(*row.Vmid)
	require.True(t, ok)
	noCloudinitDrive(t, vm)
	require.Equal(t, *row.IsoVolid+",media=cdrom", vm.Config["ide2"])
}

func TestCloudinitDrives(t *testing.T) {
	cfg := map[string]string{
		"ide2":  "local-lvm:vm-9000-cloudinit,media=cdrom",
		"ide0":  "local:100/vm-100-cloudinit.qcow2,media=cdrom",
		"sata0": "ceph:base-9000-cloudinit,media=cdrom",
		"ide3":  "local:iso/cloudinit-tools.iso,media=cdrom",
		"ide1":  "local:iso/nb-srv-1-1.iso,media=cdrom",
		"scsi0": "local-lvm:vm-100-disk-0,size=10G",
		"name":  "cloudinit",
	}
	require.Equal(t, []string{"ide0", "ide2", "sata0"}, servers.CloudinitDrives(cfg))
	require.Empty(t, servers.CloudinitDrives(map[string]string{"ide2": "none,media=cdrom"}))
}

package servers_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Hexeption/nebla/internal/actions"
	"github.com/Hexeption/nebla/internal/ext"
	"github.com/Hexeption/nebla/internal/httpapi"
	"github.com/Hexeption/nebla/internal/pve"
	"github.com/Hexeption/nebla/internal/servers"
	"github.com/Hexeption/nebla/internal/testutil"
	"github.com/kdomanski/iso9660"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/require"
)

type recorder struct {
	mu     sync.Mutex
	events []ext.Event
}

func (r *recorder) Handle(_ context.Context, e ext.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
	return nil
}

func (r *recorder) all() []ext.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.events)
}

// workerEnv runs the real create worker through River.
func workerEnv(t *testing.T, sink ext.BillingSink) *testutil.ServerEnv {
	t.Helper()
	return testutil.NewServerEnv(testutil.NewAppWithWorkers(t, testutil.ServerWorkers, func(d *httpapi.Deps) {
		if sink != nil {
			d.Ext.BillingSink = sink
		}
	}))
}

// directEnv queues jobs without working them, plus a worker to call Work on by hand.
func directEnv(t *testing.T) (*testutil.ServerEnv, *servers.CreateServerWorker) {
	t.Helper()
	e := newEnv(t)
	w := servers.NewCreateServerWorker(servers.WorkerOptions{SQL: e.App.SQL, Ent: e.App.Ent, Catalog: e.App.Catalog})
	return e, w
}

func job(s testutil.CreatedServer, attempt int) *river.Job[servers.CreateServerArgs] {
	return &river.Job[servers.CreateServerArgs]{
		JobRow: &rivertype.JobRow{ID: 1, Attempt: attempt, MaxAttempts: servers.MaxCreateAttempts + 2},
		Args:   servers.CreateServerArgs{ServerID: s.ServerID, ActionID: s.ActionID, StartAfter: true},
	}
}

const (
	testWait = 10 * time.Second
	testTick = 20 * time.Millisecond
)

func tag(id int64) string { return fmt.Sprintf("nb-srv-%d", id) }

func hasTag(tags, want string) bool { return slices.Contains(servers.SplitTags(tags), want) }

func taggedVMs(t *testing.T, e *testutil.ServerEnv, id int64) []pve.VM {
	t.Helper()
	pc, err := e.App.Catalog.ProxmoxFor(context.Background(), e.Location)
	require.NoError(t, err)
	vms, err := pc.VMs(context.Background())
	require.NoError(t, err)
	return slices.DeleteFunc(vms, func(v pve.VM) bool { return !hasTag(v.Tags, tag(id)) })
}

func countCalls(e *testutil.ServerEnv, call string) int {
	n := 0
	for _, c := range e.App.PVE.Calls() {
		if c == call {
			n++
		}
	}
	return n
}

func isoVolid(id int64) string { return fmt.Sprintf("local:iso/nb-srv-%d-1.iso", id) }

func TestCreateServerHappyPath(t *testing.T) {
	ctx := context.Background()
	rec := &recorder{}
	e := workerEnv(t, rec)
	key := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl test@example"
	res := e.C.Send(http.MethodPost, "/v1/ssh_keys", map[string]string{"name": "laptop", "public_key": key})
	require.Equal(t, http.StatusCreated, res.StatusCode, body(t, res))

	s := e.CreateServer("web", map[string]any{"ssh_keys": []string{"laptop"}, "user_data": "#cloud-config\npackages: [htop]\n"})
	a := testutil.WaitAction(t, e.App.Ent, s.ActionID)
	require.Equal(t, actions.StatusSuccess, a.Status, "%+v", a.Error)
	require.Equal(t, 100, a.Progress)

	row, err := e.App.Ent.Server.Get(ctx, s.ServerID)
	require.NoError(t, err)
	require.Equal(t, servers.StatusRunning, string(row.Status))
	require.NotNil(t, row.Vmid)
	require.NotNil(t, row.IsoVolid)
	require.Equal(t, isoVolid(s.ServerID), *row.IsoVolid)

	vmid := *row.Vmid
	vm, ok := e.App.PVE.VM(vmid)
	require.True(t, ok)
	require.Equal(t, "pve1", vm.Node)
	require.Equal(t, "running", vm.Status)
	require.True(t, hasTag(vm.Tags, "nebla"), vm.Tags)
	require.True(t, hasTag(vm.Tags, tag(s.ServerID)), vm.Tags)
	require.Equal(t, "web", vm.Name)
	require.Equal(t, "2", vm.Config["cores"])
	require.Equal(t, "2048", vm.Config["memory"])
	require.Equal(t, "1", vm.Config["agent"])
	require.Equal(t, "virtio="+row.MAC+",bridge=vmbr0,firewall=1", vm.Config["net0"])
	require.Equal(t, isoVolid(s.ServerID)+",media=cdrom", vm.Config["ide2"])
	require.Contains(t, vm.Config["scsi0"], "size=20G")
	require.Equal(t, "1", vm.Firewall["enable"])
	require.Equal(t, "1", vm.Firewall["ipfilter"])
	require.Equal(t, "ACCEPT", vm.Firewall["policy_in"])
	require.Equal(t, []string{"10.0.0.10"}, vm.IPSets["ipfilter-net0"])
	require.Len(t, taggedVMs(t, e, s.ServerID), 1)

	raw, ok := e.App.PVE.Volume(isoVolid(s.ServerID))
	require.True(t, ok, "iso uploaded")
	img, err := iso9660.OpenImage(bytes.NewReader(raw))
	require.NoError(t, err)
	label, err := img.Label()
	require.NoError(t, err)
	require.Equal(t, "cidata", label)
	root, err := img.RootDir()
	require.NoError(t, err)
	kids, err := root.GetChildren()
	require.NoError(t, err)
	var ud string
	for _, f := range kids {
		if f.Name() == "user-data" {
			b, err := io.ReadAll(f.Reader())
			require.NoError(t, err)
			ud = string(b)
		}
	}
	require.Contains(t, ud, "htop")
	require.Contains(t, ud, key)

	require.Eventually(t, func() bool { return len(rec.all()) == 1 }, testWait, testTick)
	ev := rec.all()[0]
	require.Equal(t, ext.ServerCreated, ev.Kind)
	require.Equal(t, s.ServerID, ev.ServerID)
	require.Equal(t, e.Project, ev.ProjectID)
	require.Equal(t, "small", ev.Data["server_type"])
	require.Equal(t, "eu-1", ev.Data["location"])
	require.False(t, ev.At.IsZero())

	off := e.CreateServer("idle", map[string]any{"start_after_create": false})
	a = testutil.WaitAction(t, e.App.Ent, off.ActionID)
	require.Equal(t, actions.StatusSuccess, a.Status, "%+v", a.Error)
	row, err = e.App.Ent.Server.Get(ctx, off.ServerID)
	require.NoError(t, err)
	require.Equal(t, servers.StatusOff, string(row.Status))
	vm, ok = e.App.PVE.VM(*row.Vmid)
	require.True(t, ok)
	require.Equal(t, "stopped", vm.Status)
	require.Equal(t, 1, countCalls(e, fmt.Sprintf("POST /nodes/pve1/qemu/%d/status/start", vmid)))
}

func TestCreateServerClonesToPlacedNode(t *testing.T) {
	ctx := context.Background()
	e, w := directEnv(t)
	s := e.CreateServer("web", nil)
	require.NoError(t, e.App.Ent.Server.UpdateOneID(s.ServerID).SetNode("pve2").Exec(ctx))
	require.NoError(t, w.Work(ctx, job(s, 1)))
	row, err := e.App.Ent.Server.Get(ctx, s.ServerID)
	require.NoError(t, err)
	vm, ok := e.App.PVE.VM(*row.Vmid)
	require.True(t, ok)
	require.Equal(t, "pve2", vm.Node, "cloned from the template's node onto the server's node")
	require.Equal(t, 1, countCalls(e, "POST /nodes/pve1/qemu/9000/clone"))
	_, ok = e.App.PVE.Volume(isoVolid(s.ServerID))
	require.True(t, ok)
}

func TestCreateServerResumesAfterCrash(t *testing.T) {
	ctx := context.Background()
	e, w := directEnv(t)
	s := e.CreateServer("web", nil)

	crashCtx, crash := context.WithCancel(ctx)
	defer crash()
	servers.SetAfterStep(w, func(step int) {
		if step == 2 {
			crash()
		}
	})
	err := w.Work(crashCtx, job(s, 1))
	require.Error(t, err)
	var cancel *river.JobCancelError
	require.False(t, errors.As(err, &cancel), "a crash is not a permanent failure: %v", err)
	a, err := actions.Get(ctx, e.App.Ent, e.Project, s.ActionID)
	require.NoError(t, err)
	require.Equal(t, actions.StatusRunning, a.Status)
	require.Len(t, taggedVMs(t, e, s.ServerID), 1, "the clone survives the crash")

	servers.SetAfterStep(w, nil)
	require.NoError(t, w.Work(ctx, job(s, 2)))

	require.Len(t, taggedVMs(t, e, s.ServerID), 1)
	require.Equal(t, 1, countCalls(e, "GET /cluster/nextid"))
	require.Equal(t, 1, countCalls(e, "POST /nodes/pve1/qemu/9000/clone"))
	a, err = actions.Get(ctx, e.App.Ent, e.Project, s.ActionID)
	require.NoError(t, err)
	require.Equal(t, actions.StatusSuccess, a.Status)
	n, err := e.App.Ent.IPAllocation.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	row, err := e.App.Ent.Server.Get(ctx, s.ServerID)
	require.NoError(t, err)
	require.Equal(t, servers.StatusRunning, string(row.Status))
}

// vmCount counts non-template VMs in the fake.
func vmCount(t *testing.T, e *testutil.ServerEnv) int {
	t.Helper()
	pc, err := e.App.Catalog.ProxmoxFor(context.Background(), e.Location)
	require.NoError(t, err)
	vms, err := pc.VMs(context.Background())
	require.NoError(t, err)
	n := 0
	for _, v := range vms {
		if !v.Template {
			n++
		}
	}
	return n
}

// crashMidClone cancels Work while a delayed clone task is still running.
func crashMidClone(t *testing.T, e *testutil.ServerEnv, w *servers.CreateServerWorker, s testutil.CreatedServer, attempt int) int {
	t.Helper()
	ctx := context.Background()
	e.App.PVE.SetTaskDelay(500 * time.Millisecond)
	crashCtx, crash := context.WithCancel(ctx)
	defer crash()
	servers.SetAfterStep(w, func(step int) {
		if step == 1 {
			go func() { time.Sleep(100 * time.Millisecond); crash() }()
		}
	})
	err := w.Work(crashCtx, job(s, attempt))
	require.Error(t, err)
	var cancel *river.JobCancelError
	require.False(t, errors.As(err, &cancel), "%v", err)
	servers.SetAfterStep(w, nil)
	e.App.PVE.SetTaskDelay(0)
	row, err := e.App.Ent.Server.Get(ctx, s.ServerID)
	require.NoError(t, err)
	require.Equal(t, 1, row.CreateStep, "crashed while the clone task ran")
	require.NotNil(t, row.CloneUpid, "clone upid saved before waiting")
	vm, ok := e.App.PVE.VM(*row.Vmid)
	require.True(t, ok, "the vmid already holds the in-flight clone")
	require.Equal(t, "clone", vm.Config["lock"])
	require.Empty(t, vm.Tags)
	return *row.Vmid
}

func TestCreateServerResumesAfterCrashMidClone(t *testing.T) {
	ctx := context.Background()
	e, w := directEnv(t)
	s := e.CreateServer("web", nil)
	vmid := crashMidClone(t, e, w, s, 1)

	// Resumes while the clone task is still running.
	require.NoError(t, w.Work(ctx, job(s, 2)))
	require.Equal(t, 1, vmCount(t, e), "exactly one VM")
	require.Equal(t, 1, countCalls(e, "GET /cluster/nextid"))
	require.Equal(t, 1, countCalls(e, "POST /nodes/pve1/qemu/9000/clone"), "no second clone")
	row, err := e.App.Ent.Server.Get(ctx, s.ServerID)
	require.NoError(t, err)
	require.Equal(t, vmid, *row.Vmid)
	require.Len(t, taggedVMs(t, e, s.ServerID), 1)
	a, err := actions.Get(ctx, e.App.Ent, e.Project, s.ActionID)
	require.NoError(t, err)
	require.Equal(t, actions.StatusSuccess, a.Status)
}

func TestCreateServerLockedVMIDIsTransient(t *testing.T) {
	ctx := context.Background()
	e, w := directEnv(t)
	s := e.CreateServer("web", nil)
	crashMidClone(t, e, w, s, 1)
	e.App.PVE.SetTaskDelay(time.Hour)
	// Without the saved upid, a locked VM at our vmid is retried, never a collision.
	require.NoError(t, e.App.Ent.Server.UpdateOneID(s.ServerID).ClearCloneUpid().Exec(ctx))
	err := w.Work(ctx, job(s, 2))
	require.Error(t, err)
	var cancel *river.JobCancelError
	require.False(t, errors.As(err, &cancel), "%v", err)
	require.Contains(t, err.Error(), "locked")
	require.Equal(t, 1, countCalls(e, "GET /cluster/nextid"))
	require.Equal(t, 1, countCalls(e, "POST /nodes/pve1/qemu/9000/clone"))
}

func TestCreateServerCloneTaskFails(t *testing.T) {
	ctx := context.Background()
	e, w := directEnv(t)
	s := e.CreateServer("web", nil)
	e.App.PVE.FailNext("/clone", http.StatusInternalServerError)
	err := w.Work(ctx, job(s, 1))
	var cancel *river.JobCancelError
	require.True(t, errors.As(err, &cancel), "%v", err)
	require.Zero(t, vmCount(t, e))
	a, err := actions.Get(ctx, e.App.Ent, e.Project, s.ActionID)
	require.NoError(t, err)
	require.Equal(t, actions.StatusError, a.Status)
	require.Contains(t, a.Error.Message, "clone template")
	n, err := e.App.Ent.IPAllocation.Query().Count(ctx)
	require.NoError(t, err)
	require.Zero(t, n)
}

func TestCreateServerCleanupAfterCap(t *testing.T) {
	ctx := context.Background()
	e, w := directEnv(t)
	s := e.CreateServer("web", nil)
	vmid := crashMidClone(t, e, w, s, servers.MaxCreateAttempts)
	require.Eventually(t, func() bool { v, _ := e.App.PVE.VM(vmid); return v.Name != "" }, testWait, testTick)

	// The spare attempt after the cap only cleans up, proving the untagged clone by its upid.
	err := w.Work(ctx, job(s, servers.MaxCreateAttempts+1))
	var cancel *river.JobCancelError
	require.True(t, errors.As(err, &cancel), "%v", err)
	require.Zero(t, vmCount(t, e), "clone destroyed")
	require.Equal(t, 1, countCalls(e, "POST /nodes/pve1/qemu/9000/clone"))
	a, err := actions.Get(ctx, e.App.Ent, e.Project, s.ActionID)
	require.NoError(t, err)
	require.Equal(t, actions.StatusError, a.Status)
	require.Equal(t, "pve_error", a.Error.Code)
	n, err := e.App.Ent.IPAllocation.Query().Count(ctx)
	require.NoError(t, err)
	require.Zero(t, n)
	row, err := e.App.Ent.Server.Get(ctx, s.ServerID)
	require.NoError(t, err)
	require.Equal(t, servers.StatusError, string(row.Status))
	require.Nil(t, row.Vmid)
}

func TestCreateServerCleanupPendingResumesCleanup(t *testing.T) {
	ctx := context.Background()
	e, w := directEnv(t)
	s := e.CreateServer("web", nil)
	j := job(s, 2)
	j.Errors = []rivertype.AttemptError{{Attempt: 1, Error: servers.CleanupPending + "resize disk: boom"}}
	err := w.Work(ctx, j)
	var cancel *river.JobCancelError
	require.True(t, errors.As(err, &cancel), "%v", err)
	require.Zero(t, countCalls(e, "GET /cluster/nextid"), "no create steps run")
	a, err := actions.Get(ctx, e.App.Ent, e.Project, s.ActionID)
	require.NoError(t, err)
	require.Equal(t, actions.StatusError, a.Status)
	require.Equal(t, "resize disk: boom", a.Error.Message)
}

func TestCreateServerCleanupIncompleteKeepsIPs(t *testing.T) {
	ctx := context.Background()
	e, w := directEnv(t)
	s := e.CreateServer("web", nil)
	servers.SetAfterStep(w, func(step int) {
		if step == 3 {
			e.App.PVE.FailNext("/resize", http.StatusInternalServerError)
			e.App.PVE.FailNext("/cluster/resources", http.StatusServiceUnavailable)
		}
	})
	err := w.Work(ctx, job(s, 1))
	var cancel *river.JobCancelError
	require.True(t, errors.As(err, &cancel), "%v", err)
	row, err := e.App.Ent.Server.Get(ctx, s.ServerID)
	require.NoError(t, err)
	require.Equal(t, servers.StatusError, string(row.Status))
	require.NotNil(t, row.Vmid, "vmid kept for delete or reconcile")
	require.Len(t, taggedVMs(t, e, s.ServerID), 1, "vm still there")
	n, err := e.App.Ent.IPAllocation.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n, "ip kept while the vm exists")
	a, err := actions.Get(ctx, e.App.Ent, e.Project, s.ActionID)
	require.NoError(t, err)
	require.Equal(t, actions.StatusError, a.Status)
	require.Contains(t, a.Error.Message, "resize disk: injected failure (status 500)")
	require.Contains(t, a.Error.Message, "cleanup incomplete")
}

func TestCreateServerLeavesVMWithoutNeblaTag(t *testing.T) {
	ctx := context.Background()
	e, w := directEnv(t)
	s := e.CreateServer("web", nil)
	e.App.PVE.AddVM("pve1", 100, "other")
	pc, err := e.App.Catalog.ProxmoxFor(ctx, e.Location)
	require.NoError(t, err)
	require.NoError(t, pc.SetConfig(ctx, "pve1", 100, map[string]string{"tags": tag(s.ServerID)}))
	require.NoError(t, e.App.Ent.Server.UpdateOneID(s.ServerID).SetVmid(100).SetCreateStep(1).Exec(ctx))
	e.App.PVE.DeleteVM(testutil.TemplateVMID)

	err = w.Work(ctx, job(s, 1))
	var cancel *river.JobCancelError
	require.True(t, errors.As(err, &cancel), "%v", err)
	vm, ok := e.App.PVE.VM(100)
	require.True(t, ok, "a VM without the nebla tag is never destroyed")
	require.Equal(t, tag(s.ServerID), vm.Tags, "and never retagged")
	n, err := e.App.Ent.IPAllocation.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	a, err := actions.Get(ctx, e.App.Ent, e.Project, s.ActionID)
	require.NoError(t, err)
	require.Contains(t, a.Error.Message, "cleanup incomplete")
}

func TestCreateServerVMIDCollision(t *testing.T) {
	ctx := context.Background()
	e, w := directEnv(t)
	s := e.CreateServer("web", nil)
	// Someone else's VM took the VMID the job saved before cloning.
	require.NoError(t, e.App.Ent.Server.UpdateOneID(s.ServerID).SetVmid(100).SetCreateStep(1).Exec(ctx))
	e.App.PVE.AddVM("pve1", 100, "foreign")

	require.NoError(t, w.Work(ctx, job(s, 1)))
	row, err := e.App.Ent.Server.Get(ctx, s.ServerID)
	require.NoError(t, err)
	require.NotEqual(t, 100, *row.Vmid)
	foreign, ok := e.App.PVE.VM(100)
	require.True(t, ok)
	require.Equal(t, "foreign", foreign.Name, "the foreign VM is untouched")
	require.Len(t, taggedVMs(t, e, s.ServerID), 1)
}

func TestCreateServerTransientErrorRetries(t *testing.T) {
	ctx := context.Background()
	e, w := directEnv(t)
	s := e.CreateServer("web", nil)
	e.App.PVE.FailNext("/resize", http.StatusServiceUnavailable)
	err := w.Work(ctx, job(s, 1))
	require.Error(t, err)
	var cancel *river.JobCancelError
	require.False(t, errors.As(err, &cancel), "transient errors are retried: %v", err)
	require.Len(t, taggedVMs(t, e, s.ServerID), 1)

	require.NoError(t, w.Work(ctx, job(s, 2)))
	a, err := actions.Get(ctx, e.App.Ent, e.Project, s.ActionID)
	require.NoError(t, err)
	require.Equal(t, actions.StatusSuccess, a.Status)

	// A transient error on the last attempt cleans up.
	s2 := e.CreateServer("web-2", nil)
	e.App.PVE.FailNext("/resize", http.StatusServiceUnavailable)
	err = w.Work(ctx, job(s2, 3))
	require.True(t, errors.As(err, &cancel), "last attempt cancels: %v", err)
	require.Empty(t, taggedVMs(t, e, s2.ServerID))
	a, err = actions.Get(ctx, e.App.Ent, e.Project, s2.ActionID)
	require.NoError(t, err)
	require.Equal(t, actions.StatusError, a.Status)
}

func TestCreateServerPVEFailureCleansUp(t *testing.T) {
	ctx := context.Background()
	e := workerEnv(t, nil)

	check := func(s testutil.CreatedServer, msg string) {
		t.Helper()
		a := testutil.WaitAction(t, e.App.Ent, s.ActionID)
		require.Equal(t, actions.StatusError, a.Status)
		require.NotNil(t, a.Error)
		require.Equal(t, "pve_error", a.Error.Code)
		require.Contains(t, a.Error.Message, msg)
		require.Empty(t, taggedVMs(t, e, s.ServerID), "vm destroyed")
		_, ok := e.App.PVE.Volume(isoVolid(s.ServerID))
		require.False(t, ok, "iso deleted")
		row, err := e.App.Ent.Server.Get(ctx, s.ServerID)
		require.NoError(t, err)
		require.Equal(t, servers.StatusError, string(row.Status))
		n, err := e.App.Ent.IPAllocation.Query().Count(ctx)
		require.NoError(t, err)
		require.Zero(t, n, "ip released")
		require.NotNil(t, a.RiverJobID)
		require.Equal(t, rivertype.JobStateCancelled, testutil.WaitJob(t, e.App.Jobs, *a.RiverJobID).State)
	}

	require.False(t, pve.IsTransient(&pve.Error{Status: 500, Message: "injected failure (status 500)"}))
	e.App.PVE.FailNext("/resize", http.StatusInternalServerError)
	check(e.CreateServer("web", nil), "injected failure (status 500)")
	require.Equal(t, 1, countCalls(e, "GET /cluster/nextid"), "permanent errors are not retried")

	// Fails after the ISO is uploaded.
	e.App.PVE.FailNext("/firewall/options", http.StatusBadRequest)
	check(e.CreateServer("web-2", nil), "injected failure (status 400)")
}

func TestCreateServerCleanupStopsRunningVM(t *testing.T) {
	ctx := context.Background()
	e, w := directEnv(t)
	s := e.CreateServer("web", nil)
	servers.SetAfterStep(w, func(step int) {
		if step == 5 {
			row, err := e.App.Ent.Server.Get(ctx, s.ServerID)
			require.NoError(t, err)
			e.App.PVE.SetVMStatus(*row.Vmid, "running")
			e.App.PVE.FailNext("/firewall/options", http.StatusBadRequest)
		}
	})
	err := w.Work(ctx, job(s, 1))
	var cancel *river.JobCancelError
	require.True(t, errors.As(err, &cancel), "%v", err)
	require.Empty(t, taggedVMs(t, e, s.ServerID), "running vm stopped and destroyed")
	_, ok := e.App.PVE.Volume(isoVolid(s.ServerID))
	require.False(t, ok)
	a, err := actions.Get(ctx, e.App.Ent, e.Project, s.ActionID)
	require.NoError(t, err)
	require.Equal(t, "pve_error", a.Error.Code)
}

func TestCreateServerTemplateGone(t *testing.T) {
	e, w := directEnv(t)
	s := e.CreateServer("web", nil)
	e.App.PVE.DeleteVM(testutil.TemplateVMID)
	err := w.Work(context.Background(), job(s, 1))
	var cancel *river.JobCancelError
	require.True(t, errors.As(err, &cancel), "%v", err)
	a, err := actions.Get(context.Background(), e.App.Ent, e.Project, s.ActionID)
	require.NoError(t, err)
	require.Equal(t, actions.StatusError, a.Status)
	require.Contains(t, a.Error.Message, "template 9000")
	require.Empty(t, taggedVMs(t, e, s.ServerID))
}

func TestCreateServerCleanupTxFailureIsResumed(t *testing.T) {
	ctx := context.Background()
	e, w := directEnv(t)
	s := e.CreateServer("web", nil)
	failed := false
	servers.SetCleanupTxHook(w, func() error {
		if !failed {
			failed = true
			return errors.New("db down")
		}
		return nil
	})
	e.App.PVE.FailNext("/resize", http.StatusInternalServerError)
	err := w.Work(ctx, job(s, 1))
	require.Error(t, err)
	var cancel *river.JobCancelError
	require.False(t, errors.As(err, &cancel), "a failed cleanup tx is retried: %v", err)
	require.True(t, strings.HasPrefix(err.Error(), servers.CleanupPending), err.Error())
	a, err2 := actions.Get(ctx, e.App.Ent, e.Project, s.ActionID)
	require.NoError(t, err2)
	require.Equal(t, actions.StatusRunning, a.Status, "the action is not failed before the cleanup tx commits")

	j := job(s, 2)
	j.Errors = []rivertype.AttemptError{{Attempt: 1, Error: err.Error()}}
	require.True(t, errors.As(w.Work(ctx, j), &cancel))
	n, err := e.App.Ent.IPAllocation.Query().Count(ctx)
	require.NoError(t, err)
	require.Zero(t, n, "ip released")
	row, err := e.App.Ent.Server.Get(ctx, s.ServerID)
	require.NoError(t, err)
	require.Equal(t, servers.StatusError, string(row.Status))
	a, err = actions.Get(ctx, e.App.Ent, e.Project, s.ActionID)
	require.NoError(t, err)
	require.Equal(t, actions.StatusError, a.Status)
	require.Contains(t, a.Error.Message, "resize disk: injected failure (status 500)")
	require.Zero(t, vmCount(t, e))
}

func TestCreateServerCleanupPendingRunsEvenIfActionFinished(t *testing.T) {
	ctx := context.Background()
	e, w := directEnv(t)
	s := e.CreateServer("web", nil)
	require.NoError(t, actions.Fail(ctx, e.App.Ent, s.ActionID, "pve_error", "earlier"))
	j := job(s, 2)
	j.Errors = []rivertype.AttemptError{{Attempt: 1, Error: servers.CleanupPending + "earlier"}}
	var cancel *river.JobCancelError
	require.True(t, errors.As(w.Work(ctx, j), &cancel))
	n, err := e.App.Ent.IPAllocation.Query().Count(ctx)
	require.NoError(t, err)
	require.Zero(t, n)
	row, err := e.App.Ent.Server.Get(ctx, s.ServerID)
	require.NoError(t, err)
	require.Equal(t, servers.StatusError, string(row.Status))
}

func TestCreateServerResumesAfterCrashBeforeUPIDSave(t *testing.T) {
	ctx := context.Background()
	e, w := directEnv(t)
	s := e.CreateServer("web", nil)
	crashCtx, crash := context.WithCancel(ctx)
	defer crash()
	servers.SetAfterClonePost(w, crash)
	err := w.Work(crashCtx, job(s, 1))
	require.Error(t, err)
	row, err := e.App.Ent.Server.Get(ctx, s.ServerID)
	require.NoError(t, err)
	require.Nil(t, row.CloneUpid, "crashed before the upid was saved")
	require.NotNil(t, row.CloneNonce)

	servers.SetAfterClonePost(w, nil)
	require.NoError(t, w.Work(ctx, job(s, 2)))
	require.Equal(t, 1, vmCount(t, e), "exactly one VM")
	require.Equal(t, 1, countCalls(e, "GET /cluster/nextid"))
	require.Equal(t, 1, countCalls(e, "POST /nodes/pve1/qemu/9000/clone"))
	require.Len(t, taggedVMs(t, e, s.ServerID), 1)
	vm, ok := e.App.PVE.VM(*row.Vmid)
	require.True(t, ok)
	require.Contains(t, vm.Config["description"], *row.CloneNonce)
}

func TestCreateServerNonceWithoutVMClonesAgain(t *testing.T) {
	ctx := context.Background()
	e, w := directEnv(t)
	s := e.CreateServer("web", nil)
	// The clone POST never landed: a nonce is saved but nothing sits at the vmid.
	require.NoError(t, e.App.Ent.Server.UpdateOneID(s.ServerID).SetVmid(150).SetCloneNonce("lost").SetCreateStep(1).Exec(ctx))
	require.NoError(t, w.Work(ctx, job(s, 1)))
	row, err := e.App.Ent.Server.Get(ctx, s.ServerID)
	require.NoError(t, err)
	require.Equal(t, 150, *row.Vmid, "same vmid")
	require.Equal(t, 1, countCalls(e, "POST /nodes/pve1/qemu/9000/clone"))
	require.Nil(t, row.CloneNonce, "spent once the VM is tagged")
	vm, ok := e.App.PVE.VM(150)
	require.True(t, ok)
	require.Equal(t, servers.CloneDescription(s.ServerID, "lost"), vm.Config["description"])
	require.Zero(t, countCalls(e, "GET /cluster/nextid"))
}

func TestCreateServerNonceMismatchIsCollision(t *testing.T) {
	ctx := context.Background()
	e, w := directEnv(t)
	s := e.CreateServer("web", nil)
	e.App.PVE.AddVM("pve1", 100, "foreign")
	require.NoError(t, e.App.Ent.Server.UpdateOneID(s.ServerID).SetVmid(100).SetCloneNonce("ours").SetCreateStep(1).Exec(ctx))
	require.NoError(t, w.Work(ctx, job(s, 1)))
	row, err := e.App.Ent.Server.Get(ctx, s.ServerID)
	require.NoError(t, err)
	require.NotEqual(t, 100, *row.Vmid)
	foreign, ok := e.App.PVE.VM(100)
	require.True(t, ok)
	require.Equal(t, "foreign", foreign.Name)
	require.Empty(t, foreign.Tags, "foreign VM untouched")
}

func TestCreateServerCleanupFailedCloneTaskReleasesIPs(t *testing.T) {
	ctx := context.Background()
	e, w := directEnv(t)
	s := e.CreateServer("web", nil)
	e.App.PVE.FailNext("/clone", http.StatusInternalServerError)
	// Someone else's VM lands on our vmid after our clone task failed.
	servers.SetAfterClonePost(w, func() {
		row, err := e.App.Ent.Server.Get(ctx, s.ServerID)
		require.NoError(t, err)
		e.App.PVE.AddVM("pve1", *row.Vmid, "foreign")
	})
	var cancel *river.JobCancelError
	require.True(t, errors.As(w.Work(ctx, job(s, 1)), &cancel))
	n, err := e.App.Ent.IPAllocation.Query().Count(ctx)
	require.NoError(t, err)
	require.Zero(t, n, "our clone failed, so nothing of ours is left and the ip is released")
	foreign, ok := e.App.PVE.VM(100)
	require.True(t, ok)
	require.Equal(t, "foreign", foreign.Name)
	a, err := actions.Get(ctx, e.App.Ent, e.Project, s.ActionID)
	require.NoError(t, err)
	require.NotContains(t, a.Error.Message, "cleanup incomplete")
}

// TestCreateDoesNotAdoptTaggedVM checks a VM already tagged for the server id is left alone; create picks NextID.
func TestCreateDoesNotAdoptTaggedVM(t *testing.T) {
	ctx := context.Background()
	e, w := directEnv(t)
	s := e.CreateServer("web", nil)
	e.App.PVE.AddVM("pve1", 900, "stale")
	e.App.PVE.SetVMConfig(900, "tags", "nebla;"+tag(s.ServerID))
	before, _ := e.App.PVE.VM(900)
	require.NoError(t, w.Work(ctx, job(s, 1)))
	a, err := actions.Get(ctx, e.App.Ent, e.Project, s.ActionID)
	require.NoError(t, err)
	require.Equal(t, actions.StatusSuccess, a.Status, "%+v", a.Error)
	row, err := e.App.Ent.Server.Get(ctx, s.ServerID)
	require.NoError(t, err)
	require.NotEqual(t, 900, *row.Vmid)
	after, ok := e.App.PVE.VM(900)
	require.True(t, ok)
	require.Equal(t, before, after)
}

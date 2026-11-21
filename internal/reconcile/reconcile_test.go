package reconcile_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/db"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/action"
	"github.com/nebla-cloud/nebla/internal/ent/node"
	"github.com/nebla-cloud/nebla/internal/ent/server"
	"github.com/nebla-cloud/nebla/internal/firewalls"
	"github.com/nebla-cloud/nebla/internal/httpapi"
	"github.com/nebla-cloud/nebla/internal/jobs"
	"github.com/nebla-cloud/nebla/internal/pve"
	"github.com/nebla-cloud/nebla/internal/pve/fake"
	"github.com/nebla-cloud/nebla/internal/reconcile"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/require"
)

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// lockedBuffer is a log sink safe for concurrent writes.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func newReconciler(e *testutil.ServerEnv, log *slog.Logger) *reconcile.Reconciler {
	return reconcile.New(reconcile.Options{SQL: e.App.SQL, Ent: e.App.Ent, Catalog: e.App.Catalog, Jobs: e.App.Jobs, Logger: log})
}

// workerEnv works server jobs through River.
func workerEnv(t *testing.T) *testutil.ServerEnv {
	t.Helper()
	return testutil.NewServerEnv(testutil.NewAppWithWorkers(t, testutil.ServerWorkers))
}

// running creates a server and waits for its create job.
func running(t *testing.T, e *testutil.ServerEnv, name string) (*ent.Server, int) {
	t.Helper()
	s := e.CreateServer(name, nil)
	a := testutil.WaitAction(t, e.App.Ent, s.ActionID)
	require.Equal(t, actions.StatusSuccess, a.Status, "%+v", a.Error)
	row := get(t, e, s.ServerID)
	require.NotNil(t, row.Vmid)
	return row, *row.Vmid
}

func get(t *testing.T, e *testutil.ServerEnv, id int64) *ent.Server {
	t.Helper()
	row, err := e.App.Ent.Server.Get(context.Background(), id)
	require.NoError(t, err)
	return row
}

func TestReconcileStatusChange(t *testing.T) {
	ctx := context.Background()
	e := workerEnv(t)
	r := newReconciler(e, quiet())
	row, vmid := running(t, e, "web")

	require.NoError(t, r.Run(ctx, e.Location))
	require.Equal(t, server.StatusRunning, get(t, e, row.ID).Status)

	e.App.PVE.SetVMStatus(vmid, "stopped")
	require.NoError(t, r.Run(ctx, e.Location))
	require.Equal(t, server.StatusOff, get(t, e, row.ID).Status)

	e.App.PVE.SetVMStatus(vmid, "running")
	require.NoError(t, r.Run(ctx, e.Location))
	require.Equal(t, server.StatusRunning, get(t, e, row.ID).Status)

	// Paused counts as running.
	e.App.PVE.SetVMStatus(vmid, "stopped")
	require.NoError(t, r.Run(ctx, e.Location))
	e.App.PVE.SetVMStatus(vmid, "paused")
	require.NoError(t, r.Run(ctx, e.Location))
	require.Equal(t, server.StatusRunning, get(t, e, row.ID).Status)

	// An unknown state (node unreachable) changes nothing.
	e.App.PVE.SetVMStatus(vmid, "unknown")
	require.NoError(t, r.Run(ctx, e.Location))
	require.Equal(t, server.StatusRunning, get(t, e, row.ID).Status)

	// A server in error keeps its status even when its VM runs.
	require.NoError(t, e.App.Ent.Server.UpdateOneID(row.ID).SetStatus(server.StatusError).Exec(ctx))
	e.App.PVE.SetVMStatus(vmid, "running")
	require.NoError(t, r.Run(ctx, e.Location))
	require.Equal(t, server.StatusError, get(t, e, row.ID).Status)
}

func TestReconcileMissingVM(t *testing.T) {
	ctx := context.Background()
	e := workerEnv(t)
	r := newReconciler(e, quiet())
	row, vmid := running(t, e, "web")
	vm, ok := e.App.PVE.VM(vmid)
	require.True(t, ok)

	// A server without a VM yet is left alone.
	q := e.CreateServer("queued", nil)
	testutil.WaitAction(t, e.App.Ent, q.ActionID)
	require.NoError(t, e.App.Ent.Server.UpdateOneID(q.ServerID).ClearVmid().SetStatus(server.StatusOff).Exec(ctx))

	e.App.PVE.DeleteVM(vmid)
	require.NoError(t, r.Run(ctx, e.Location))
	require.Equal(t, server.StatusMissing, get(t, e, row.ID).Status)
	require.Equal(t, server.StatusOff, get(t, e, q.ServerID).Status)

	// Another server's VM at the vmid does not count.
	e.App.PVE.AddVM("pve2", vmid, "other")
	pc, err := e.App.Catalog.ProxmoxFor(ctx, e.Location)
	require.NoError(t, err)
	require.NoError(t, pc.SetConfig(ctx, "pve2", vmid, map[string]string{"tags": "nebla;nb-srv-" + strconv.FormatInt(row.ID+1000, 10)}))
	require.NoError(t, r.Run(ctx, e.Location))
	require.Equal(t, server.StatusMissing, get(t, e, row.ID).Status)

	// The VM comes back with our tags: status and node follow Proxmox.
	require.NoError(t, pc.SetConfig(ctx, "pve2", vmid, map[string]string{"tags": vm.Tags}))
	require.NoError(t, r.Run(ctx, e.Location))
	got := get(t, e, row.ID)
	require.Equal(t, server.StatusOff, got.Status)
	require.Equal(t, "pve2", got.Node)
}

func TestReconcileSkipsLockedServer(t *testing.T) {
	ctx := context.Background()
	e := workerEnv(t)
	r := newReconciler(e, quiet())
	row, vmid := running(t, e, "web")

	// A running action whose job is still scheduled holds the server.
	var act actions.Action
	require.NoError(t, db.WithTx(ctx, e.App.SQL, func(tx *sql.Tx, etx *ent.Client) error {
		var err error
		act, err = e.App.Jobs.StartAction(ctx, tx, etx, e.Project, "stop_server",
			[]actions.Resource{{ID: row.ID, Type: servers.ResourceType}},
			func(int64) river.JobArgs { return jobs.PingArgs{} }, &river.InsertOpts{ScheduledAt: time.Now().Add(time.Hour)})
		return err
	}))
	e.App.PVE.SetVMStatus(vmid, "stopped")
	e.App.PVE.MoveVM(vmid, "pve2")
	require.NoError(t, r.Run(ctx, e.Location))
	got := get(t, e, row.ID)
	require.Equal(t, server.StatusRunning, got.Status)
	require.Equal(t, "pve1", got.Node)
	a, err := actions.Get(ctx, e.App.Ent, e.Project, act.ID)
	require.NoError(t, err)
	require.Equal(t, actions.StatusRunning, a.Status, "a live job is not orphaned")

	// Transient statuses are skipped even without an action.
	require.NoError(t, actions.Succeed(ctx, e.App.Ent, act.ID))
	for _, st := range []server.Status{server.StatusRebuilding, server.StatusDeleting, server.StatusInitializing, server.StatusStarting, server.StatusStopping} {
		require.NoError(t, e.App.Ent.Server.UpdateOneID(row.ID).SetStatus(st).Exec(ctx))
		require.NoError(t, r.Run(ctx, e.Location))
		require.Equal(t, st, get(t, e, row.ID).Status)
	}
}

func TestReconcileFollowsMigration(t *testing.T) {
	ctx := context.Background()
	e := workerEnv(t)
	r := newReconciler(e, quiet())
	row, vmid := running(t, e, "web")
	require.Equal(t, "pve1", row.Node)

	e.App.PVE.MoveVM(vmid, "pve2")
	e.App.PVE.SetNodeLoad("pve2", 0.5, 4096)
	require.NoError(t, r.Run(ctx, e.Location))
	got := get(t, e, row.ID)
	require.Equal(t, "pve2", got.Node)
	require.Equal(t, server.StatusRunning, got.Status)

	// Node load is refreshed for placement.
	n, err := e.App.Ent.Node.Query().Where(node.LocationID(e.Location), node.Name("pve2")).Only(ctx)
	require.NoError(t, err)
	require.InDelta(t, 0.5, n.CPU, 0.001)
	require.Equal(t, int64(4096)<<20, n.Mem)
}

func TestReconcileGuardsAgainstMassMissing(t *testing.T) {
	ctx := context.Background()
	e := workerEnv(t)
	r := newReconciler(e, quiet())
	row, vmid := running(t, e, "web")

	// VMs() errors: nothing changes.
	e.App.PVE.DeleteVM(vmid)
	e.App.PVE.FailNext("GET /cluster/resources", http.StatusInternalServerError)
	require.Error(t, r.Run(ctx, e.Location))
	require.Equal(t, server.StatusRunning, get(t, e, row.ID).Status)

	// An empty VM list while servers have VMs is a Proxmox problem.
	e.App.PVE.DeleteVM(testutil.TemplateVMID)
	err := r.Run(ctx, e.Location)
	require.ErrorIs(t, err, reconcile.ErrProxmoxState)
	require.Equal(t, server.StatusRunning, get(t, e, row.ID).Status)

	// No nodes at all: also nothing changes.
	e.App.PVE.RemoveNode("pve1")
	e.App.PVE.RemoveNode("pve2")
	require.Error(t, r.Run(ctx, e.Location))
	require.Equal(t, server.StatusRunning, get(t, e, row.ID).Status)
}

func TestReconcileOrphanNeverDeleted(t *testing.T) {
	ctx := context.Background()
	e := workerEnv(t)
	logs := &lockedBuffer{}
	r := newReconciler(e, slog.New(slog.NewTextHandler(logs, nil)))
	row, vmid := running(t, e, "web")
	pc, err := e.App.Catalog.ProxmoxFor(ctx, e.Location)
	require.NoError(t, err)

	// A tagged VM for no server, a stale leftover with our server tag at another vmid, and an untagged VM.
	e.App.PVE.AddVM("pve1", 500, "orphan")
	require.NoError(t, pc.SetConfig(ctx, "pve1", 500, map[string]string{"tags": "nebla;nb-srv-99999"}))
	e.App.PVE.AddVM("pve2", 501, "leftover")
	require.NoError(t, pc.SetConfig(ctx, "pve2", 501, map[string]string{"tags": "nebla;" + servers.ServerTag(row.ID)}))
	e.App.PVE.SetVMStatus(501, "running")
	e.App.PVE.AddVM("pve1", 502, "foreign")

	before := len(e.App.PVE.Calls())
	for range 3 {
		require.NoError(t, r.Run(ctx, e.Location))
	}
	for _, id := range []int{500, 501, 502, vmid} {
		_, ok := e.App.PVE.VM(id)
		require.True(t, ok, "vm %d kept", id)
	}
	for _, c := range e.App.PVE.Calls()[before:] {
		require.True(t, strings.HasPrefix(c, "GET "), "reconcile only reads Proxmox: %s", c)
	}
	got := get(t, e, row.ID)
	require.Equal(t, server.StatusRunning, got.Status)
	require.Equal(t, "pve1", got.Node, "a leftover VM does not move the server")
	out := logs.String()
	require.Equal(t, 1, strings.Count(out, "vmid=500"), out)
	require.Equal(t, 1, strings.Count(out, "vmid=501"), out)
	require.NotContains(t, out, "vmid=502")

	// A known rebuild VM is not an orphan.
	require.NoError(t, e.App.Ent.Server.UpdateOneID(row.ID).SetRebuildVmid(501).Exec(ctx))
	logs2 := &lockedBuffer{}
	r2 := newReconciler(e, slog.New(slog.NewTextHandler(logs2, nil)))
	require.NoError(t, r2.Run(ctx, e.Location))
	require.NotContains(t, logs2.String(), "vmid=501")
	require.Equal(t, "pve1", get(t, e, row.ID).Node)
}

// discard marks an action's job as discarded, as River does when its attempts run out.
func discard(t *testing.T, e *testutil.ServerEnv, actionID int64) int64 {
	t.Helper()
	a, err := actions.Get(context.Background(), e.App.Ent, e.Project, actionID)
	require.NoError(t, err)
	require.NotNil(t, a.RiverJobID)
	_, err = e.App.SQL.ExecContext(context.Background(),
		`update river_job set state = 'discarded', finalized_at = now() where id = $1`, *a.RiverJobID)
	require.NoError(t, err)
	return *a.RiverJobID
}

func expectOrphaned(t *testing.T, e *testutil.ServerEnv, actionID int64) {
	t.Helper()
	a, err := actions.Get(context.Background(), e.App.Ent, e.Project, actionID)
	require.NoError(t, err)
	require.Equal(t, actions.StatusError, a.Status)
	require.NotNil(t, a.Error)
	require.Equal(t, "orphaned", a.Error.Code)
}

func TestReconcileRescuesStuckAction(t *testing.T) {
	ctx := context.Background()
	e := testutil.NewServerEnv(testutil.NewApp(t, testutil.QueueOnlyJobs(t)))
	r := newReconciler(e, quiet())
	e.Admin.CreateServerType("big", 4, 4096, 40)

	// Build the server by hand; queued jobs are never worked.
	s := e.CreateServer("web", nil)
	w := servers.NewCreateServerWorker(servers.WorkerOptions{SQL: e.App.SQL, Ent: e.App.Ent, Catalog: e.App.Catalog, Logger: quiet(),
		PollInterval: 10 * time.Millisecond})
	require.NoError(t, w.Work(ctx, &river.Job[servers.CreateServerArgs]{
		JobRow: &rivertype.JobRow{ID: 1, Attempt: 1, MaxAttempts: servers.MaxAttempts + 2},
		Args:   servers.CreateServerArgs{ServerID: s.ServerID, ActionID: s.ActionID, StartAfter: true}}))
	vmid := *get(t, e, s.ServerID).Vmid

	// A live job is left alone.
	res := e.C.Send(http.MethodPost, "/v1/servers/"+strconv.FormatInt(s.ServerID, 10)+"/actions/poweroff", nil)
	require.Equal(t, http.StatusCreated, res.StatusCode)
	var out struct {
		Action struct {
			ID int64 `json:"id"`
		} `json:"action"`
	}
	decode(t, res, &out)
	require.NoError(t, r.Run(ctx, e.Location))
	a, err := actions.Get(ctx, e.App.Ent, e.Project, out.Action.ID)
	require.NoError(t, err)
	require.Equal(t, actions.StatusRunning, a.Status)

	// The power job was discarded mid-way: the action fails and the status comes from Proxmox.
	require.NoError(t, e.App.Ent.Server.UpdateOneID(s.ServerID).SetStatus(server.StatusStopping).Exec(ctx))
	discard(t, e, out.Action.ID)
	require.NoError(t, r.Run(ctx, e.Location))
	expectOrphaned(t, e, out.Action.ID)
	require.Equal(t, server.StatusRunning, get(t, e, s.ServerID).Status)

	// A discarded change_type puts the old snapshot back.
	e.App.PVE.SetVMStatus(vmid, "stopped")
	require.NoError(t, r.Run(ctx, e.Location))
	require.Equal(t, server.StatusOff, get(t, e, s.ServerID).Status)
	res = e.C.Send(http.MethodPost, "/v1/servers/"+strconv.FormatInt(s.ServerID, 10)+"/actions/change_type",
		map[string]any{"server_type": "big", "upgrade_disk": true})
	require.Equal(t, http.StatusCreated, res.StatusCode)
	decode(t, res, &out)
	held := get(t, e, s.ServerID)
	require.Equal(t, 4, held.Cores)
	require.Equal(t, 40, held.DiskGB)
	discard(t, e, out.Action.ID)
	e.App.PVE.SetVMConfig(vmid, "cores", "4")
	require.NoError(t, r.Run(ctx, e.Location))
	expectOrphaned(t, e, out.Action.ID)
	vm, ok := e.App.PVE.VM(vmid)
	require.True(t, ok)
	require.Equal(t, "2", vm.Config["cores"], "Proxmox up: the VM config is restored too")
	got := get(t, e, s.ServerID)
	require.Equal(t, e.Type, got.ServerTypeID)
	require.Equal(t, 2, got.Cores)
	require.Equal(t, 2048, got.MemoryMB)
	require.Equal(t, 20, got.DiskGB, "Proxmox up: the disk never grew")
	require.Equal(t, server.StatusOff, got.Status)

	// A job that is gone counts as orphaned too.
	res = e.C.Send(http.MethodPost, "/v1/servers/"+strconv.FormatInt(s.ServerID, 10)+"/actions/poweron", nil)
	require.Equal(t, http.StatusCreated, res.StatusCode)
	decode(t, res, &out)
	jobID := discard(t, e, out.Action.ID)
	_, err = e.App.SQL.ExecContext(ctx, `delete from river_job where id = $1`, jobID)
	require.NoError(t, err)
	require.NoError(t, r.Run(ctx, e.Location))
	expectOrphaned(t, e, out.Action.ID)
}

func decode(t *testing.T, res *http.Response, v any) {
	t.Helper()
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(b, v), string(b))
}

func TestReconcileRunsThroughRiver(t *testing.T) {
	ctx := context.Background()
	e := testutil.NewServerEnv(testutil.NewAppWithWorkers(t, func(w *river.Workers, d *httpapi.Deps, p *fake.Server) {
		testutil.ServerWorkers(w, d, p)
		reconcile.Register(w, reconcile.New(reconcile.Options{SQL: d.SQL, Ent: d.Ent, Catalog: d.Catalog, Logger: quiet()}))
	}))
	row, vmid := running(t, e, "web")
	e.App.PVE.SetVMStatus(vmid, "stopped")
	_, err := e.App.Jobs.Client.Insert(ctx, reconcile.AllArgs{}, nil)
	require.NoError(t, err)
	require.Eventually(t, func() bool { return get(t, e, row.ID).Status == server.StatusOff }, 10*time.Second, 20*time.Millisecond)
}

func TestMaintenanceJobsAreUnique(t *testing.T) {
	ctx := context.Background()
	pool := testutil.NewDB(t)
	w := river.NewWorkers()
	reconcile.Register(w, reconcile.New(reconcile.Options{}))
	river.AddWorker(w, servers.NewUsageWorker(servers.WorkerOptions{}))
	j, err := jobs.New(pool, db.SQL(pool), w)
	require.NoError(t, err)
	for _, args := range []river.JobArgs{reconcile.AllArgs{}, reconcile.LocationArgs{LocationID: 1}, servers.UsageArgs{}} {
		first, err := j.Client.Insert(ctx, args, nil)
		require.NoError(t, err)
		require.False(t, first.UniqueSkippedAsDuplicate)
		require.Equal(t, jobs.QueueMaintenance, first.Job.Queue)
		again, err := j.Client.Insert(ctx, args, nil)
		require.NoError(t, err)
		require.True(t, again.UniqueSkippedAsDuplicate, "%s while queued", args.Kind())
	}
	other, err := j.Client.Insert(ctx, reconcile.LocationArgs{LocationID: 2}, nil)
	require.NoError(t, err)
	require.False(t, other.UniqueSkippedAsDuplicate, "other locations are not blocked")
	_, err = pool.Exec(ctx, `update river_job set state = 'completed', finalized_at = now()`)
	require.NoError(t, err)
	next, err := j.Client.Insert(ctx, reconcile.LocationArgs{LocationID: 1}, nil)
	require.NoError(t, err)
	require.False(t, next.UniqueSkippedAsDuplicate, "a finished run does not block the next")
	usage, err := j.Client.Insert(ctx, servers.UsageArgs{}, nil)
	require.NoError(t, err)
	require.True(t, usage.UniqueSkippedAsDuplicate, "usage runs once per hour, even after a restart")
	n := 0
	require.NoError(t, pool.QueryRow(ctx, `select count(*) from river_job where kind = $1`, servers.KindUsage).Scan(&n))
	require.Equal(t, 1, n)
}

func TestReconcileYieldsToActionAfterList(t *testing.T) {
	ctx := context.Background()
	e := workerEnv(t)
	r := newReconciler(e, quiet())
	row, vmid := running(t, e, "web")

	// The list shows the VM stopped, then a power action starts it and finishes before the write.
	e.App.PVE.SetVMStatus(vmid, "stopped")
	reconcile.SetAfterList(r, func() {
		a, err := actions.Create(ctx, e.App.Ent, e.Project, "start_server", []actions.Resource{{ID: row.ID, Type: servers.ResourceType}})
		require.NoError(t, err)
		e.App.PVE.SetVMStatus(vmid, "running")
		require.NoError(t, actions.Succeed(ctx, e.App.Ent, a.ID))
	})
	require.NoError(t, r.Run(ctx, e.Location))
	require.Equal(t, server.StatusRunning, get(t, e, row.ID).Status, "the stale list does not win")

	reconcile.SetAfterList(r, nil)
	e.App.PVE.SetVMStatus(vmid, "stopped")
	require.NoError(t, r.Run(ctx, e.Location))
	require.Equal(t, server.StatusOff, get(t, e, row.ID).Status)
}

// queuedEnv never works jobs; built servers are made by running the create worker by hand.
func queuedEnv(t *testing.T) *testutil.ServerEnv {
	t.Helper()
	return testutil.NewServerEnv(testutil.NewApp(t, testutil.QueueOnlyJobs(t)))
}

func build(t *testing.T, e *testutil.ServerEnv, name string) int64 {
	t.Helper()
	s := e.CreateServer(name, nil)
	w := servers.NewCreateServerWorker(servers.WorkerOptions{SQL: e.App.SQL, Ent: e.App.Ent, Catalog: e.App.Catalog, Logger: quiet(),
		PollInterval: 10 * time.Millisecond})
	require.NoError(t, w.Work(context.Background(), &river.Job[servers.CreateServerArgs]{
		JobRow: &rivertype.JobRow{ID: 1, Attempt: 1, MaxAttempts: servers.MaxAttempts + 2},
		Args:   servers.CreateServerArgs{ServerID: s.ServerID, ActionID: s.ActionID, StartAfter: true}}))
	return s.ServerID
}

// orphan queues a power op on id and discards its job, leaving the server in st.
func orphan(t *testing.T, e *testutil.ServerEnv, id int64, op string, st server.Status) int64 {
	t.Helper()
	res := e.C.Send(http.MethodPost, "/v1/servers/"+strconv.FormatInt(id, 10)+"/actions/"+op, nil)
	require.Equal(t, http.StatusCreated, res.StatusCode)
	var out struct {
		Action struct {
			ID int64 `json:"id"`
		} `json:"action"`
	}
	decode(t, res, &out)
	require.NoError(t, e.App.Ent.Server.UpdateOneID(id).SetStatus(st).Exec(context.Background()))
	discard(t, e, out.Action.ID)
	return out.Action.ID
}

func TestReconcileOfflineNode(t *testing.T) {
	ctx := context.Background()
	e := workerEnv(t)
	r := newReconciler(e, quiet())
	a, avm := running(t, e, "a")
	b, bvm := running(t, e, "b")
	e.App.PVE.MoveVM(bvm, "pve2")
	require.NoError(t, r.Run(ctx, e.Location))
	require.Equal(t, "pve2", get(t, e, b.ID).Node)

	// pve2 goes down and hides its VMs; a's VM on pve1 really is deleted.
	e.App.PVE.SetNodeOffline("pve2", true)
	e.App.PVE.DeleteVM(avm)
	require.NoError(t, r.Run(ctx, e.Location))
	require.Equal(t, server.StatusMissing, get(t, e, a.ID).Status)
	got := get(t, e, b.ID)
	require.Equal(t, server.StatusRunning, got.Status, "a hidden VM on an offline node is unknown")
	require.Equal(t, "pve2", got.Node)

	// pve2 returns with the VM stopped.
	e.App.PVE.SetVMStatus(bvm, "stopped")
	e.App.PVE.SetNodeOffline("pve2", false)
	require.NoError(t, r.Run(ctx, e.Location))
	require.Equal(t, server.StatusOff, get(t, e, b.ID).Status)
}

func TestReconcileCircuitBreaker(t *testing.T) {
	ctx := context.Background()
	e := workerEnv(t)
	logs := &lockedBuffer{}
	r := newReconciler(e, slog.New(slog.NewTextHandler(logs, nil)))
	var ids []int64
	var vmids []int
	for _, name := range []string{"a", "b", "c", "d"} {
		row, vmid := running(t, e, name)
		ids, vmids = append(ids, row.ID), append(vmids, vmid)
	}

	// Two of four vanish: not over half, so both turn missing.
	e.App.PVE.DeleteVM(vmids[0])
	e.App.PVE.DeleteVM(vmids[1])
	require.NoError(t, r.Run(ctx, e.Location))
	require.Equal(t, server.StatusMissing, get(t, e, ids[0]).Status)
	require.Equal(t, server.StatusMissing, get(t, e, ids[1]).Status)

	for _, name := range []string{"e", "f"} {
		row, vmid := running(t, e, name)
		ids, vmids = append(ids, row.ID), append(vmids, vmid)
	}
	// Six servers with VMs, two already missing; the other four vanish at once: 4 of 6 is over half.
	for _, v := range vmids[2:] {
		e.App.PVE.DeleteVM(v)
	}
	err := r.Run(ctx, e.Location)
	require.ErrorIs(t, err, reconcile.ErrProxmoxState)
	for _, id := range ids[2:] {
		require.Equal(t, server.StatusRunning, get(t, e, id).Status, "breaker tripped: nothing marked")
	}
	require.Contains(t, logs.String(), "refusing to mark servers missing")
}

func TestReconcileRescueFailsTransientAndContinues(t *testing.T) {
	ctx := context.Background()
	e := queuedEnv(t)
	r := newReconciler(e, quiet())
	id := build(t, e, "web")
	other := build(t, e, "other")

	// A lost job in initializing, rebuilding or deleting leaves the server in error.
	for _, st := range []server.Status{server.StatusInitializing, server.StatusRebuilding, server.StatusDeleting} {
		act := orphan(t, e, id, "reboot", st)
		require.NoError(t, r.Run(ctx, e.Location))
		expectOrphaned(t, e, act)
		require.Equal(t, server.StatusError, get(t, e, id).Status, "%s", st)
		require.NoError(t, e.App.Ent.Server.UpdateOneID(id).SetStatus(server.StatusRunning).Exec(ctx))
	}

	// One failing rescue does not block the next.
	first := orphan(t, e, id, "reboot", server.StatusRunning)
	second := orphan(t, e, other, "reboot", server.StatusRunning)
	reconcile.SetRescueHook(r, func(a int64) error {
		if a == first {
			return io.ErrUnexpectedEOF
		}
		return nil
	})
	err := r.Run(ctx, e.Location)
	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	expectOrphaned(t, e, second)
	a, err := actions.Get(ctx, e.App.Ent, e.Project, first)
	require.NoError(t, err)
	require.Equal(t, actions.StatusRunning, a.Status, "the failed rescue rolled back")
	reconcile.SetRescueHook(r, nil)
	require.NoError(t, r.Run(ctx, e.Location))
	expectOrphaned(t, e, first)
}

func TestReconcileRescueOnOfflineNode(t *testing.T) {
	ctx := context.Background()
	e := queuedEnv(t)
	r := newReconciler(e, quiet())
	id := build(t, e, "web")
	other := build(t, e, "other")
	e.App.PVE.MoveVM(*get(t, e, id).Vmid, "pve2")
	require.NoError(t, r.Run(ctx, e.Location))
	require.Equal(t, "pve2", get(t, e, id).Node)

	// The power job is lost while pve2 hides the VM: fall back to the status before the action.
	e.App.PVE.SetNodeOffline("pve2", true)
	act := orphan(t, e, id, "poweroff", server.StatusStopping)
	require.NoError(t, r.Run(ctx, e.Location))
	expectOrphaned(t, e, act)
	require.Equal(t, server.StatusRunning, get(t, e, id).Status)
	require.Equal(t, server.StatusRunning, get(t, e, other).Status)
}

func TestReconcileRescuesWithProxmoxDown(t *testing.T) {
	ctx := context.Background()
	e := queuedEnv(t)
	r := newReconciler(e, quiet())
	e.Admin.CreateServerType("big", 4, 4096, 40)
	id := build(t, e, "web")
	vmid := *get(t, e, id).Vmid

	// An orphaned create with Proxmox down: the server and the action end in error.
	c := e.CreateServer("new", nil)
	require.Equal(t, server.StatusInitializing, get(t, e, c.ServerID).Status)
	discard(t, e, c.ActionID)
	e.App.PVE.SetDown(true)
	require.Error(t, r.Run(ctx, e.Location))
	expectOrphaned(t, e, c.ActionID)
	require.Equal(t, server.StatusError, get(t, e, c.ServerID).Status)

	// An orphaned change_type with Proxmox down: the row snapshot and quota usage come back.
	e.App.PVE.SetDown(false)
	e.App.PVE.SetVMStatus(vmid, "stopped")
	require.NoError(t, r.Run(ctx, e.Location))
	res := e.C.Send(http.MethodPost, "/v1/servers/"+strconv.FormatInt(id, 10)+"/actions/change_type",
		map[string]any{"server_type": "big", "upgrade_disk": true})
	require.Equal(t, http.StatusCreated, res.StatusCode)
	var out struct {
		Action struct {
			ID int64 `json:"id"`
		} `json:"action"`
	}
	decode(t, res, &out)
	require.Equal(t, 4, get(t, e, id).Cores)
	discard(t, e, out.Action.ID)
	e.App.PVE.SetDown(true)
	require.Error(t, r.Run(ctx, e.Location))
	expectOrphaned(t, e, out.Action.ID)
	got := get(t, e, id)
	require.Equal(t, e.Type, got.ServerTypeID)
	require.Equal(t, 2, got.Cores)
	require.Equal(t, 2048, got.MemoryMB)
	require.Equal(t, 40, got.DiskGB, "Proxmox down: the requested disk size is kept")
	var vcpu int
	require.NoError(t, e.App.SQL.QueryRowContext(ctx,
		`select coalesce(sum(cores), 0) from servers where project_id = $1 and id = $2`, e.Project, id).Scan(&vcpu))
	require.Equal(t, 2, vcpu)
}

func TestReconcileRescueBeforeBreaker(t *testing.T) {
	ctx := context.Background()
	e := queuedEnv(t)
	r := newReconciler(e, quiet())
	var ids []int64
	for _, name := range []string{"a", "b", "c", "d"} {
		ids = append(ids, build(t, e, name))
	}
	act := orphan(t, e, ids[0], "poweroff", server.StatusStopping)
	for _, id := range ids {
		e.App.PVE.DeleteVM(*get(t, e, id).Vmid)
	}
	e.App.PVE.AddVM("pve1", 900, "keep-list-nonempty")
	require.ErrorIs(t, r.Run(ctx, e.Location), reconcile.ErrProxmoxState)
	expectOrphaned(t, e, act)
	require.Equal(t, server.StatusRunning, get(t, e, ids[0]).Status)
}

func TestRescueAllWithoutProxmox(t *testing.T) {
	ctx := context.Background()
	e := queuedEnv(t)
	r := newReconciler(e, quiet())
	id := build(t, e, "web")
	c := e.CreateServer("new", nil)
	discard(t, e, c.ActionID)

	// A power job gone with its args: starting falls back to off.
	start := orphan(t, e, id, "poweron", server.StatusStarting)
	_, err := e.App.SQL.ExecContext(ctx, `delete from river_job where kind = $1`, servers.KindPowerServer)
	require.NoError(t, err)
	e.App.PVE.SetDown(true)
	require.NoError(t, r.RescueAll(ctx))
	expectOrphaned(t, e, c.ActionID)
	expectOrphaned(t, e, start)
	require.Equal(t, server.StatusError, get(t, e, c.ServerID).Status)
	require.Equal(t, server.StatusOff, get(t, e, id).Status)
}

func TestRescueAllSkipsFailingRow(t *testing.T) {
	ctx := context.Background()
	e := queuedEnv(t)
	r := newReconciler(e, quiet())
	id := build(t, e, "web")
	other := build(t, e, "other")
	first := orphan(t, e, id, "reboot", server.StatusInitializing)
	second := orphan(t, e, other, "reboot", server.StatusInitializing)
	reconcile.SetRescueHook(r, func(a int64) error {
		if a == first {
			return io.ErrUnexpectedEOF
		}
		return nil
	})
	e.App.PVE.SetDown(true)
	require.NoError(t, r.RescueAll(ctx), "a failing row does not stop startup")
	_, err := e.App.Jobs.FailOrphanedActions(ctx)
	require.NoError(t, err)
	expectOrphaned(t, e, second)
	require.Equal(t, server.StatusError, get(t, e, other).Status)
	a, err := actions.Get(ctx, e.App.Ent, e.Project, first)
	require.NoError(t, err)
	require.Equal(t, actions.StatusRunning, a.Status, "left for the periodic rescue")

	reconcile.SetRescueHook(r, nil)
	require.Error(t, r.Run(ctx, e.Location))
	expectOrphaned(t, e, first)
	require.Equal(t, server.StatusError, get(t, e, id).Status)
}

func TestRunLogsUnrestoredVMConfig(t *testing.T) {
	ctx := context.Background()
	e := queuedEnv(t)
	logs := &lockedBuffer{}
	r := newReconciler(e, slog.New(slog.NewTextHandler(logs, nil)))
	e.Admin.CreateServerType("big", 4, 4096, 40)
	id := build(t, e, "web")
	e.App.PVE.SetVMStatus(*get(t, e, id).Vmid, "stopped")
	require.NoError(t, r.Run(ctx, e.Location))
	res := e.C.Send(http.MethodPost, "/v1/servers/"+strconv.FormatInt(id, 10)+"/actions/change_type",
		map[string]any{"server_type": "big"})
	require.Equal(t, http.StatusCreated, res.StatusCode)
	var out struct {
		Action struct {
			ID int64 `json:"id"`
		} `json:"action"`
	}
	decode(t, res, &out)
	discard(t, e, out.Action.ID)
	e.App.PVE.SetDown(true)
	require.Error(t, r.Run(ctx, e.Location))
	expectOrphaned(t, e, out.Action.ID)
	require.Contains(t, logs.String(), "vm config not restored")
}

// A rescued change_type takes the disk size Proxmox reports.
func TestRescueRestoresActualDiskSize(t *testing.T) {
	ctx := context.Background()
	e := queuedEnv(t)
	e.Admin.CreateServerType("big", 4, 4096, 40)
	id := build(t, e, "web")
	vmid := *get(t, e, id).Vmid
	e.App.PVE.SetVMStatus(vmid, "stopped")
	require.NoError(t, e.App.Ent.Server.UpdateOneID(id).SetStatus(server.StatusOff).Exec(ctx))
	orphanChange := func() int64 {
		res := e.C.Send(http.MethodPost, "/v1/servers/"+strconv.FormatInt(id, 10)+"/actions/change_type",
			map[string]any{"server_type": "big", "upgrade_disk": true})
		require.Equal(t, http.StatusCreated, res.StatusCode)
		var out struct {
			Action struct {
				ID int64 `json:"id"`
			} `json:"action"`
		}
		decode(t, res, &out)
		require.Equal(t, 40, get(t, e, id).DiskGB)
		discard(t, e, out.Action.ID)
		return out.Action.ID
	}

	act := orphanChange()
	require.NoError(t, newReconciler(e, quiet()).Run(ctx, e.Location))
	expectOrphaned(t, e, act)
	got := get(t, e, id)
	require.Equal(t, e.Type, got.ServerTypeID)
	require.Equal(t, 2, got.Cores)
	require.Equal(t, 20, got.DiskGB, "the disk never grew")

	act = orphanChange()
	vm, ok := e.App.PVE.VM(vmid)
	require.True(t, ok)
	e.App.PVE.SetVMConfig(vmid, "scsi0", strings.Replace(vm.Config["scsi0"], "size=20G", "size=40G", 1))
	require.NoError(t, newReconciler(e, quiet()).Run(ctx, e.Location))
	expectOrphaned(t, e, act)
	require.Equal(t, 40, get(t, e, id).DiskGB, "the grown disk is kept")
}

func TestReconcileIgnoresTemplateBuildVMs(t *testing.T) {
	ctx := context.Background()
	e := workerEnv(t)
	logs := &lockedBuffer{}
	r := newReconciler(e, slog.New(slog.NewTextHandler(logs, nil)))
	row, _ := running(t, e, "web")
	pc, err := e.App.Catalog.ProxmoxFor(ctx, e.Location)
	require.NoError(t, err)
	e.App.PVE.AddVM("pve1", 600, "building")
	require.NoError(t, pc.SetConfig(ctx, "pve1", 600, map[string]string{"tags": "nebla-tpl;nebla-tpl-1-ab"}))

	require.NoError(t, r.Run(ctx, e.Location))
	require.NotContains(t, logs.String(), "vmid=600")
	require.Equal(t, server.StatusRunning, get(t, e, row.ID).Status)
	_, ok := e.App.PVE.VM(600)
	require.True(t, ok)
}

func TestRescueKeepsStatusForApply(t *testing.T) {
	ctx := context.Background()
	e := queuedEnv(t)
	r := newReconciler(e, quiet())
	id := build(t, e, "web")
	require.NoError(t, e.App.Ent.Server.UpdateOneID(id).SetStatus(server.StatusInitializing).Exec(ctx))
	fw := firewalls.New(firewalls.Options{SQL: e.App.SQL, Ent: e.App.Ent, Jobs: e.App.Jobs})
	_, acts, err := fw.Create(ctx, e.Project, firewalls.CreateInput{Name: "fw",
		ApplyTo: []firewalls.ResourceInput{{Type: "server", ServerID: &id}}})
	require.NoError(t, err)
	require.Len(t, acts, 1)
	discard(t, e, acts[0].ID)

	require.NoError(t, r.Run(ctx, e.Location))
	expectOrphaned(t, e, acts[0].ID)
	require.Equal(t, server.StatusInitializing, get(t, e, id).Status, "a lost apply leaves the status alone")
}

// applies lists the server's apply_firewalls actions, oldest first.
func applies(t *testing.T, e *testutil.ServerEnv, id int64) []*ent.Action {
	t.Helper()
	as, err := e.App.Ent.Action.Query().Where(servers.ActionsOf(e.Project, id), action.Command(firewalls.KindApply)).
		Order(action.ByID()).All(context.Background())
	require.NoError(t, err)
	return as
}

// settle waits for every apply_firewalls action on the server.
func settle(t *testing.T, e *testutil.ServerEnv, id int64) {
	t.Helper()
	for _, a := range applies(t, e, id) {
		got := testutil.WaitAction(t, e.App.Ent, a.ID)
		require.Equal(t, actions.StatusSuccess, got.Status, "%+v", got.Error)
	}
}

func sshFirewall(t *testing.T, e *testutil.ServerEnv, ids ...int64) {
	t.Helper()
	fw := firewalls.New(firewalls.Options{SQL: e.App.SQL, Ent: e.App.Ent, Jobs: e.App.Jobs})
	port := "22"
	in := firewalls.CreateInput{Name: "ssh", Rules: []firewalls.RuleInput{{Direction: "in", Protocol: "tcp", Port: &port, IPs: []string{"0.0.0.0/0"}}}}
	for _, id := range ids {
		in.ApplyTo = append(in.ApplyTo, firewalls.ResourceInput{Type: "server", ServerID: &id})
	}
	_, acts, err := fw.Create(context.Background(), e.Project, in)
	require.NoError(t, err)
	for _, a := range acts {
		got := testutil.WaitAction(t, e.App.Ent, a.ID)
		require.Equal(t, actions.StatusSuccess, got.Status, "%+v", got.Error)
	}
}

func vmFW(t *testing.T, e *testutil.ServerEnv, vmid int) fake.FakeVM {
	t.Helper()
	v, ok := e.App.PVE.VM(vmid)
	require.True(t, ok)
	return v
}

func TestReconcileMigratesV1Servers(t *testing.T) {
	ctx := context.Background()
	e := workerEnv(t)
	r := newReconciler(e, quiet())
	open, openVM := running(t, e, "open")
	guarded, guardedVM := running(t, e, "guarded")
	sshFirewall(t, e, guarded.ID)

	v1 := map[string]string{"enable": "1", "ipfilter": "1", "macfilter": "1", "policy_in": "ACCEPT", "policy_out": "ACCEPT"}
	for _, vmid := range []int{openVM, guardedVM} {
		e.App.PVE.SetVMFirewall(vmid, v1)
	}
	for _, id := range []int64{open.ID, guarded.ID} {
		require.NoError(t, e.App.Ent.Server.UpdateOneID(id).SetFirewallGenDesired(0).SetFirewallGenApplied(0).Exec(ctx))
	}
	before := vmFW(t, e, openVM)
	require.NotEmpty(t, before.IPSets["ipfilter-net0"])
	require.Contains(t, []string{"", "0"}, before.Firewall["dhcp"])
	baseline := len(applies(t, e, open.ID))

	var mu sync.Mutex
	var seen []map[string]string
	e.App.PVE.OnFirewallWrite(func() {
		v, _ := e.App.PVE.VM(openVM)
		mu.Lock()
		seen = append(seen, v.Firewall)
		mu.Unlock()
	})
	require.NoError(t, r.Run(ctx, e.Location))
	require.Len(t, applies(t, e, open.ID), baseline+1)
	settle(t, e, open.ID)
	settle(t, e, guarded.ID)
	e.App.PVE.OnFirewallWrite(nil)

	mu.Lock()
	require.NotEmpty(t, seen)
	for _, fw := range seen {
		require.Equal(t, "ACCEPT", fw["policy_in"], "a server with no firewall never gets DROP")
		require.Equal(t, "ACCEPT", fw["policy_out"])
		require.Contains(t, []string{"", "0"}, fw["dhcp"])
	}
	mu.Unlock()

	after := vmFW(t, e, openVM)
	want := map[string]string{"enable": "1", "ipfilter": "1", "macfilter": "1", "policy_in": "ACCEPT", "policy_out": "ACCEPT",
		"ndp": "1", "radv": "0", "dhcp": "0"}
	require.Equal(t, want, after.Firewall)
	require.Empty(t, after.Rules)
	require.Equal(t, before.IPSets, after.IPSets)
	require.GreaterOrEqual(t, get(t, e, open.ID).FirewallGenApplied, int64(1))

	g := vmFW(t, e, guardedVM)
	require.Equal(t, "DROP", g.Firewall["policy_in"])
	require.NotEmpty(t, g.Rules)
	require.GreaterOrEqual(t, get(t, e, guarded.ID).FirewallGenApplied, int64(1))

	// Migrated: the next pass finds nothing to do.
	n := len(applies(t, e, open.ID)) + len(applies(t, e, guarded.ID))
	require.NoError(t, r.Run(ctx, e.Location))
	require.Equal(t, n, len(applies(t, e, open.ID))+len(applies(t, e, guarded.ID)))
}

func TestReconcileRepairsForeignRule(t *testing.T) {
	ctx := context.Background()
	e := workerEnv(t)
	r := newReconciler(e, quiet())
	row, vmid := running(t, e, "web")
	pc, err := e.App.Catalog.ProxmoxFor(ctx, e.Location)
	require.NoError(t, err)
	require.NoError(t, pc.AddFirewallRule(ctx, row.Node, vmid, pve.FirewallRule{Type: "in", Action: "ACCEPT", Proto: "tcp", Dport: "3389", Enable: true}))
	baseline := len(applies(t, e, row.ID))

	require.NoError(t, r.Run(ctx, e.Location))
	as := applies(t, e, row.ID)
	require.Len(t, as, baseline+1)
	require.Equal(t, []actions.Resource{{ID: row.ID, Type: servers.ResourceType}}, as[len(as)-1].Resources)
	settle(t, e, row.ID)
	require.Empty(t, vmFW(t, e, vmid).Rules)
}

func TestReconcileRepairsPolicy(t *testing.T) {
	ctx := context.Background()
	e := workerEnv(t)
	r := newReconciler(e, quiet())
	row, vmid := running(t, e, "web")
	sshFirewall(t, e, row.ID)
	require.Equal(t, "DROP", vmFW(t, e, vmid).Firewall["policy_in"])
	pc, err := e.App.Catalog.ProxmoxFor(ctx, e.Location)
	require.NoError(t, err)
	require.NoError(t, pc.SetFirewallOptions(ctx, row.Node, vmid, map[string]string{"policy_in": "ACCEPT"}))
	baseline := len(applies(t, e, row.ID))

	require.NoError(t, r.Run(ctx, e.Location))
	require.Len(t, applies(t, e, row.ID), baseline+1)
	settle(t, e, row.ID)
	require.Equal(t, "DROP", vmFW(t, e, vmid).Firewall["policy_in"])
}

func TestReconcileNoopWhenInSync(t *testing.T) {
	ctx := context.Background()
	e := workerEnv(t)
	r := newReconciler(e, quiet())
	plain, _ := running(t, e, "plain")
	guarded, _ := running(t, e, "guarded")
	sshFirewall(t, e, guarded.ID)
	a, b := len(applies(t, e, plain.ID)), len(applies(t, e, guarded.ID))

	require.NoError(t, r.Run(ctx, e.Location))
	require.Len(t, applies(t, e, plain.ID), a)
	require.Len(t, applies(t, e, guarded.ID), b)
}

func TestReconcileSkipsPendingApply(t *testing.T) {
	ctx := context.Background()
	e := workerEnv(t)
	r := newReconciler(e, quiet())
	row, vmid := running(t, e, "web")
	pc, err := e.App.Catalog.ProxmoxFor(ctx, e.Location)
	require.NoError(t, err)
	foreign := pve.FirewallRule{Type: "in", Action: "ACCEPT", Proto: "tcp", Dport: "3389", Enable: true}
	require.NoError(t, pc.AddFirewallRule(ctx, row.Node, vmid, foreign))

	// A running apply whose job is still scheduled.
	var act actions.Action
	require.NoError(t, db.WithTx(ctx, e.App.SQL, func(tx *sql.Tx, etx *ent.Client) error {
		var err error
		act, err = e.App.Jobs.StartAction(ctx, tx, etx, e.Project, firewalls.KindApply,
			[]actions.Resource{{ID: row.ID, Type: servers.ResourceType}},
			func(int64) river.JobArgs { return jobs.PingArgs{} }, &river.InsertOpts{ScheduledAt: time.Now().Add(time.Hour)})
		return err
	}))
	baseline := len(applies(t, e, row.ID))
	require.NoError(t, r.Run(ctx, e.Location))
	require.Len(t, applies(t, e, row.ID), baseline)
	require.Len(t, vmFW(t, e, vmid).Rules, 1)
	require.NoError(t, actions.Succeed(ctx, e.App.Ent, act.ID))

	// A build in progress, a transient status, or an error server is left alone too.
	cases := []func(*ent.ServerUpdateOne){
		func(u *ent.ServerUpdateOne) { u.SetRebuildStep(2) },
		func(u *ent.ServerUpdateOne) { u.SetStatus(server.StatusStopping) },
		func(u *ent.ServerUpdateOne) { u.SetStatus(server.StatusError) },
	}
	for _, c := range cases {
		u := e.App.Ent.Server.UpdateOneID(row.ID)
		c(u)
		require.NoError(t, u.Exec(ctx))
		require.NoError(t, r.Run(ctx, e.Location))
		require.Len(t, applies(t, e, row.ID), baseline)
		require.NoError(t, e.App.Ent.Server.UpdateOneID(row.ID).SetStatus(server.StatusRunning).SetRebuildStep(0).Exec(ctx))
	}
	require.NoError(t, r.Run(ctx, e.Location))
	require.Len(t, applies(t, e, row.ID), baseline+1, "repaired once nothing holds it")
}

func TestReconcileRecordsClusterFirewall(t *testing.T) {
	ctx := context.Background()
	e := workerEnv(t)
	r := newReconciler(e, quiet())
	loc, err := e.App.Catalog.GetLocation(ctx, e.Location)
	require.NoError(t, err)
	require.Nil(t, loc.FirewallEnabled)

	require.NoError(t, r.Run(ctx, e.Location))
	loc, err = e.App.Catalog.GetLocation(ctx, e.Location)
	require.NoError(t, err)
	require.NotNil(t, loc.FirewallEnabled)
	require.True(t, *loc.FirewallEnabled)

	// With the datacenter firewall off, a guarded server's drift is not queued: the apply could only fail.
	row, vmid := running(t, e, "web")
	sshFirewall(t, e, row.ID)
	e.App.PVE.SetClusterFirewall(false)
	pc, err := e.App.Catalog.ProxmoxFor(ctx, e.Location)
	require.NoError(t, err)
	require.NoError(t, pc.SetFirewallOptions(ctx, row.Node, vmid, map[string]string{"policy_in": "ACCEPT"}))
	baseline := len(applies(t, e, row.ID))
	require.NoError(t, r.Run(ctx, e.Location))
	loc, err = e.App.Catalog.GetLocation(ctx, e.Location)
	require.NoError(t, err)
	require.NotNil(t, loc.FirewallEnabled)
	require.False(t, *loc.FirewallEnabled)
	require.Len(t, applies(t, e, row.ID), baseline)
}

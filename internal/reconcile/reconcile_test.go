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

	"github.com/Hexeption/nebla/internal/actions"
	"github.com/Hexeption/nebla/internal/db"
	"github.com/Hexeption/nebla/internal/ent"
	"github.com/Hexeption/nebla/internal/ent/node"
	"github.com/Hexeption/nebla/internal/ent/server"
	"github.com/Hexeption/nebla/internal/httpapi"
	"github.com/Hexeption/nebla/internal/jobs"
	"github.com/Hexeption/nebla/internal/pve/fake"
	"github.com/Hexeption/nebla/internal/reconcile"
	"github.com/Hexeption/nebla/internal/servers"
	"github.com/Hexeption/nebla/internal/testutil"
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
	return reconcile.New(reconcile.Options{SQL: e.App.SQL, Ent: e.App.Ent, Catalog: e.App.Catalog, Logger: log})
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
	require.NoError(t, r.Run(ctx, e.Location))
	expectOrphaned(t, e, out.Action.ID)
	got := get(t, e, s.ServerID)
	require.Equal(t, e.Type, got.ServerTypeID)
	require.Equal(t, 2, got.Cores)
	require.Equal(t, 2048, got.MemoryMB)
	require.Equal(t, 20, got.DiskGB)
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

// Copyright (C) 2026 The Nebla contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package backups_test

import (
	"context"
	"testing"
	"time"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/backups"
	entaction "github.com/nebla-cloud/nebla/internal/ent/action"
	entbackup "github.com/nebla-cloud/nebla/internal/ent/backup"
	"github.com/nebla-cloud/nebla/internal/ent/server"
	"github.com/nebla-cloud/nebla/internal/ext"
	"github.com/nebla-cloud/nebla/internal/reconcile"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/nebla-cloud/nebla/internal/volumes"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/require"
)

func TestRescueBackups(t *testing.T) {
	ctx := context.Background()
	e := newJobEnv(t)
	require.NoError(t, e.App.Ent.Project.UpdateOneID(e.Project).SetMaxBackups(0).Exec(ctx))
	a, b, c, d := e.running(t, "a"), e.running(t, "b"), e.running(t, "c"), e.running(t, "d")
	// A creating backup whose job was lost, and a queued one whose job is live.
	lost := e.put(t, a, entbackup.StatusCreating)
	live := e.queue(t, b)
	// A delete that lost its action, and one still running.
	stuck := e.put(t, c, entbackup.StatusDeleting)
	deleting := e.put(t, d, entbackup.StatusAvailable)
	_, err := e.svc.Delete(ctx, e.Project, d, deleting.ID)
	require.NoError(t, err)

	for range 2 {
		require.NoError(t, backups.RescueOrphaned(ctx, e.App.SQL, e.App.Jobs))
	}
	require.Equal(t, 1, e.createJobs(t, lost.ID))
	require.Equal(t, entbackup.StatusCreating, e.App.Ent.Backup.GetX(ctx, lost.ID).Status)
	require.Equal(t, 1, e.createJobs(t, live))
	require.Equal(t, entbackup.StatusAvailable, e.App.Ent.Backup.GetX(ctx, stuck.ID).Status)
	require.Equal(t, entbackup.StatusDeleting, e.App.Ent.Backup.GetX(ctx, deleting.ID).Status)

	// The re-enqueued job finishes the backup.
	require.NoError(t, e.work(t, lost.ID))
	require.Equal(t, entbackup.StatusAvailable, e.App.Ent.Backup.GetX(ctx, lost.ID).Status)
}

func TestBackupUsageEvents(t *testing.T) {
	ctx := context.Background()
	e := newJobEnv(t)
	a, b := e.running(t, "a"), e.running(t, "b")
	avail := e.put(t, a, entbackup.StatusAvailable)
	require.NoError(t, e.App.Ent.Backup.UpdateOneID(avail.ID).SetSizeBytes(42).Exec(ctx))
	restoring := e.put(t, b, entbackup.StatusRestoring)
	for _, st := range []entbackup.Status{entbackup.StatusQueued, entbackup.StatusError, entbackup.StatusMissing} {
		e.put(t, a, st)
	}
	e.put(t, b, entbackup.StatusCreating)

	w := backups.NewUsageWorker(e.o)
	created := time.Date(2026, 11, 19, 14, 59, 59, 0, time.UTC)
	require.NoError(t, w.Work(ctx, &river.Job[backups.UsageArgs]{JobRow: &rivertype.JobRow{CreatedAt: created}}))
	evs := e.rec.of(ext.BackupUsage)
	require.Len(t, evs, 2)
	byID := map[int64]ext.Event{}
	for _, ev := range evs {
		require.Equal(t, e.Project, ev.ProjectID)
		require.Equal(t, backups.ResourceType, ev.ResourceType)
		byID[ev.ResourceID] = ev
	}
	period := "2026-11-19T14:00:00Z"
	require.Equal(t, map[string]any{"server_id": a, "size_bytes": int64(42), "kind": "manual", "location_id": e.Location,
		"period": period, "status": "available"}, byID[avail.ID].Data)
	require.Equal(t, "restoring", byID[restoring.ID].Data["status"])

	srv, own := volumes.UsageArgs{}.InsertOpts(), backups.UsageArgs{}.InsertOpts()
	require.Equal(t, srv.UniqueOpts.ByPeriod, own.UniqueOpts.ByPeriod)
	require.Equal(t, srv.UniqueOpts.ByState, own.UniqueOpts.ByState)
	require.Equal(t, srv.Queue, own.Queue)
	require.NotNil(t, backups.UsagePeriodicJob())
	require.NotNil(t, backups.SchedulePeriodicJob())
	require.NotEqual(t, servers.KindUsage, backups.UsageArgs{}.Kind())
	require.Equal(t, "schedule_backups", backups.ScheduleArgs{}.Kind())
	require.Equal(t, 2*time.Hour, backups.NewCreateBackupWorker(e.o).Timeout(nil))
	require.Equal(t, servers.InsertOpts(), backups.CreateBackupArgs{}.InsertOpts())
}

// lostBackup is a creating backup whose job is gone while its action still runs, as after a crash.
func (e *jenv) lostBackup(t *testing.T, serverID int64) (backupID, actionID int64) {
	t.Helper()
	ctx := context.Background()
	b := e.put(t, serverID, entbackup.StatusCreating)
	act, err := actions.Create(ctx, e.App.Ent, e.Project, backups.KindCreateBackup, []actions.Resource{{ID: serverID, Type: servers.ResourceType}})
	require.NoError(t, err)
	require.NoError(t, actions.AttachJob(ctx, e.App.Ent, act.ID, 999999))
	return b.ID, act.ID
}

func TestReconcileFailsLostBackupAction(t *testing.T) {
	ctx := context.Background()
	e := newJobEnv(t)
	rec := reconcile.New(reconcile.Options{SQL: e.App.SQL, Ent: e.App.Ent, Catalog: e.App.Catalog, Logger: quiet()})
	a, b := e.running(t, "a"), e.running(t, "b")
	vmidB := *e.App.Ent.Server.GetX(ctx, b).Vmid
	e.App.PVE.SetVMStatus(vmidB, "stopped")
	require.NoError(t, e.App.Ent.Server.UpdateOneID(b).SetStatus(server.StatusOff).Exec(ctx))

	// Startup: the server rescue fails the lost action and leaves the server's status alone.
	ba, actA := e.lostBackup(t, a)
	require.NoError(t, rec.RescueAll(ctx))
	require.Equal(t, entaction.StatusError, e.App.Ent.Action.GetX(ctx, actA).Status)
	require.Equal(t, server.StatusRunning, e.App.Ent.Server.GetX(ctx, a).Status)

	// The periodic pass does the same.
	bb, actB := e.lostBackup(t, b)
	require.NoError(t, rec.Run(ctx, e.Location))
	require.Equal(t, entaction.StatusError, e.App.Ent.Action.GetX(ctx, actB).Status)
	require.Equal(t, server.StatusOff, e.App.Ent.Server.GetX(ctx, b).Status)

	// Re-enqueued, the backups take the now free server lock and finish.
	require.NoError(t, backups.RescueOrphaned(ctx, e.App.SQL, e.App.Jobs))
	for _, id := range []int64{ba, bb} {
		require.NoError(t, e.work(t, id))
		require.Equal(t, entbackup.StatusAvailable, e.App.Ent.Backup.GetX(ctx, id).Status)
	}
}

func TestRescueReleasesLostRestores(t *testing.T) {
	ctx := context.Background()
	e := newJobEnv(t)
	a, b, c := e.running(t, "a"), e.running(t, "b"), e.running(t, "c")
	// A restore whose action ended outside its job, one still running, and one whose action runs but whose job died.
	lost := e.put(t, a, entbackup.StatusRestoring)
	live := e.put(t, b, entbackup.StatusAvailable)
	_, err := e.svc.Restore(ctx, e.Project, b, live.ID)
	require.NoError(t, err)
	dead := e.put(t, c, entbackup.StatusAvailable)
	act, err := e.svc.Restore(ctx, e.Project, c, dead.ID)
	require.NoError(t, err)
	_, err = e.App.SQL.ExecContext(ctx, `update river_job set state = 'discarded', finalized_at = now() where id = $1`, *act.RiverJobID)
	require.NoError(t, err)

	require.NoError(t, backups.RescueOrphaned(ctx, e.App.SQL, e.App.Jobs))
	require.Equal(t, entbackup.StatusAvailable, e.App.Ent.Backup.GetX(ctx, lost.ID).Status)
	require.Equal(t, entbackup.StatusRestoring, e.App.Ent.Backup.GetX(ctx, live.ID).Status)
	require.Equal(t, entbackup.StatusAvailable, e.App.Ent.Backup.GetX(ctx, dead.ID).Status)

	// The periodic scheduler does the same once the action fails.
	require.NoError(t, actions.Fail(ctx, e.App.Ent, e.restoreActionOf(t, b), actions.CodeFailed, "lost"))
	e.schedule(t, time.Now())
	require.Equal(t, entbackup.StatusAvailable, e.App.Ent.Backup.GetX(ctx, live.ID).Status)
}

// restoreActionOf is the id of the server's running restore_backup action.
func (e *jenv) restoreActionOf(t *testing.T, serverID int64) int64 {
	t.Helper()
	ids, err := e.App.Ent.Action.Query().Where(entaction.Command(servers.KindRestoreBackup), entaction.StatusEQ(entaction.StatusRunning)).IDs(context.Background())
	require.NoError(t, err)
	for _, id := range ids {
		a, err := actions.Get(context.Background(), e.App.Ent, e.Project, id)
		require.NoError(t, err)
		for _, r := range a.Resources {
			if r.ID == serverID {
				return id
			}
		}
	}
	t.Fatalf("no restore action for server %d", serverID)
	return 0
}

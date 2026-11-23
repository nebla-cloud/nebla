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

	"github.com/nebla-cloud/nebla/internal/backups"
	"github.com/nebla-cloud/nebla/internal/catalog"
	entbackup "github.com/nebla-cloud/nebla/internal/ent/backup"
	"github.com/nebla-cloud/nebla/internal/ent/server"
	"github.com/riverqueue/river"
	"github.com/stretchr/testify/require"
)

// schedule runs one scheduler pass at now.
func (e *jenv) schedule(t *testing.T, now time.Time) {
	t.Helper()
	w := backups.NewScheduleWorker(backups.SetNow(e.o, func() time.Time { return now }))
	require.NoError(t, w.Work(context.Background(), &river.Job[backups.ScheduleArgs]{}))
}

func (e *jenv) scheduled(t *testing.T, serverID int64) []entbackupRow {
	t.Helper()
	rows, err := e.App.Ent.Backup.Query().Where(entbackup.ServerID(serverID), entbackup.KindEQ(entbackup.KindScheduled)).
		Order(entbackup.ByID()).All(context.Background())
	require.NoError(t, err)
	out := make([]entbackupRow, len(rows))
	for i, r := range rows {
		out[i] = entbackupRow{r.ID, r.Status, r.CreatedAt}
	}
	return out
}

type entbackupRow struct {
	ID      int64
	Status  entbackup.Status
	Created time.Time
}

func (e *jenv) createJobs(t *testing.T, backupID int64) int {
	t.Helper()
	var n int
	require.NoError(t, e.App.SQL.QueryRow(`select count(*) from river_job where kind = 'create_backup'
		and (args->>'backup_id')::bigint = $1`, backupID).Scan(&n))
	return n
}

// dueAfter is the server's first due time after from.
func (e *jenv) dueAfter(t *testing.T, serverID int64, from time.Time) time.Time {
	t.Helper()
	loc, err := e.App.Catalog.GetLocation(context.Background(), e.Location)
	require.NoError(t, err)
	return backups.NextDue(from, loc, e.App.Ent.Server.GetX(context.Background(), serverID).BackupSlot)
}

func (e *jenv) enabledAt(t *testing.T, serverID int64, at time.Time) {
	t.Helper()
	require.NoError(t, e.App.Ent.Server.UpdateOneID(serverID).SetBackupsEnabledAt(at).Exec(context.Background()))
}

func TestScheduleOncePerDay(t *testing.T) {
	ctx := context.Background()
	e := newJobEnv(t)
	id := e.running(t, "a")
	off := e.RunningServer("off").ServerID
	due := e.dueAfter(t, id, at(20, 0, 0))
	// On since after yesterday's slot.
	e.enabledAt(t, id, due.Add(-time.Hour))

	e.schedule(t, due.Add(-time.Minute))
	require.Empty(t, e.scheduled(t, id), "not due yet")

	now := due.Add(time.Minute)
	e.schedule(t, now)
	rows := e.scheduled(t, id)
	require.Len(t, rows, 1)
	require.Equal(t, entbackup.StatusQueued, rows[0].Status)
	require.Equal(t, 1, e.createJobs(t, rows[0].ID))
	require.Empty(t, e.scheduled(t, off), "backups are off")
	b := e.App.Ent.Backup.GetX(ctx, rows[0].ID)
	require.Equal(t, "pbs", b.Storage)
	require.Equal(t, "pve1", b.Node)

	e.schedule(t, now.Add(time.Minute))
	require.Len(t, e.scheduled(t, id), 1, "one pending backup per server")

	// Done today: nothing more until tomorrow's slot.
	require.NoError(t, e.App.Ent.Backup.UpdateOneID(rows[0].ID).SetStatus(entbackup.StatusAvailable).SetVolid("pbs:backup/vm/1/x").Exec(ctx))
	e.schedule(t, now.Add(6*time.Hour))
	e.schedule(t, due.Add(24*time.Hour-time.Minute))
	require.Len(t, e.scheduled(t, id), 1)

	// A failed run leaves no row, and prune can remove a good one: either still counts as the day's backup.
	for i, st := range []string{"error", "success"} {
		day := due.Add(time.Duration(24*(i+1))*time.Hour + time.Minute)
		e.schedule(t, day)
		rows = e.scheduled(t, id)
		require.Len(t, rows, 2)
		require.Equal(t, entbackup.StatusQueued, rows[1].Status)
		require.NoError(t, e.App.Ent.Backup.DeleteOneID(rows[1].ID).Exec(ctx))
		_, err := e.App.SQL.Exec(`insert into actions (project_id, command, status, progress, resources, started, finished)
			values ($1, 'create_backup', $2, 0, jsonb_build_array(jsonb_build_object('id', $3::bigint, 'type', 'server')), $4, $4)`,
			e.Project, st, id, day)
		require.NoError(t, err)
		e.schedule(t, day.Add(5*time.Minute))
		require.Len(t, e.scheduled(t, id), 1, st)
	}

	// Servers that cannot be backed up now are skipped.
	next := due.Add(72*time.Hour + time.Minute)
	require.NoError(t, e.App.Ent.Server.UpdateOneID(id).SetStatus(server.StatusRebuilding).Exec(ctx))
	e.schedule(t, next)
	require.Len(t, e.scheduled(t, id), 1)
	require.NoError(t, e.App.Ent.Server.UpdateOneID(id).SetStatus(server.StatusRunning).Exec(ctx))
	// So are locations whose backup storage stopped working.
	require.NoError(t, e.App.Ent.Location.UpdateOneID(e.Location).SetBackupStorageOk(false).Exec(ctx))
	e.schedule(t, next)
	require.Len(t, e.scheduled(t, id), 1)
	require.NoError(t, e.App.Ent.Location.UpdateOneID(e.Location).SetBackupStorageOk(true).Exec(ctx))
	e.schedule(t, next)
	require.Len(t, e.scheduled(t, id), 2)
}

func TestScheduleRespectsEnabledAt(t *testing.T) {
	e := newJobEnv(t)
	id := e.running(t, "a")
	due := e.dueAfter(t, id, at(20, 0, 0))
	// Turned on after today's slot: the first backup is tomorrow's.
	e.enabledAt(t, id, due.Add(time.Minute))
	e.schedule(t, due.Add(2*time.Minute))
	require.Empty(t, e.scheduled(t, id))
	e.schedule(t, due.Add(24*time.Hour-time.Minute))
	require.Empty(t, e.scheduled(t, id))
	e.schedule(t, due.Add(24*time.Hour))
	require.Len(t, e.scheduled(t, id), 1)
}

func TestScheduleInsideWindow(t *testing.T) {
	loc := catalog.Location{BackupWindowStart: 120, BackupWindowEnd: 360}
	for id := int64(1); id <= 2000; id++ {
		due := backups.NextDue(at(23, 0, 0), loc, int(id*7919%1440))
		require.False(t, due.Before(at(23, 2, 0)), "%d", id)
		require.True(t, due.Before(at(23, 6, 0)), "%d", id)
	}
}

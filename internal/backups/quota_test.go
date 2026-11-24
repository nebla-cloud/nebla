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
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/backups"
	"github.com/nebla-cloud/nebla/internal/db"
	"github.com/nebla-cloud/nebla/internal/ent"
	entbackup "github.com/nebla-cloud/nebla/internal/ent/backup"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/require"
)

// covered requires the project's reservation to cover every stored backup.
func (e *jenv) covered(t *testing.T, stored int) {
	t.Helper()
	u := e.usage(t)
	require.Equal(t, stored, u.Backups)
	require.GreaterOrEqual(t, u.BackupsReserved, u.Backups, "%+v", u)
}

func (e *jenv) setBackups(t *testing.T, serverID int64, on bool, keep int) {
	t.Helper()
	_, err := e.srv.Update(context.Background(), e.Project, serverID, servers.UpdateInput{Backups: &servers.BackupSettings{Enabled: on, Keep: keep}})
	require.NoError(t, err)
}

func TestReservationCoversStored(t *testing.T) {
	ctx := context.Background()
	e := newJobEnv(t)
	id := e.running(t, "a")
	e.covered(t, 0)
	for range 3 {
		e.backUp(t, id)
	}
	e.covered(t, 3)

	// Lowering keep leaves the extra backups until the next prune.
	e.setBackups(t, id, true, 1)
	e.covered(t, 3)
	require.Equal(t, 3, e.usage(t).BackupsReserved)

	// Turning backups off drops the queued one, so it cannot land afterwards.
	bid := e.queue(t, id)
	e.setBackups(t, id, false, 1)
	require.False(t, e.App.Ent.Backup.Query().Where(entbackup.ID(bid)).ExistX(ctx), "queued backup dropped")
	require.NoError(t, e.work(t, bid))
	require.Zero(t, e.vzdumps()-3)
	e.covered(t, 3)

	e.setBackups(t, id, true, 1)
	e.covered(t, 3)

	act, err := e.srv.Delete(ctx, e.Project, id)
	require.NoError(t, err)
	e.covered(t, 3)
	a, err := actions.Get(ctx, e.App.Ent, e.Project, act.ID)
	require.NoError(t, err)
	j, err := e.App.Jobs.Client.JobGet(ctx, *a.RiverJobID)
	require.NoError(t, err)
	var args servers.DeleteServerArgs
	require.NoError(t, json.Unmarshal(j.EncodedArgs, &args))
	w := servers.NewDeleteServerWorker(servers.WorkerOptions{SQL: e.App.SQL, Ent: e.App.Ent, Catalog: e.App.Catalog, Logger: quiet()})
	require.NoError(t, w.Work(ctx, &river.Job[servers.DeleteServerArgs]{JobRow: &rivertype.JobRow{ID: j.ID, Attempt: 1}, Args: args}))
	e.covered(t, 0)
	require.Zero(t, e.usage(t).BackupsReserved)
}

func TestCreateBackupDropsQueuedWhenOff(t *testing.T) {
	ctx := context.Background()
	e := newJobEnv(t)
	id := e.running(t, "a")
	// A backup queued after the switch read the server, as if it raced turning backups off.
	require.NoError(t, e.App.Ent.Server.UpdateOneID(id).SetBackupsEnabled(false).Exec(ctx))
	b := e.put(t, id, entbackup.StatusQueued)
	require.NoError(t, db.WithTx(ctx, e.App.SQL, func(tx *sql.Tx, _ *ent.Client) error {
		_, err := e.App.Jobs.InsertTx(ctx, tx, backups.CreateBackupArgs{BackupID: b.ID}, nil)
		return err
	}))

	require.NoError(t, e.work(t, b.ID))
	require.False(t, e.App.Ent.Backup.Query().Where(entbackup.ID(b.ID)).ExistX(ctx))
	require.Zero(t, e.vzdumps())
	e.noBackupAction(t, id)
	e.slotFree(t, "pve1")
}

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
	"testing"

	"github.com/nebla-cloud/nebla/internal/backups"
	"github.com/nebla-cloud/nebla/internal/db"
	"github.com/nebla-cloud/nebla/internal/ent"
	entbackup "github.com/nebla-cloud/nebla/internal/ent/backup"
	"github.com/nebla-cloud/nebla/internal/ent/server"
	"github.com/stretchr/testify/require"
)

// interrupted is a creating backup whose vzdump wrote an archive before a crash, with no volid recorded, and its job.
func (e *jenv) interrupted(t *testing.T, serverID int64) (*ent.Backup, string) {
	t.Helper()
	ctx := context.Background()
	b := e.put(t, serverID, entbackup.StatusCreating)
	row := e.App.Ent.Server.GetX(ctx, serverID)
	pc := e.pc(t)
	up, err := pc.Vzdump(ctx, row.Node, *row.Vmid, "pbs", backups.Notes(serverID, b.ID))
	require.NoError(t, err)
	require.NoError(t, pc.WaitTask(ctx, row.Node, up))
	items := e.archives(t)
	require.Len(t, items, 1)
	require.NoError(t, db.WithTx(ctx, e.App.SQL, func(tx *sql.Tx, _ *ent.Client) error {
		_, err := e.App.Jobs.InsertTx(ctx, tx, backups.CreateBackupArgs{BackupID: b.ID}, nil)
		return err
	}))
	return b, items[0].Volid
}

func TestCreateBackupDropRemovesLeftArchive(t *testing.T) {
	ctx := context.Background()
	e := newJobEnv(t)
	id := e.running(t, "a")
	b, _ := e.interrupted(t, id)
	require.NoError(t, e.App.Ent.Server.UpdateOneID(id).SetStatus(server.StatusRebuilding).Exec(ctx))

	require.NoError(t, e.work(t, b.ID))
	require.False(t, e.App.Ent.Backup.Query().Where(entbackup.ID(b.ID)).ExistX(ctx))
	require.Empty(t, e.archives(t), "archive found by its notes and deleted")
	require.Zero(t, e.App.Ent.BackupTombstone.Query().CountX(ctx))
}

func TestCreateBackupDropTombstonesLeftArchive(t *testing.T) {
	ctx := context.Background()
	e := newJobEnv(t)
	id := e.running(t, "a")
	b, volid := e.interrupted(t, id)
	require.NoError(t, e.App.Ent.Server.UpdateOneID(id).SetStatus(server.StatusRebuilding).Exec(ctx))
	e.App.PVE.FailNext(volid, 500)

	require.NoError(t, e.work(t, b.ID))
	require.False(t, e.App.Ent.Backup.Query().Where(entbackup.ID(b.ID)).ExistX(ctx))
	require.Len(t, e.archives(t), 1)
	ts := e.App.Ent.BackupTombstone.Query().OnlyX(ctx)
	require.Equal(t, volid, ts.Volid)
	require.Equal(t, "pbs", ts.Storage)
}

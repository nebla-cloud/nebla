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

package servers_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/ent/backuptombstone"
	"github.com/nebla-cloud/nebla/internal/ext"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/stretchr/testify/require"
)

func (e *restoreEnv) deleteServer(t *testing.T) actions.Action {
	t.Helper()
	ctx := context.Background()
	del, _ := expect[actionBody](t, e.C.Send(http.MethodDelete, spath(e.s.ServerID), nil), http.StatusOK)
	o := opts(e.ServerEnv)
	o.Bus = e.bus
	require.NoError(t, servers.NewDeleteServerWorker(o).Work(ctx, jobOf(queuedArgs[servers.DeleteServerArgs](t, e.ServerEnv, del.Action.ID), 1)))
	a, err := actions.Get(ctx, e.App.Ent, e.Project, del.Action.ID)
	require.NoError(t, err)
	require.Equal(t, actions.StatusSuccess, a.Status, "%+v", a.Error)
	return a
}

func (e *restoreEnv) archives(t *testing.T) int {
	t.Helper()
	pc, err := e.App.Catalog.ProxmoxFor(context.Background(), e.Location)
	require.NoError(t, err)
	items, err := pc.BackupContent(context.Background(), "pve1", "pbs", 0)
	require.NoError(t, err)
	return len(items)
}

func TestDeleteServerDeletesBackups(t *testing.T) {
	ctx := context.Background()
	e := newRestoreEnv(t)
	e.backUp(t, e.sameNet(t))
	e.backUp(t, e.sameNet(t))
	require.Equal(t, 2, e.archives(t))

	e.deleteServer(t)
	require.Zero(t, e.archives(t), "archives deleted")
	require.Zero(t, e.App.Ent.Backup.Query().CountX(ctx))
	require.Zero(t, e.App.Ent.BackupTombstone.Query().CountX(ctx))
	require.Len(t, e.bus.of(ext.BackupDeleted), 2)
}

func TestDeleteServerTombstonesOnFailure(t *testing.T) {
	ctx := context.Background()
	e := newRestoreEnv(t)
	b := e.backUp(t, e.sameNet(t))
	e.App.PVE.FailNext(*b.Volid, 500)

	e.deleteServer(t)
	require.Equal(t, 1, e.archives(t), "archive left behind")
	require.Zero(t, e.App.Ent.Backup.Query().CountX(ctx))
	ts := e.App.Ent.BackupTombstone.Query().Where(backuptombstone.Volid(*b.Volid)).OnlyX(ctx)
	require.Equal(t, "pbs", ts.Storage)
	require.Equal(t, e.Location, ts.LocationID)
	require.Len(t, e.bus.of(ext.BackupDeleted), 1)
}

func TestDeleteDuringBackup409(t *testing.T) {
	ctx := context.Background()
	e := newRestoreEnv(t)
	_, err := actions.Create(ctx, e.App.Ent, e.Project, servers.KindCreateBackup, []actions.Resource{{ID: e.s.ServerID, Type: servers.ResourceType}})
	require.NoError(t, err)
	res := e.C.Send(http.MethodDelete, spath(e.s.ServerID), nil)
	require.Equal(t, http.StatusConflict, res.StatusCode)
	require.Contains(t, body(t, res), "backup_in_progress")
}

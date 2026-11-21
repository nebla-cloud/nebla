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
	"github.com/nebla-cloud/nebla/internal/ent/server"
	"github.com/nebla-cloud/nebla/internal/ipam"
	"github.com/nebla-cloud/nebla/internal/reconcile"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestDeleteAlreadyGoneVM(t *testing.T) {
	ctx := context.Background()
	e := workerEnv(t, nil)
	s := e.CreateServer("web", nil)
	waitOK(t, e, s.ActionID)
	vmid := vmidOf(t, e, s.ServerID)
	n, err := e.App.Ent.IPAllocation.Query().Count(ctx)
	require.NoError(t, err)
	require.Positive(t, n)

	e.App.PVE.DeleteVM(vmid)
	r := reconcile.New(reconcile.Options{SQL: e.App.SQL, Ent: e.App.Ent, Catalog: e.App.Catalog, Logger: quiet()})
	require.NoError(t, r.Run(ctx, e.Location))
	require.Equal(t, servers.StatusMissing, status(t, e, s.ServerID))

	del, _ := expect[actionBody](t, e.C.Send(http.MethodDelete, spath(s.ServerID), nil), http.StatusOK)
	a := waitOK(t, e, del.Action.ID)
	require.Equal(t, "delete_server", a.Command)
	_, err = e.App.Ent.Server.Get(ctx, s.ServerID)
	require.Error(t, err, "row deleted")
	n, err = e.App.Ent.IPAllocation.Query().Count(ctx)
	require.NoError(t, err)
	require.Zero(t, n, "ips released")
}

// R28: a lost create leaves the row in error with no VM and no IPs; DELETE just removes it.
func TestDeleteHalfCreatedServer(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	s := e.CreateServer("web", nil)
	require.NoError(t, actions.Fail(ctx, e.App.Ent, s.ActionID, "orphaned", "lost"))
	require.NoError(t, ipam.Release(ctx, e.App.Ent, s.ServerID))
	require.NoError(t, e.App.Ent.Server.UpdateOneID(s.ServerID).SetStatus(server.StatusError).Exec(ctx))
	require.Nil(t, vmidOrNil(t, e, s.ServerID))
	before := len(e.App.PVE.Calls())

	del, _ := expect[actionBody](t, e.C.Send(http.MethodDelete, spath(s.ServerID), nil), http.StatusOK)
	w := servers.NewDeleteServerWorker(opts(e))
	require.NoError(t, w.Work(ctx, jobOf(queuedArgs[servers.DeleteServerArgs](t, e, del.Action.ID), 1)))
	a, err := actions.Get(ctx, e.App.Ent, e.Project, del.Action.ID)
	require.NoError(t, err)
	require.Equal(t, actions.StatusSuccess, a.Status, "%+v", a.Error)
	_, err = e.App.Ent.Server.Get(ctx, s.ServerID)
	require.Error(t, err, "row deleted")
	for _, c := range e.App.PVE.Calls()[before:] {
		require.NotContains(t, c, "/qemu/", "no VM calls: %s", c)
	}
}

func vmidOrNil(t *testing.T, e *testutil.ServerEnv, id int64) *int {
	t.Helper()
	row, err := e.App.Ent.Server.Get(context.Background(), id)
	require.NoError(t, err)
	return row.Vmid
}

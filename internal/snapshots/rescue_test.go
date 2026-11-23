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

package snapshots_test

import (
	"context"
	"testing"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/ent/snapshot"
	"github.com/nebla-cloud/nebla/internal/reconcile"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/nebla-cloud/nebla/internal/snapshots"
	"github.com/stretchr/testify/require"
)

// orphan cancels the action's River job without settling the action, as if it was lost.
func (e *env) orphan(t *testing.T, a actions.Action) {
	t.Helper()
	_, err := e.App.SQL.ExecContext(context.Background(),
		`update river_job set state = 'cancelled', finalized_at = now() where id = $1`, *e.action(t, a.ID).RiverJobID)
	require.NoError(t, err)
}

func TestRescueSnapshotActions(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	// A create lost after the template VM was made, with the server stopped.
	s, vmid := e.bootServer(t, "web1")
	creating, create := e.take(t, "web1", false)
	e.crashAfter(t, e.opts(nil), create, 4)
	e.requireServer(t, s.ServerID, vmid, servers.StatusOff)
	require.Len(t, e.templates(t, creating.ID), 1)
	e.orphan(t, create)
	// Booted first: the fake puts server VMs at 100+id, where a later template could sit.
	e.bootServer(t, "web3")
	// A delete lost before it ran.
	gone := e.available(t, "web2")
	del, err := e.svc.Delete(ctx, e.Project, gone.ID)
	require.NoError(t, err)
	e.orphan(t, del)
	// A live create is left alone.
	_, live := e.take(t, "web3", false)
	liveJob := *live.RiverJobID

	// The server rescue leaves snapshot commands to the snapshot rescue.
	rec := reconcile.New(reconcile.Options{SQL: e.App.SQL, Ent: e.App.Ent, Catalog: e.App.Catalog, Logger: quiet()})
	require.NoError(t, rec.RescueAll(ctx))
	require.Equal(t, actions.StatusRunning, e.action(t, create.ID).Status)

	n, err := snapshots.RescueOrphaned(ctx, e.App.Ent, e.App.SQL, nil)
	require.NoError(t, err)
	require.Equal(t, 2, n)
	require.Equal(t, actions.StatusRunning, e.action(t, create.ID).Status, "the compensation job finishes it")
	require.Equal(t, actions.StatusRunning, e.action(t, del.ID).Status)
	require.NotEqual(t, *create.RiverJobID, *e.action(t, create.ID).RiverJobID)
	require.NotEqual(t, *del.RiverJobID, *e.action(t, del.ID).RiverJobID)
	require.Equal(t, liveJob, *e.action(t, live.ID).RiverJobID)

	// Nothing is left to rescue while the new jobs live.
	n, err = snapshots.RescueOrphaned(ctx, e.App.Ent, e.App.SQL, nil)
	require.NoError(t, err)
	require.Zero(t, n)

	a := e.finish(t, e.opts(nil), create.ID)
	require.Equal(t, actions.StatusError, a.Status)
	require.Equal(t, actions.CodeOrphaned, a.Error.Code)
	e.requireServer(t, s.ServerID, vmid, servers.StatusRunning)
	require.False(t, e.App.Ent.Snapshot.Query().Where(snapshot.ID(creating.ID)).ExistX(ctx))
	require.Empty(t, e.templates(t, creating.ID))

	a = e.finish(t, e.opts(nil), del.ID)
	require.Equal(t, actions.StatusSuccess, a.Status, "%+v", a.Error)
	require.False(t, e.App.Ent.Snapshot.Query().Where(snapshot.ID(gone.ID)).ExistX(ctx))
	_, ok := e.App.PVE.VM(gone.VMID)
	require.False(t, ok)
}

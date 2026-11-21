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

package volumes_test

import (
	"context"
	"testing"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/ent/volume"
	"github.com/nebla-cloud/nebla/internal/ext"
	"github.com/nebla-cloud/nebla/internal/reconcile"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/nebla-cloud/nebla/internal/volumes"
	"github.com/nebla-cloud/nebla/internal/volumes/vref"
	"github.com/stretchr/testify/require"
)

// orphan cancels the action's River job without settling the action, as if it was lost.
func (e *env) orphan(t *testing.T, a actions.Action) {
	t.Helper()
	_, err := e.App.SQL.ExecContext(context.Background(),
		`update river_job set state = 'cancelled', finalized_at = now() where id = $1`, *a.RiverJobID)
	require.NoError(t, err)
}

func (e *env) action(t *testing.T, id int64) actions.Action {
	t.Helper()
	a, err := actions.Get(context.Background(), e.App.Ent, e.Project, id)
	require.NoError(t, err)
	return a
}

func TestRescueVolumeActions(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	s := e.RunningServer("web")
	free := e.put(t, e.Project, "free", "", "ceph", 10, nil, "")
	attach, err := e.svc.Attach(ctx, e.Project, free, "web")
	require.NoError(t, err)
	e.orphan(t, attach)

	creating, create, err := e.svc.Create(ctx, e.Project, volumes.CreateInput{Name: "new", SizeGB: 10, Location: "eu-1"})
	require.NoError(t, err)
	e.orphan(t, create)
	gone := e.put(t, e.Project, "gone", "", "ceph", 10, nil, "")
	del, err := e.svc.Delete(ctx, e.Project, gone)
	require.NoError(t, err)
	e.orphan(t, del)
	grow := e.put(t, e.Project, "grow", "", "ceph", 10, nil, "")
	resize, err := e.svc.Resize(ctx, e.Project, grow, 40)
	require.NoError(t, err)
	e.orphan(t, resize)
	// A live create is left alone.
	live, liveAct, err := e.svc.Create(ctx, e.Project, volumes.CreateInput{Name: "live", SizeGB: 10, Location: "eu-1"})
	require.NoError(t, err)

	// The server rescue leaves volume commands to the volumes rescue.
	rec := reconcile.New(reconcile.Options{SQL: e.App.SQL, Ent: e.App.Ent, Catalog: e.App.Catalog, Logger: quiet()})
	require.NoError(t, rec.RescueAll(ctx))
	require.Equal(t, actions.StatusRunning, e.action(t, attach.ID).Status)

	n, err := volumes.RescueOrphaned(ctx, e.App.Ent, e.App.SQL, nil)
	require.NoError(t, err)
	require.Equal(t, 4, n)
	for _, id := range []int64{attach.ID, create.ID, del.ID, resize.ID} {
		a := e.action(t, id)
		require.Equal(t, actions.StatusError, a.Status)
		require.Equal(t, actions.CodeOrphaned, a.Error.Code)
	}
	require.Equal(t, actions.StatusRunning, e.action(t, liveAct.ID).Status)

	row, err := e.App.Ent.Server.Get(ctx, s.ServerID)
	require.NoError(t, err)
	require.Equal(t, servers.StatusRunning, string(row.Status))
	require.Equal(t, volume.StatusAvailable, e.row(t, free).Status)
	require.Nil(t, e.row(t, free).ServerID)
	require.Equal(t, volume.StatusError, e.row(t, creating.ID).Status)
	require.Equal(t, volume.StatusAvailable, e.row(t, gone).Status)
	require.Equal(t, 10, e.row(t, grow).SizeGB, "a lost resize gives its quota back")
	require.Equal(t, volume.StatusCreating, e.row(t, live.ID).Status)

	// Nothing is left to rescue.
	n, err = volumes.RescueOrphaned(ctx, e.App.Ent, e.App.SQL, nil)
	require.NoError(t, err)
	require.Zero(t, n)
}

func TestRescuePublishesCreatedAfterAllocation(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	s := e.RunningServer("web")
	rec := &recorder{}

	// Orphaned after the disk was allocated: the rescue publishes volume.created.
	alloc, a1, err := e.svc.Create(ctx, e.Project, volumes.CreateInput{Name: "alloc", SizeGB: 10, Location: "eu-1"})
	require.NoError(t, err)
	require.NoError(t, e.App.Ent.Volume.UpdateOneID(alloc.ID).SetStatus(volume.StatusAvailable).SetAppliedGB(10).
		SetVolid(vref.Volid("ceph", alloc.ID, false)).Exec(ctx))
	e.orphan(t, a1)
	// Orphaned mid-attach: settles attached and publishes attached.
	plug, a2, err := e.svc.Create(ctx, e.Project, volumes.CreateInput{Name: "plug", SizeGB: 20, Server: "web"})
	require.NoError(t, err)
	require.NoError(t, e.App.Ent.Volume.UpdateOneID(plug.ID).SetStatus(volume.StatusAvailable).SetAppliedGB(20).
		SetVolid(vref.Volid("ceph", plug.ID, false)).SetServerID(s.ServerID).SetDevice("virtio1").Exec(ctx))
	e.orphan(t, a2)
	// Orphaned before allocation: no disk, no event.
	_, a3, err := e.svc.Create(ctx, e.Project, volumes.CreateInput{Name: "none", SizeGB: 10, Location: "eu-1"})
	require.NoError(t, err)
	e.orphan(t, a3)

	n, err := volumes.RescueOrphaned(ctx, e.App.Ent, e.App.SQL, rec)
	require.NoError(t, err)
	require.Equal(t, 3, n)
	require.Equal(t, volume.StatusAttached, e.row(t, plug.ID).Status)
	evs := rec.all()
	require.Len(t, evs, 2)
	got := map[int64]map[string]any{}
	for _, ev := range evs {
		require.Equal(t, ext.VolumeCreated, ev.Kind)
		got[ev.ResourceID] = ev.Data
	}
	require.Equal(t, map[string]any{"size_gb": 10, "location_id": e.Location, "attached": false}, got[alloc.ID])
	require.Equal(t, map[string]any{"size_gb": 20, "location_id": e.Location, "attached": true}, got[plug.ID])

	// A second pass publishes nothing more.
	n, err = volumes.RescueOrphaned(ctx, e.App.Ent, e.App.SQL, rec)
	require.NoError(t, err)
	require.Zero(t, n)
	require.Len(t, rec.all(), 2)
}

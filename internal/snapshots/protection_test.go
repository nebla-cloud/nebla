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
	"net/http"
	"testing"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/ent/snapshot"
	"github.com/nebla-cloud/nebla/internal/protection"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/nebla-cloud/nebla/internal/snapshots"
	"github.com/stretchr/testify/require"
)

func (e *env) putSnapshot(t *testing.T, name string, st snapshot.Status, protected bool) int64 {
	t.Helper()
	return e.App.Ent.Snapshot.Create().SetProjectID(e.Project).SetLocationID(e.Location).SetName(name).
		SetLabels(map[string]string{}).SetSizeGB(10).SetStatus(st).SetDeleteProtected(protected).SaveX(context.Background()).ID
}

func TestSnapshotDelete_Protected(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	id := e.putSnapshot(t, "snap", snapshot.StatusAvailable, true)
	before := e.App.Ent.Action.Query().CountX(ctx)
	_, err := e.svc.Delete(ctx, e.Project, id)
	require.ErrorIs(t, err, protection.ErrDelete)
	require.Equal(t, snapshot.StatusAvailable, e.App.Ent.Snapshot.GetX(ctx, id).Status)
	require.Equal(t, before, e.App.Ent.Action.Query().CountX(ctx))
}

func TestSnapshotDelete_ProtectedErrorState(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	id := e.putSnapshot(t, "snap", snapshot.StatusError, true)
	_, err := e.svc.Delete(ctx, e.Project, id)
	require.ErrorIs(t, err, protection.ErrDelete)
}

func TestSnapshotDelete_ProtectedBeatsBusy(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	s := e.RunningServer("web1")
	id := e.App.Ent.Snapshot.Create().SetProjectID(e.Project).SetLocationID(e.Location).SetName("snap").
		SetLabels(map[string]string{}).SetSizeGB(10).SetStatus(snapshot.StatusError).SetServerID(s.ServerID).
		SetWasRunning(true).SetDeleteProtected(true).SaveX(ctx).ID
	_, err := actions.Create(ctx, e.App.Ent, e.Project, "create_snapshot", []actions.Resource{{ID: id, Type: snapshots.ResourceType}})
	require.NoError(t, err)
	_, err = actions.Create(ctx, e.App.Ent, e.Project, "reboot_server", []actions.Resource{{ID: s.ServerID, Type: servers.ResourceType}})
	require.NoError(t, err)
	_, err = e.svc.Delete(ctx, e.Project, id)
	require.ErrorIs(t, err, protection.ErrDelete)
}

func TestSnapshotUpdate_ProtectionOnlyWhileBusy(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	id := e.putSnapshot(t, "snap", snapshot.StatusAvailable, false)
	_, err := actions.Create(ctx, e.App.Ent, e.Project, "create_snapshot", []actions.Resource{{ID: id, Type: snapshots.ResourceType}})
	require.NoError(t, err)
	got, err := e.svc.Update(ctx, e.Project, id, snapshots.UpdateInput{DeleteProtected: new(true)})
	require.NoError(t, err)
	require.True(t, got.DeleteProtected)
	require.True(t, e.App.Ent.Snapshot.GetX(ctx, id).DeleteProtected)
}

func TestSnapshotUpdate_MixedWhileBusyRefused(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	id := e.putSnapshot(t, "snap", snapshot.StatusAvailable, false)
	_, err := actions.Create(ctx, e.App.Ent, e.Project, "create_snapshot", []actions.Resource{{ID: id, Type: snapshots.ResourceType}})
	require.NoError(t, err)
	_, err = e.svc.Update(ctx, e.Project, id, snapshots.UpdateInput{Name: new("renamed"), DeleteProtected: new(true)})
	requireAPIErr(t, err, http.StatusConflict, "locked")
	r := e.App.Ent.Snapshot.GetX(ctx, id)
	require.Equal(t, "snap", r.Name)
	require.False(t, r.DeleteProtected)
}

func TestSnapshotCreate_WithProtection(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.RunningServer("web1")
	snap, _, err := e.svc.Create(ctx, e.Project, snapshots.CreateInput{Server: "web1", Name: "a", DeleteProtected: true})
	require.NoError(t, err)
	require.True(t, snap.DeleteProtected)
	require.True(t, e.App.Ent.Snapshot.GetX(ctx, snap.ID).DeleteProtected)
}

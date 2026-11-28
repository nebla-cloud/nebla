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
	"net/http"
	"testing"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/ent/volume"
	"github.com/nebla-cloud/nebla/internal/protection"
	"github.com/nebla-cloud/nebla/internal/volumes"
	"github.com/stretchr/testify/require"
)

func TestVolumeDelete_Protected(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	id := e.put(t, e.Project, "data", "", "ceph", 10, nil, "")
	e.App.Ent.Volume.UpdateOneID(id).SetDeleteProtected(true).ExecX(ctx)
	before := e.App.Ent.Action.Query().CountX(ctx)
	_, err := e.svc.Delete(ctx, e.Project, id)
	require.ErrorIs(t, err, protection.ErrDelete)
	require.Equal(t, volume.StatusAvailable, e.row(t, id).Status)
	require.Equal(t, before, e.App.Ent.Action.Query().CountX(ctx))
}

func TestVolumeDelete_ProtectedBeatsBusy(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	id := e.put(t, e.Project, "data", "", "ceph", 10, nil, "")
	e.App.Ent.Volume.UpdateOneID(id).SetDeleteProtected(true).ExecX(ctx)
	_, err := actions.Create(ctx, e.App.Ent, e.Project, "resize_volume", []actions.Resource{{ID: id, Type: volumes.ResourceType}})
	require.NoError(t, err)
	_, err = e.svc.Delete(ctx, e.Project, id)
	require.ErrorIs(t, err, protection.ErrDelete)
}

func TestVolumeUpdate_ProtectionOnlyWhileBusy(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	id := e.put(t, e.Project, "data", "", "ceph", 10, nil, "")
	_, err := actions.Create(ctx, e.App.Ent, e.Project, "resize_volume", []actions.Resource{{ID: id, Type: volumes.ResourceType}})
	require.NoError(t, err)
	v, err := e.svc.Update(ctx, e.Project, id, volumes.UpdateInput{DeleteProtected: new(true)})
	require.NoError(t, err)
	require.True(t, v.DeleteProtected)
	require.True(t, e.row(t, id).DeleteProtected)
}

func TestVolumeUpdate_MixedWhileBusyRefused(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	id := e.put(t, e.Project, "data", "", "ceph", 10, nil, "")
	_, err := actions.Create(ctx, e.App.Ent, e.Project, "resize_volume", []actions.Resource{{ID: id, Type: volumes.ResourceType}})
	require.NoError(t, err)
	_, err = e.svc.Update(ctx, e.Project, id, volumes.UpdateInput{Name: new("renamed"), DeleteProtected: new(true)})
	requireAPIErr(t, err, http.StatusConflict, "locked")
	r := e.row(t, id)
	require.Equal(t, "data", r.Name)
	require.False(t, r.DeleteProtected)
}

func TestVolumeCreate_WithProtection(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.localStorage(t)
	e.App.PVE.SetStorageAvail("pve1", "local-lvm", 20_000)
	v, _, err := e.svc.Create(ctx, e.Project, volumes.CreateInput{Name: "data", SizeGB: 10, Location: "eu-1", DeleteProtected: true})
	require.NoError(t, err)
	require.True(t, v.DeleteProtected)
	require.True(t, e.row(t, v.ID).DeleteProtected)
}

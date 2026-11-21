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

	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/volume"
	"github.com/nebla-cloud/nebla/internal/pve"
	"github.com/nebla-cloud/nebla/internal/volumes"
	"github.com/nebla-cloud/nebla/internal/volumes/vref"
	"github.com/stretchr/testify/require"
)

// fileStorage adds a file-type images storage on both nodes and points the location's volumes at it.
func (e *env) fileStorage(t *testing.T, id, typ string, shared bool) {
	t.Helper()
	for _, n := range []string{"pve1", "pve2"} {
		e.App.PVE.AddStorage(n, id, []string{"images", "iso"}, shared)
		e.App.PVE.SetStorageType(n, id, typ)
	}
	require.NoError(t, e.App.Ent.Location.UpdateOneID(e.Location).SetVolumeStorage(id).Exec(context.Background()))
}

// diskOn lists pve1's items on storage whose volid is volid.
func (e *env) diskOn(t *testing.T, storage, volid string) []pve.StorageItem {
	t.Helper()
	pc, err := e.App.Catalog.ProxmoxFor(context.Background(), e.Location)
	require.NoError(t, err)
	items, err := pc.StorageContent(context.Background(), "pve1", storage, vref.OwnerVMID)
	require.NoError(t, err)
	var out []pve.StorageItem
	for _, it := range items {
		if it.Volid == volid {
			out = append(out, it)
		}
	}
	return out
}

func TestFileStorageVolumeLifecycle(t *testing.T) {
	for _, tc := range []struct {
		name, storage, typ, node string
		shared                   bool
	}{
		{"dir", "dirvol", "dir", "pve1", false},
		{"nfs", "nfsvol", "nfs", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			e := newEnv(t)
			e.fileStorage(t, tc.storage, tc.typ, tc.shared)
			o := e.opts(nil)
			s := e.RunningServer("web")
			vmid := vmidOf(s)

			v, act, err := e.svc.Create(ctx, e.Project, volumes.CreateInput{Name: "data", SizeGB: 10, Server: "web"})
			e.ok(t, o, act, err)
			id := v.ID
			volid := tc.storage + ":999999999/vm-999999999-nb-vol-" + ref(id) + ".raw"
			r := e.row(t, id)
			require.Equal(t, tc.node, r.Node)
			require.Equal(t, volid, r.Volid)
			require.Equal(t, volume.StatusAttached, r.Status)
			want := volid + ",serial=nb-vol-" + ref(id) + ",backup=0"
			require.Equal(t, map[string]string{"virtio1": want}, e.keysFor(t, vmid, id))
			require.Len(t, e.diskOn(t, tc.storage, volid), 1)

			act, err = e.svc.Resize(ctx, e.Project, id, 20)
			e.ok(t, o, act, err)
			require.Equal(t, 20, e.row(t, id).AppliedGB)
			require.EqualValues(t, int64(20)<<30, e.diskOn(t, tc.storage, volid)[0].Size)

			act, err = e.svc.Detach(ctx, e.Project, id)
			e.ok(t, o, act, err)
			require.Empty(t, e.keysFor(t, vmid, id))

			act, err = e.svc.Attach(ctx, e.Project, id, "web")
			e.ok(t, o, act, err)
			require.Equal(t, map[string]string{"virtio1": want}, e.keysFor(t, vmid, id))
			act, err = e.svc.Detach(ctx, e.Project, id)
			e.ok(t, o, act, err)

			act, err = e.svc.Delete(ctx, e.Project, id)
			e.ok(t, o, act, err)
			_, err = e.App.Ent.Volume.Get(ctx, id)
			require.True(t, ent.IsNotFound(err))
			require.Empty(t, e.diskOn(t, tc.storage, volid))
		})
	}
}

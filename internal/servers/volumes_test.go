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
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/volume"
	"github.com/nebla-cloud/nebla/internal/pve"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/nebla-cloud/nebla/internal/volumes/vref"
	"github.com/stretchr/testify/require"
)

// attachVol allocates a ceph disk on the server's node, records the volume attached at device and plugs it into vmid.
func attachVol(t *testing.T, e *testutil.ServerEnv, serverID int64, vmid int, name, device string, sizeGB int) *ent.Volume {
	t.Helper()
	ctx := context.Background()
	srv, err := e.App.Ent.Server.Get(ctx, serverID)
	require.NoError(t, err)
	v, err := e.App.Ent.Volume.Create().SetProjectID(e.Project).SetLocationID(e.Location).SetName(name).
		SetLabels(map[string]string{}).SetSizeGB(sizeGB).SetAppliedGB(sizeGB).SetStorage("ceph").
		SetServerID(serverID).SetDevice(device).SetStatus(volume.StatusAttached).Save(ctx)
	require.NoError(t, err)
	pc, err := e.App.Catalog.ProxmoxFor(ctx, e.Location)
	require.NoError(t, err)
	volid, err := pc.AllocVolume(ctx, srv.Node, "ceph", vref.OwnerVMID, vref.Name(v.ID), sizeGB)
	require.NoError(t, err)
	v, err = v.Update().SetVolid(volid).Save(ctx)
	require.NoError(t, err)
	e.App.PVE.SetVMConfig(vmid, device, vref.ConfigValue(vref.Volid("ceph", v.ID, false), v.ID))
	return v
}

// disk returns the size of volume id's disk on node's ceph.
func disk(t *testing.T, e *testutil.ServerEnv, node string, id int64) (int64, bool) {
	t.Helper()
	pc, err := e.App.Catalog.ProxmoxFor(context.Background(), e.Location)
	require.NoError(t, err)
	items, err := pc.StorageContent(context.Background(), node, "ceph", vref.OwnerVMID)
	require.NoError(t, err)
	i := slices.IndexFunc(items, func(it pve.StorageItem) bool { return it.Volid == "ceph:"+vref.Name(id) })
	if i < 0 {
		return 0, false
	}
	return items[i].Size, true
}

func volRow(t *testing.T, e *testutil.ServerEnv, id int64) *ent.Volume {
	t.Helper()
	v, err := e.App.Ent.Volume.Get(context.Background(), id)
	require.NoError(t, err)
	return v
}

// callIndex is the position of the first call after from matching pred, or -1.
func callIndex(calls []string, from int, pred func(string) bool) int {
	for i := from; i < len(calls); i++ {
		if pred(calls[i]) {
			return i
		}
	}
	return -1
}

func TestDeleteServerKeepsVolumes(t *testing.T) {
	ctx := context.Background()
	e, cw := directEnv(t)
	s := builtServer(t, e, cw, "web")
	vmid := vmidOf(t, e, s.ServerID)
	srv, err := e.App.Ent.Server.Get(ctx, s.ServerID)
	require.NoError(t, err)
	a := attachVol(t, e, s.ServerID, vmid, "data", "virtio1", 10)
	b := attachVol(t, e, s.ServerID, vmid, "logs", "virtio2", 20)
	// A key that only looks like ours is left alone.
	e.App.PVE.SetVMConfig(vmid, "virtio3", "ceph:vm-999999999-nb-vol-x")

	del, _ := expect[actionBody](t, e.C.Send(http.MethodDelete, spath(s.ServerID), nil), http.StatusOK)
	from := len(e.App.PVE.Calls())
	w := servers.NewDeleteServerWorker(opts(e))
	require.NoError(t, w.Work(ctx, jobOf(queuedArgs[servers.DeleteServerArgs](t, e, del.Action.ID), 1)))

	calls := e.App.PVE.Calls()
	cfg := fmt.Sprintf("/nodes/%s/qemu/%d/config", srv.Node, vmid)
	unplug := callIndex(calls, from, func(c string) bool {
		return (strings.HasPrefix(c, "PUT ") || strings.HasPrefix(c, "POST ")) && strings.HasSuffix(c, cfg)
	})
	destroy := callIndex(calls, from, func(c string) bool { return c == fmt.Sprintf("DELETE /nodes/%s/qemu/%d", srv.Node, vmid) })
	require.GreaterOrEqual(t, unplug, 0, "volumes unplugged")
	require.Greater(t, destroy, unplug, "unplugged before the destroy")
	_, ok := e.App.PVE.VM(vmid)
	require.False(t, ok)
	for _, v := range []*ent.Volume{a, b} {
		got := volRow(t, e, v.ID)
		require.Equal(t, volume.StatusAvailable, got.Status)
		require.Nil(t, got.ServerID)
		require.Empty(t, got.Device)
		size, ok := disk(t, e, srv.Node, v.ID)
		require.True(t, ok, "disk kept")
		require.Equal(t, int64(v.SizeGB)<<30, size)
	}
	act, err := actions.Get(ctx, e.App.Ent, e.Project, del.Action.ID)
	require.NoError(t, err)
	require.Equal(t, actions.StatusSuccess, act.Status)
}

func TestDeleteServerUnplugsRebuildVM(t *testing.T) {
	ctx := context.Background()
	e, cw := directEnv(t)
	s := builtServer(t, e, cw, "web")
	vmid := vmidOf(t, e, s.ServerID)
	srv, err := e.App.Ent.Server.Get(ctx, s.ServerID)
	require.NoError(t, err)
	v := attachVol(t, e, s.ServerID, vmid, "data", "virtio1", 10)
	// A failed rebuild left a tagged new VM holding the volume too.
	e.App.PVE.AddVM(srv.Node, 777, "web")
	e.App.PVE.SetVMConfig(777, "tags", servers.TagNebla+";"+tag(s.ServerID))
	e.App.PVE.SetVMConfig(777, "virtio1", vref.ConfigValue(vref.Volid("ceph", v.ID, false), v.ID))
	require.NoError(t, e.App.Ent.Server.UpdateOneID(s.ServerID).SetRebuildVmid(777).Exec(ctx))

	del, _ := expect[actionBody](t, e.C.Send(http.MethodDelete, spath(s.ServerID), nil), http.StatusOK)
	from := len(e.App.PVE.Calls())
	w := servers.NewDeleteServerWorker(opts(e))
	require.NoError(t, w.Work(ctx, jobOf(queuedArgs[servers.DeleteServerArgs](t, e, del.Action.ID), 1)))
	calls := e.App.PVE.Calls()
	for _, id := range []int{vmid, 777} {
		cfg := fmt.Sprintf("/nodes/%s/qemu/%d/config", srv.Node, id)
		unplug := callIndex(calls, from, func(c string) bool {
			return (strings.HasPrefix(c, "PUT ") || strings.HasPrefix(c, "POST ")) && strings.HasSuffix(c, cfg)
		})
		destroy := callIndex(calls, from, func(c string) bool { return c == fmt.Sprintf("DELETE /nodes/%s/qemu/%d", srv.Node, id) })
		require.GreaterOrEqual(t, unplug, 0, "vm %d unplugged", id)
		require.Greater(t, destroy, unplug, "vm %d unplugged before the destroy", id)
	}
	got := volRow(t, e, v.ID)
	require.Equal(t, volume.StatusAvailable, got.Status)
	require.Nil(t, got.ServerID)
	_, ok := disk(t, e, srv.Node, v.ID)
	require.True(t, ok)
}

func TestDeleteServerVMGoneKeepsVolumes(t *testing.T) {
	ctx := context.Background()
	e, cw := directEnv(t)
	s := builtServer(t, e, cw, "web")
	vmid := vmidOf(t, e, s.ServerID)
	v := attachVol(t, e, s.ServerID, vmid, "data", "virtio1", 10)
	missingVol := attachVol(t, e, s.ServerID, vmid, "gone", "virtio2", 10)
	require.NoError(t, missingVol.Update().SetStatus(volume.StatusMissing).Exec(ctx))
	e.App.PVE.DeleteVM(vmid)

	del, _ := expect[actionBody](t, e.C.Send(http.MethodDelete, spath(s.ServerID), nil), http.StatusOK)
	w := servers.NewDeleteServerWorker(opts(e))
	require.NoError(t, w.Work(ctx, jobOf(queuedArgs[servers.DeleteServerArgs](t, e, del.Action.ID), 1)))
	got := volRow(t, e, v.ID)
	require.Equal(t, volume.StatusAvailable, got.Status)
	require.Nil(t, got.ServerID)
	got = volRow(t, e, missingVol.ID)
	require.Equal(t, volume.StatusMissing, got.Status, "a missing disk stays missing")
	require.Nil(t, got.ServerID)
	require.Empty(t, got.Device)
}

func TestRebuildCarriesVolumes(t *testing.T) {
	ctx := context.Background()
	e, s, w := rebuildEnv(t)
	old := vmidOf(t, e, s.ServerID)
	srv, err := e.App.Ent.Server.Get(ctx, s.ServerID)
	require.NoError(t, err)
	a := attachVol(t, e, s.ServerID, old, "data", "virtio1", 10)
	b := attachVol(t, e, s.ServerID, old, "logs", "virtio3", 20)
	oldVM, _ := e.App.PVE.VM(old)

	from := len(e.App.PVE.Calls())
	id := startAction(t, e, s.ServerID, "rebuild", map[string]any{"image": "debian"})
	require.NoError(t, w.Work(ctx, jobOf(queuedArgs[servers.RebuildServerArgs](t, e, id), 1)))
	nu := vmidOf(t, e, s.ServerID)
	require.NotEqual(t, old, nu)
	vm, ok := e.App.PVE.VM(nu)
	require.True(t, ok)
	for _, k := range []string{"virtio1", "virtio3"} {
		require.Equal(t, oldVM.Config[k], vm.Config[k], k)
	}
	_, ok = vm.Config["virtio2"]
	require.False(t, ok)

	calls := e.App.PVE.Calls()
	cfg := fmt.Sprintf("/nodes/%s/qemu/%d/config", srv.Node, old)
	unplug := callIndex(calls, from, func(c string) bool {
		return (strings.HasPrefix(c, "PUT ") || strings.HasPrefix(c, "POST ")) && strings.HasSuffix(c, cfg)
	})
	destroy := callIndex(calls, from, func(c string) bool { return c == fmt.Sprintf("DELETE /nodes/%s/qemu/%d", srv.Node, old) })
	require.GreaterOrEqual(t, unplug, 0, "old vm unplugged")
	require.Greater(t, destroy, unplug, "unplugged before the destroy")
	_, ok = e.App.PVE.VM(old)
	require.False(t, ok)

	for _, v := range []*ent.Volume{a, b} {
		got := volRow(t, e, v.ID)
		require.Equal(t, volume.StatusAttached, got.Status)
		require.Equal(t, s.ServerID, *got.ServerID)
		require.Equal(t, v.Device, got.Device)
		size, ok := disk(t, e, srv.Node, v.ID)
		require.True(t, ok)
		require.Equal(t, int64(v.SizeGB)<<30, size, "size unchanged")
	}
	require.Equal(t, servers.StatusRunning, status(t, e, s.ServerID))
}

func TestRebuildResumeAfterPlug(t *testing.T) {
	ctx := context.Background()
	e, s, w := rebuildEnv(t)
	old := vmidOf(t, e, s.ServerID)
	a := attachVol(t, e, s.ServerID, old, "data", "virtio1", 10)
	b := attachVol(t, e, s.ServerID, old, "logs", "virtio2", 10)
	id := startAction(t, e, s.ServerID, "rebuild", map[string]any{"image": "debian"})
	args := queuedArgs[servers.RebuildServerArgs](t, e, id)

	// Crash before the swap, with one volume already plugged into the new VM.
	crashCtx, crash := context.WithCancel(ctx)
	servers.SetRebuildAfterStep(w, func(n int) {
		if n == servers.StepSwap-1 {
			crash()
		}
	})
	require.Error(t, w.Work(crashCtx, jobOf(args, 1)))
	row, err := e.App.Ent.Server.Get(ctx, s.ServerID)
	require.NoError(t, err)
	require.NotNil(t, row.RebuildVmid)
	nu := *row.RebuildVmid
	e.App.PVE.SetVMConfig(nu, "virtio1", vref.ConfigValue(vref.Volid("ceph", a.ID, false), a.ID))

	// Crash again right after the swap.
	crashCtx, crash = context.WithCancel(ctx)
	servers.SetRebuildAfterStep(w, func(n int) {
		if n == servers.StepSwap {
			crash()
		}
	})
	require.Error(t, w.Work(crashCtx, jobOf(args, 2)))
	vm, _ := e.App.PVE.VM(nu)
	require.Equal(t, vref.ConfigValue(vref.Volid("ceph", a.ID, false), a.ID), vm.Config["virtio1"])
	require.Equal(t, vref.ConfigValue(vref.Volid("ceph", b.ID, false), b.ID), vm.Config["virtio2"])
	oldVM, ok := e.App.PVE.VM(old)
	require.True(t, ok)
	require.Equal(t, vref.ConfigValue(vref.Volid("ceph", a.ID, false), a.ID), oldVM.Config["virtio1"], "old VM keeps them until it retires")

	servers.SetRebuildAfterStep(w, nil)
	require.NoError(t, w.Work(ctx, jobOf(args, 3)))
	require.Equal(t, nu, vmidOf(t, e, s.ServerID))
	_, ok = e.App.PVE.VM(old)
	require.False(t, ok)
	vm, _ = e.App.PVE.VM(nu)
	require.Equal(t, vref.ConfigValue(vref.Volid("ceph", a.ID, false), a.ID), vm.Config["virtio1"])
	require.Equal(t, vref.ConfigValue(vref.Volid("ceph", b.ID, false), b.ID), vm.Config["virtio2"])
	for _, v := range []*ent.Volume{a, b} {
		got := volRow(t, e, v.ID)
		require.Equal(t, volume.StatusAttached, got.Status)
		require.Equal(t, v.Device, got.Device)
	}
}

func TestRebuildRefusesForeignKey(t *testing.T) {
	ctx := context.Background()
	e, s, w := rebuildEnv(t)
	old := vmidOf(t, e, s.ServerID)
	attachVol(t, e, s.ServerID, old, "data", "virtio1", 10)
	id := startAction(t, e, s.ServerID, "rebuild", map[string]any{"image": "debian"})
	args := queuedArgs[servers.RebuildServerArgs](t, e, id)
	crashCtx, crash := context.WithCancel(ctx)
	servers.SetRebuildAfterStep(w, func(n int) {
		if n == servers.StepSwap-1 {
			crash()
		}
	})
	require.Error(t, w.Work(crashCtx, jobOf(args, 1)))
	row, err := e.App.Ent.Server.Get(ctx, s.ServerID)
	require.NoError(t, err)
	e.App.PVE.SetVMConfig(*row.RebuildVmid, "virtio1", "ceph:vm-123-disk-1")

	servers.SetRebuildAfterStep(w, nil)
	require.Error(t, w.Work(ctx, jobOf(args, 2)))
	a, err := actions.Get(ctx, e.App.Ent, e.Project, id)
	require.NoError(t, err)
	require.Equal(t, actions.StatusError, a.Status)
	require.Contains(t, a.Error.Detail, "attach volumes to new vm")
	require.Equal(t, old, vmidOf(t, e, s.ServerID), "old VM is still the server's")
	vm, ok := e.App.PVE.VM(old)
	require.True(t, ok)
	require.Contains(t, vm.Config["virtio1"], "nb-vol-")
}

func TestDeleteServerUnplugFailureIsBestEffort(t *testing.T) {
	ctx := context.Background()
	e, cw := directEnv(t)
	s := builtServer(t, e, cw, "web")
	vmid := vmidOf(t, e, s.ServerID)
	srv, err := e.App.Ent.Server.Get(ctx, s.ServerID)
	require.NoError(t, err)
	v := attachVol(t, e, s.ServerID, vmid, "data", "virtio1", 10)
	put := fmt.Sprintf("PUT /nodes/%s/qemu/%d/config", srv.Node, vmid)
	w := servers.NewDeleteServerWorker(opts(e))
	del, _ := expect[actionBody](t, e.C.Send(http.MethodDelete, spath(s.ServerID), nil), http.StatusOK)
	args := queuedArgs[servers.DeleteServerArgs](t, e, del.Action.ID)

	before := countCalls(e, put)
	// A transient error retries the job.
	e.App.PVE.FailNext(put, http.StatusServiceUnavailable)
	require.Error(t, w.Work(ctx, jobOf(args, 1)))
	_, ok := e.App.PVE.VM(vmid)
	require.True(t, ok, "not destroyed on a transient unplug error")

	// The guest can't ack the hot-unplug: logged, and the destroy goes ahead.
	e.App.PVE.FailNext(put, http.StatusBadRequest)
	require.NoError(t, w.Work(ctx, jobOf(args, 2)))
	require.Equal(t, before+2, countCalls(e, put), "one failed unplug per attempt")
	act, err := actions.Get(ctx, e.App.Ent, e.Project, del.Action.ID)
	require.NoError(t, err)
	require.Equal(t, actions.StatusSuccess, act.Status)
	_, ok = e.App.PVE.VM(vmid)
	require.False(t, ok)
	got := volRow(t, e, v.ID)
	require.Equal(t, volume.StatusAvailable, got.Status)
	require.Nil(t, got.ServerID)
	_, ok = disk(t, e, srv.Node, v.ID)
	require.True(t, ok, "disk kept")
}

func TestRebuildRetireUnplugFailureIsBestEffort(t *testing.T) {
	ctx := context.Background()
	e, s, w := rebuildEnv(t)
	old := vmidOf(t, e, s.ServerID)
	srv, err := e.App.Ent.Server.Get(ctx, s.ServerID)
	require.NoError(t, err)
	v := attachVol(t, e, s.ServerID, old, "data", "virtio1", 10)
	put := fmt.Sprintf("PUT /nodes/%s/qemu/%d/config", srv.Node, old)
	id := startAction(t, e, s.ServerID, "rebuild", map[string]any{"image": "debian"})
	before := countCalls(e, put)
	e.App.PVE.FailNext(put, http.StatusBadRequest)
	require.NoError(t, w.Work(ctx, jobOf(queuedArgs[servers.RebuildServerArgs](t, e, id), 1)))
	require.Equal(t, before+1, countCalls(e, put), "the failed unplug was tried once")
	a, err := actions.Get(ctx, e.App.Ent, e.Project, id)
	require.NoError(t, err)
	require.Equal(t, actions.StatusSuccess, a.Status)
	_, ok := e.App.PVE.VM(old)
	require.False(t, ok)
	nu := vmidOf(t, e, s.ServerID)
	vm, _ := e.App.PVE.VM(nu)
	require.Equal(t, vref.ConfigValue(vref.Volid("ceph", v.ID, false), v.ID), vm.Config["virtio1"])
	got := volRow(t, e, v.ID)
	require.Equal(t, volume.StatusAttached, got.Status)
	require.Equal(t, s.ServerID, *got.ServerID)
	_, ok = disk(t, e, srv.Node, v.ID)
	require.True(t, ok)
}

func TestRebuildSkipsMissingVolumes(t *testing.T) {
	ctx := context.Background()
	e, s, w := rebuildEnv(t)
	old := vmidOf(t, e, s.ServerID)
	a := attachVol(t, e, s.ServerID, old, "data", "virtio1", 10)
	gone := attachVol(t, e, s.ServerID, old, "gone", "virtio2", 10)
	require.NoError(t, gone.Update().SetStatus(volume.StatusMissing).Exec(ctx))
	id := startAction(t, e, s.ServerID, "rebuild", map[string]any{"image": "debian"})
	require.NoError(t, w.Work(ctx, jobOf(queuedArgs[servers.RebuildServerArgs](t, e, id), 1)))
	vm, _ := e.App.PVE.VM(vmidOf(t, e, s.ServerID))
	require.Equal(t, vref.ConfigValue(vref.Volid("ceph", a.ID, false), a.ID), vm.Config["virtio1"])
	_, ok := vm.Config["virtio2"]
	require.False(t, ok, "a missing volume is not plugged")
	got := volRow(t, e, gone.ID)
	require.Equal(t, volume.StatusMissing, got.Status)
	require.Equal(t, s.ServerID, *got.ServerID)
	require.Equal(t, "virtio2", got.Device)
}

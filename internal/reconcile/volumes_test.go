package reconcile_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/nebla-cloud/nebla/internal/ent/action"
	"github.com/nebla-cloud/nebla/internal/ent/schema"
	"github.com/nebla-cloud/nebla/internal/ent/volume"
	"github.com/nebla-cloud/nebla/internal/reconcile"
	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/nebla-cloud/nebla/internal/volumes/vref"
	"github.com/stretchr/testify/require"
)

// diskVolume adds a volume row on pve1/local-lvm with its disk in Proxmox; the status is available.
func diskVolume(t *testing.T, e *testutil.ServerEnv, name string, gb int) (id int64, volid string) {
	t.Helper()
	ctx := context.Background()
	v, err := e.App.Ent.Volume.Create().SetProjectID(e.Project).SetLocationID(e.Location).SetName(name).
		SetLabels(map[string]string{}).SetSizeGB(gb).SetAppliedGB(gb).SetNode("pve1").SetStorage("local-lvm").
		SetStatus(volume.StatusAvailable).Save(ctx)
	require.NoError(t, err)
	pc, err := e.App.Catalog.ProxmoxFor(ctx, e.Location)
	require.NoError(t, err)
	volid, err = pc.AllocVolume(ctx, "pve1", "local-lvm", vref.OwnerVMID, vref.Name(v.ID), gb)
	require.NoError(t, err)
	require.NoError(t, v.Update().SetVolid(volid).Exec(ctx))
	return v.ID, volid
}

func volStatus(t *testing.T, e *testutil.ServerEnv, id int64) volume.Status {
	t.Helper()
	v, err := e.App.Ent.Volume.Get(context.Background(), id)
	require.NoError(t, err)
	return v.Status
}

func TestReconcileMarksVolumeMissing(t *testing.T) {
	ctx := context.Background()
	e := workerEnv(t)
	r := newReconciler(e, quiet())
	id, volid := diskVolume(t, e, "data", 5)

	require.NoError(t, r.Run(ctx, e.Location))
	require.Equal(t, volume.StatusAvailable, volStatus(t, e, id))

	pc, _ := e.App.Catalog.ProxmoxFor(ctx, e.Location)
	require.NoError(t, pc.DeleteVolume(ctx, "pve1", "local-lvm", volid))
	require.NoError(t, r.Run(ctx, e.Location))
	require.Equal(t, volume.StatusMissing, volStatus(t, e, id))
}

func TestReconcileRestoresMissingVolume(t *testing.T) {
	ctx := context.Background()
	e := workerEnv(t)
	r := newReconciler(e, quiet())
	id, _ := diskVolume(t, e, "data", 5)
	other, _ := diskVolume(t, e, "attached", 5)
	srv, _ := running(t, e, "web")
	require.NoError(t, e.App.Ent.Volume.UpdateOneID(id).SetStatus(volume.StatusMissing).Exec(ctx))
	require.NoError(t, e.App.Ent.Volume.UpdateOneID(other).SetStatus(volume.StatusMissing).SetServerID(srv.ID).SetDevice("virtio1").Exec(ctx))

	require.NoError(t, r.Run(ctx, e.Location))
	require.Equal(t, volume.StatusAvailable, volStatus(t, e, id))
	require.Equal(t, volume.StatusAttached, volStatus(t, e, other))
}

func TestReconcileIgnoresForeignDisks(t *testing.T) {
	ctx := context.Background()
	e := workerEnv(t)
	logs := &lockedBuffer{}
	r := newReconciler(e, newLogger(logs))
	id, _ := diskVolume(t, e, "data", 5)
	pc, _ := e.App.Catalog.ProxmoxFor(ctx, e.Location)
	stray, err := pc.AllocVolume(ctx, "pve1", "local-lvm", vref.OwnerVMID, "vm-999999999-disk-0", 1)
	require.NoError(t, err)
	foreign, err := pc.AllocVolume(ctx, "pve1", "local-lvm", 100, "vm-100-disk-1", 1)
	require.NoError(t, err)

	require.NoError(t, r.Run(ctx, e.Location))
	require.Equal(t, volume.StatusAvailable, volStatus(t, e, id))
	items, err := pc.StorageContent(ctx, "pve1", "local-lvm", 0)
	require.NoError(t, err)
	var volids []string
	for _, it := range items {
		volids = append(volids, it.Volid)
	}
	require.Contains(t, volids, stray)
	require.Contains(t, volids, foreign)
	require.Contains(t, logs.String(), stray)
	require.NotContains(t, logs.String(), foreign)
}

func TestReconcileSkipsBusyVolume(t *testing.T) {
	ctx := context.Background()
	e := workerEnv(t)
	r := newReconciler(e, quiet())
	id, volid := diskVolume(t, e, "data", 5)
	_, err := e.App.Ent.Action.Create().SetProjectID(e.Project).SetCommand("resize_volume").SetStatus(action.StatusRunning).
		SetResources([]schema.ActionResource{{ID: id, Type: "volume"}}).Save(ctx)
	require.NoError(t, err)
	pc, _ := e.App.Catalog.ProxmoxFor(ctx, e.Location)
	require.NoError(t, pc.DeleteVolume(ctx, "pve1", "local-lvm", volid))

	require.NoError(t, r.Run(ctx, e.Location))
	require.Equal(t, volume.StatusAvailable, volStatus(t, e, id))
}

func TestReconcileUpdatesAppliedSize(t *testing.T) {
	ctx := context.Background()
	e := workerEnv(t)
	r := newReconciler(e, quiet())
	id, _ := diskVolume(t, e, "data", 8)
	require.NoError(t, e.App.Ent.Volume.UpdateOneID(id).SetAppliedGB(5).Exec(ctx))

	require.NoError(t, r.Run(ctx, e.Location))
	v, err := e.App.Ent.Volume.Get(ctx, id)
	require.NoError(t, err)
	require.Equal(t, 8, v.AppliedGB)
	require.Equal(t, 8, v.SizeGB)

	// A disk larger than size_gb is only logged.
	require.NoError(t, e.App.Ent.Volume.UpdateOneID(id).SetSizeGB(6).SetAppliedGB(6).Exec(ctx))
	require.NoError(t, r.Run(ctx, e.Location))
	v, _ = e.App.Ent.Volume.Get(ctx, id)
	require.Equal(t, 6, v.SizeGB)
	require.Equal(t, 6, v.AppliedGB)
}

func TestReconcileRunsVolumeRescue(t *testing.T) {
	ctx := context.Background()
	e := workerEnv(t)
	calls := 0
	r := reconcile.New(reconcile.Options{SQL: e.App.SQL, Ent: e.App.Ent, Catalog: e.App.Catalog, Logger: quiet(),
		VolumeRescue: func(context.Context) error { calls++; return errors.New("boom") }})
	require.NoError(t, r.Run(ctx, e.Location))
	require.Equal(t, 1, calls)
}

func newLogger(w *lockedBuffer) *slog.Logger { return slog.New(slog.NewTextHandler(w, nil)) }

func TestReconcileSharedVolume(t *testing.T) {
	ctx := context.Background()
	e := workerEnv(t)
	r := newReconciler(e, quiet())
	v, err := e.App.Ent.Volume.Create().SetProjectID(e.Project).SetLocationID(e.Location).SetName("shared").
		SetLabels(map[string]string{}).SetSizeGB(2).SetAppliedGB(2).SetStorage("ceph").SetStatus(volume.StatusAvailable).Save(ctx)
	require.NoError(t, err)
	require.NoError(t, v.Update().SetVolid(vref.Volid("ceph", v.ID, false)).Exec(ctx))
	require.NoError(t, r.Run(ctx, e.Location))
	require.Equal(t, volume.StatusMissing, volStatus(t, e, v.ID))

	pc, _ := e.App.Catalog.ProxmoxFor(ctx, e.Location)
	_, err = pc.AllocVolume(ctx, "pve1", "ceph", vref.OwnerVMID, vref.Name(v.ID), 2)
	require.NoError(t, err)
	require.NoError(t, r.Run(ctx, e.Location))
	require.Equal(t, volume.StatusAvailable, volStatus(t, e, v.ID))
}

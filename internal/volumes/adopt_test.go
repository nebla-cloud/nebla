package volumes_test

import (
	"context"
	"testing"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/ent/volume"
	"github.com/nebla-cloud/nebla/internal/volumes"
	"github.com/nebla-cloud/nebla/internal/volumes/vref"
	"github.com/stretchr/testify/require"
)

// preallocate puts a disk with volume id's exact name on pve1 local-lvm, as if something else made it.
func (e *env) preallocate(t *testing.T, id int64) string {
	t.Helper()
	pc, err := e.App.Catalog.ProxmoxFor(context.Background(), e.Location)
	require.NoError(t, err)
	volid, err := pc.AllocVolume(context.Background(), "pve1", "local-lvm", vref.OwnerVMID, vref.Name(id), 10)
	require.NoError(t, err)
	return volid
}

func TestCreateRefusesExistingDiskOnFirstAttempt(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.pinPVE1(t)
	o := e.opts(nil)
	v, act, err := e.svc.Create(ctx, e.Project, volumes.CreateInput{Name: "data", SizeGB: 10, Location: "eu-1"})
	require.NoError(t, err)
	volid := e.preallocate(t, v.ID)

	got := e.finish(t, o, act)
	require.Equal(t, actions.StatusError, got.Status)
	ae, err := actions.GetAny(ctx, e.App.Ent, act.ID)
	require.NoError(t, err)
	require.NotNil(t, ae.Error)
	require.Equal(t, actions.CodeFailed, ae.Error.Code)
	require.Contains(t, ae.Error.Detail, "a disk named "+volid+" already exists on local-lvm; it was not created by this volume")
	r := e.row(t, v.ID)
	require.Equal(t, volume.StatusError, r.Status)
	require.Equal(t, "", r.Volid)

	// Deleting the failed volume leaves the disk it never owned.
	act, err = e.svc.Delete(ctx, e.Project, v.ID)
	e.ok(t, o, act, err)
	require.Len(t, e.disks(t, v.ID), 1)
}

func TestCreateAdoptsOwnDiskOnRetry(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.pinPVE1(t)
	o := e.opts(nil)
	v, act, err := e.svc.Create(ctx, e.Project, volumes.CreateInput{Name: "data", SizeGB: 10, Location: "eu-1"})
	require.NoError(t, err)
	// An earlier attempt sent the allocation and lost the reply.
	require.NoError(t, actions.Progress(ctx, e.App.Ent, act.ID, 10))
	volid := e.preallocate(t, v.ID)

	require.NoError(t, e.work(ctx, t, o, act.ID, 2))
	got, err := actions.Get(ctx, e.App.Ent, e.Project, act.ID)
	require.NoError(t, err)
	require.Equal(t, actions.StatusSuccess, got.Status)
	r := e.row(t, v.ID)
	require.Equal(t, volume.StatusAvailable, r.Status)
	require.Equal(t, volid, r.Volid)
}

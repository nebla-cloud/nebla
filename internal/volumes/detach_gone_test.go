package volumes_test

import (
	"context"
	"testing"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/ent/volume"
	"github.com/nebla-cloud/nebla/internal/volumes"
	"github.com/stretchr/testify/require"
)

func TestDetachWhenServerVMGone(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.pinPVE1(t)
	o := e.opts(nil)
	s1 := e.RunningServer("web")
	s2 := e.RunningServer("db")

	v, act, err := e.svc.Create(ctx, e.Project, volumes.CreateInput{Name: "data", SizeGB: 10, Server: "web"})
	e.ok(t, o, act, err)
	w, act, err := e.svc.Create(ctx, e.Project, volumes.CreateInput{Name: "logs", SizeGB: 10, Server: "db"})
	e.ok(t, o, act, err)

	// Absent from the listing: nothing to unplug, the row is freed.
	e.App.PVE.DeleteVM(vmidOf(s1))
	act, err = e.svc.Detach(ctx, e.Project, v.ID)
	e.ok(t, o, act, err)
	r := e.row(t, v.ID)
	require.Equal(t, volume.StatusAvailable, r.Status)
	require.Nil(t, r.ServerID)
	require.Equal(t, "", r.Device)

	// Present but no longer the server's: refused.
	e.App.PVE.SetVMConfig(vmidOf(s2), "tags", "")
	act, err = e.svc.Detach(ctx, e.Project, w.ID)
	require.NoError(t, err)
	got := e.finish(t, o, act)
	require.Equal(t, actions.StatusError, got.Status)
	require.Equal(t, actions.CodeServerMissing, got.Error.Code)
	require.Equal(t, volume.StatusAttached, e.row(t, w.ID).Status)

	// QEMU's listing omits containers, which also occupy the ID.
	e.App.PVE.DeleteVM(vmidOf(s2))
	e.App.PVE.AddContainer("pve1", vmidOf(s2))
	act, err = e.svc.Detach(ctx, e.Project, w.ID)
	require.NoError(t, err)
	got = e.finish(t, o, act)
	require.Equal(t, actions.StatusError, got.Status)
	require.Equal(t, actions.CodeServerMissing, got.Error.Code)
	require.Equal(t, volume.StatusAttached, e.row(t, w.ID).Status)
}

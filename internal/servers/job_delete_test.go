package servers_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/Hexeption/nebla/internal/reconcile"
	"github.com/Hexeption/nebla/internal/servers"
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

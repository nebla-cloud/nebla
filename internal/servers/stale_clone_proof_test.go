package servers_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/riverqueue/river"
	"github.com/stretchr/testify/require"
)

// A stale clone_upid must not get an unrelated VM destroyed.
func TestDeleteSparesUnrelatedVMAfterLostDestroyPoll(t *testing.T) {
	ctx := context.Background()
	e, w := directEnv(t)
	s := e.CreateServer("web", nil)

	servers.SetAfterClonePost(w, func() {
		e.App.PVE.FailNext("/status", http.StatusInternalServerError)
		e.App.PVE.FailNext("/status", http.StatusInternalServerError)
		for range 4 {
			e.App.PVE.FailNext("/status", http.StatusServiceUnavailable)
		}
	})
	err := w.Work(ctx, job(s, 1))
	var cancel *river.JobCancelError
	require.True(t, errors.As(err, &cancel), "%v", err)

	row, err := e.App.Ent.Server.Get(ctx, s.ServerID)
	require.NoError(t, err)
	require.NotNil(t, row.Vmid)
	require.NotNil(t, row.CloneUpid)
	vmid := *row.Vmid

	require.Eventually(t, func() bool { _, ok := e.App.PVE.VM(vmid); return !ok }, 30*time.Second, 200*time.Millisecond)
	e.App.PVE.AddVM("pve1", vmid, "operator-dummy")

	del, _ := expect[actionBody](t, e.C.Send(http.MethodDelete, spath(s.ServerID), nil), http.StatusOK)
	dw := servers.NewDeleteServerWorker(opts(e))
	require.NoError(t, dw.Work(ctx, jobOf(queuedArgs[servers.DeleteServerArgs](t, e, del.Action.ID), 1)))

	a, err := actions.Get(ctx, e.App.Ent, e.Project, del.Action.ID)
	require.NoError(t, err)
	require.Equal(t, actions.StatusSuccess, a.Status, "%+v", a.Error)
	vm, ok := e.App.PVE.VM(vmid)
	require.True(t, ok, "unrelated vm must survive")
	require.Equal(t, "operator-dummy", vm.Name)
	require.Empty(t, vm.Tags, "unrelated vm must stay untagged")
	n, err := e.App.Ent.IPAllocation.Query().Count(ctx)
	require.NoError(t, err)
	require.Zero(t, n, "ips released")
}

// Cleanup still destroys its own untagged clone via the nonce.
func TestCleanupStillDestroysOwnUntaggedClone(t *testing.T) {
	ctx := context.Background()
	e, w := directEnv(t)
	s := e.CreateServer("web", nil)

	servers.SetAfterClonePost(w, func() {
		e.App.PVE.FailNext("/status", http.StatusInternalServerError)
	})
	err := w.Work(ctx, job(s, 1))
	var cancel *river.JobCancelError
	require.True(t, errors.As(err, &cancel), "%v", err)

	require.Zero(t, vmCount(t, e), "own clone destroyed")
	row, err := e.App.Ent.Server.Get(ctx, s.ServerID)
	require.NoError(t, err)
	require.Nil(t, row.Vmid)
	require.Nil(t, row.CloneUpid)
}

// A succeeded saved UPID with no nonce spares the VM and forgets the vmid.
func TestDeleteSparesVMWhenUPIDSucceededWithoutNonce(t *testing.T) {
	ctx := context.Background()
	e, w := directEnv(t)
	s := e.CreateServer("web", nil)

	servers.SetAfterClonePost(w, func() {
		e.App.PVE.FailNext("/status", http.StatusInternalServerError)
		e.App.PVE.FailNext("/status", http.StatusInternalServerError)
		for range 4 {
			e.App.PVE.FailNext("/status", http.StatusServiceUnavailable)
		}
	})
	err := w.Work(ctx, job(s, 1))
	var cancel *river.JobCancelError
	require.True(t, errors.As(err, &cancel), "%v", err)

	row, err := e.App.Ent.Server.Get(ctx, s.ServerID)
	require.NoError(t, err)
	require.NotNil(t, row.Vmid)
	require.NotNil(t, row.CloneUpid)
	vmid := *row.Vmid
	require.NoError(t, e.App.Ent.Server.UpdateOneID(s.ServerID).ClearCloneNonce().Exec(ctx))

	require.Eventually(t, func() bool { _, ok := e.App.PVE.VM(vmid); return !ok }, 30*time.Second, 200*time.Millisecond)
	e.App.PVE.AddVM("pve1", vmid, "operator-dummy")

	del, _ := expect[actionBody](t, e.C.Send(http.MethodDelete, spath(s.ServerID), nil), http.StatusOK)
	dw := servers.NewDeleteServerWorker(opts(e))
	require.NoError(t, dw.Work(ctx, jobOf(queuedArgs[servers.DeleteServerArgs](t, e, del.Action.ID), 1)))

	a, err := actions.Get(ctx, e.App.Ent, e.Project, del.Action.ID)
	require.NoError(t, err)
	require.Equal(t, actions.StatusSuccess, a.Status, "%+v", a.Error)
	vm, ok := e.App.PVE.VM(vmid)
	require.True(t, ok)
	require.Empty(t, vm.Tags)
}

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

package reconcile_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/nebla-cloud/nebla/internal/ent/action"
	"github.com/nebla-cloud/nebla/internal/ent/schema"
	"github.com/nebla-cloud/nebla/internal/ent/snapshot"
	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/stretchr/testify/require"
)

// snapRow adds a snapshot row at vmid; the template VM is added only when withVM is set.
func snapRow(t *testing.T, e *testutil.ServerEnv, name string, vmid int, st snapshot.Status, withVM bool) int64 {
	t.Helper()
	ctx := context.Background()
	row, err := e.App.Ent.Snapshot.Create().SetProjectID(e.Project).SetLocationID(e.Location).SetName(name).
		SetLabels(map[string]string{}).SetSizeGB(10).SetVmid(vmid).SetNode("pve1").SetNonce("n0nce").SetStatus(st).Save(ctx)
	require.NoError(t, err)
	if withVM {
		e.App.PVE.AddTemplate("pve1", vmid, "local-lvm")
		e.App.PVE.SetVMConfig(vmid, "tags", fmt.Sprintf("nebla-snapshot;nebla-snapshot-%d", row.ID))
		e.App.PVE.SetVMConfig(vmid, "description", fmt.Sprintf("nebla snapshot %d n0nce", row.ID))
	}
	return row.ID
}

func snapStatus(t *testing.T, e *testutil.ServerEnv, id int64) snapshot.Status {
	t.Helper()
	row, err := e.App.Ent.Snapshot.Get(context.Background(), id)
	require.NoError(t, err)
	return row.Status
}

func TestReconcileSnapshotMissing(t *testing.T) {
	ctx := context.Background()
	e := workerEnv(t)
	r := newReconciler(e, quiet())
	_, _ = running(t, e, "web")
	id := snapRow(t, e, "gold", 9100, snapshot.StatusAvailable, true)

	require.NoError(t, r.Run(ctx, e.Location))
	require.Equal(t, snapshot.StatusAvailable, snapStatus(t, e, id))

	e.App.PVE.DeleteVM(9100)
	require.NoError(t, r.Run(ctx, e.Location))
	require.Equal(t, snapshot.StatusMissing, snapStatus(t, e, id))
}

func TestReconcileSnapshotLeavesJobOwnedRows(t *testing.T) {
	ctx := context.Background()
	e := workerEnv(t)
	r := newReconciler(e, quiet())
	_, _ = running(t, e, "web")
	creating := snapRow(t, e, "a", 9101, snapshot.StatusCreating, false)
	deleting := snapRow(t, e, "b", 9102, snapshot.StatusDeleting, false)
	busy := snapRow(t, e, "c", 9103, snapshot.StatusAvailable, false)
	_, err := e.App.Ent.Action.Create().SetProjectID(e.Project).SetCommand("delete_snapshot").SetStatus(action.StatusRunning).
		SetResources([]schema.ActionResource{{ID: busy, Type: "snapshot"}}).Save(ctx)
	require.NoError(t, err)
	stuck := snapRow(t, e, "d", 9104, snapshot.StatusError, false)
	_, err = e.App.Ent.Action.Create().SetProjectID(e.Project).SetCommand("delete_snapshot").SetStatus(action.StatusRunning).
		SetResources([]schema.ActionResource{{ID: stuck, Type: "snapshot"}}).Save(ctx)
	require.NoError(t, err)

	require.NoError(t, r.Run(ctx, e.Location))
	require.Equal(t, snapshot.StatusCreating, snapStatus(t, e, creating))
	require.Equal(t, snapshot.StatusDeleting, snapStatus(t, e, deleting))
	require.Equal(t, snapshot.StatusAvailable, snapStatus(t, e, busy))
	require.Equal(t, snapshot.StatusError, snapStatus(t, e, stuck))
}

func TestReconcileUntrackedSnapshotTemplateLogged(t *testing.T) {
	ctx := context.Background()
	e := workerEnv(t)
	logs := &lockedBuffer{}
	r := newReconciler(e, newLogger(logs))
	_, _ = running(t, e, "web")
	e.App.PVE.AddTemplate("pve1", 9200, "local-lvm")
	e.App.PVE.SetVMConfig(9200, "tags", "nebla-snapshot;nebla-snapshot-777")

	require.NoError(t, r.Run(ctx, e.Location))
	require.Contains(t, logs.String(), "snapshot template without a row")
	require.Contains(t, logs.String(), "vmid=9200")
	_, found := e.App.PVE.VM(9200)
	require.True(t, found, "never deleted")
}

func TestReconcileRetriesErrorCleanup(t *testing.T) {
	ctx := context.Background()
	e := workerEnv(t)
	r := newReconciler(e, quiet())
	_, _ = running(t, e, "web")
	gone := snapRow(t, e, "gone", 9300, snapshot.StatusError, false)
	ours := snapRow(t, e, "ours", 9301, snapshot.StatusError, true)
	foreign := snapRow(t, e, "foreign", 9302, snapshot.StatusError, true)
	e.App.PVE.SetVMConfig(9302, "description", "someone else")

	require.NoError(t, r.Run(ctx, e.Location))
	for _, id := range []int64{gone, ours, foreign} {
		_, err := e.App.Ent.Snapshot.Get(ctx, id)
		require.Error(t, err, "row %d released", id)
	}
	_, found := e.App.PVE.VM(9301)
	require.False(t, found, "our vm destroyed")
	_, found = e.App.PVE.VM(9302)
	require.True(t, found, "foreign vm left alone")
}

func TestReconcileRetryRestartsSourceServer(t *testing.T) {
	ctx := context.Background()
	e := workerEnv(t)
	r := newReconciler(e, quiet())
	srv, vmid := running(t, e, "web")
	id := snapRow(t, e, "gone", 9400, snapshot.StatusError, false)
	require.NoError(t, e.App.Ent.Snapshot.UpdateOneID(id).SetServerID(srv.ID).SetWasRunning(true).Exec(ctx))
	e.App.PVE.SetVMStatus(vmid, "stopped")

	require.NoError(t, r.Run(ctx, e.Location))
	v, _ := e.App.PVE.VM(vmid)
	require.Equal(t, "running", v.Status)
	_, err := e.App.Ent.Snapshot.Get(ctx, id)
	require.Error(t, err)
}

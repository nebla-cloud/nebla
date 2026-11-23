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
	"errors"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/nebla-cloud/nebla/internal/apierr"
	"github.com/nebla-cloud/nebla/internal/ent/server"
	"github.com/nebla-cloud/nebla/internal/ent/snapshot"
	"github.com/nebla-cloud/nebla/internal/projects"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/stretchr/testify/require"
)

func (e *env) servers() *servers.Service {
	a := e.App
	return servers.New(servers.Options{SQL: a.SQL, Ent: a.Ent, Catalog: a.Catalog, Projects: projects.New(a.Ent, projects.Options{}), Jobs: a.Jobs})
}

// addAvailable inserts an available snapshot with a template VM on pve1.
func (e *env) addAvailable(t *testing.T, name string, vmid int) int64 {
	t.Helper()
	e.App.PVE.AddTemplate("pve1", vmid, "ceph")
	return e.App.Ent.Snapshot.Create().SetProjectID(e.Project).SetLocationID(e.Location).SetName(name).SetLabels(map[string]string{}).
		SetSizeGB(10).SetVmid(vmid).SetNode("pve1").SetStatus(snapshot.StatusAvailable).SaveX(context.Background()).ID
}

func fromSnapshot(name, snap string) servers.CreateInput {
	return servers.CreateInput{Name: name, ServerType: "small", Snapshot: snap}
}

func TestDeleteSnapshotInUseByCreate(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	id := e.addAvailable(t, "gold", 9100)
	srv, _, err := e.servers().Create(ctx, e.Project, fromSnapshot("web", "gold"))
	require.NoError(t, err)

	_, err = e.svc.Delete(ctx, e.Project, id)
	ae := requireAPIErr(t, err, http.StatusConflict, "snapshot_in_use")
	require.Equal(t, "This snapshot is being used to build a server. Try again when it's done.", ae.Msg)
	require.Equal(t, snapshot.StatusAvailable, e.App.Ent.Snapshot.GetX(ctx, id).Status)

	// Once the server is built the clone is done, and the delete goes through.
	e.settle(t)
	e.App.Ent.Server.UpdateOneID(srv.ID).SetStatus(server.StatusRunning).ExecX(ctx)
	_, err = e.svc.Delete(ctx, e.Project, id)
	require.NoError(t, err)
}

func TestDeleteSnapshotInUseByRebuild(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	id := e.addAvailable(t, "gold", 9100)
	s := e.RunningServer("web")
	_, err := e.servers().Rebuild(ctx, e.Project, s.ServerID, servers.RebuildSource{Snapshot: "gold"})
	require.NoError(t, err)

	_, err = e.svc.Delete(ctx, e.Project, id)
	requireAPIErr(t, err, http.StatusConflict, "snapshot_in_use")

	// The marker clears when the rebuild ends.
	e.settle(t)
	e.App.Ent.Server.UpdateOneID(s.ServerID).ClearRebuildSnapshotID().SetStatus(server.StatusRunning).ExecX(ctx)
	_, err = e.svc.Delete(ctx, e.Project, id)
	require.NoError(t, err)
}

func TestDeleteSnapshotAfterCloneDone(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	id := e.addAvailable(t, "gold", 9100)
	srv, _, err := e.servers().Create(ctx, e.Project, fromSnapshot("web", "gold"))
	require.NoError(t, err)
	e.settle(t)
	e.App.Ent.Server.UpdateOneID(srv.ID).SetStatus(server.StatusOff).ExecX(ctx)
	_, err = e.svc.Delete(ctx, e.Project, id)
	require.NoError(t, err)
	require.Nil(t, e.App.Ent.Server.GetX(ctx, srv.ID).RebuildSnapshotID)
}

// A create and a delete of the same snapshot never both win: a server never starts building from a snapshot that is being deleted.
func TestSnapshotDeleteVsCreateRace(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	require.NoError(t, e.App.Ent.Project.UpdateOneID(e.Project).SetMaxServers(1000).SetMaxVcpu(1<<20).SetMaxMemoryMB(1<<24).SetMaxDiskGB(1<<20).Exec(ctx))
	srvs := e.servers()

	const iterations = 50
	created, deleted := 0, 0
	for i := range iterations {
		name := fmt.Sprintf("gold-%d", i)
		id := e.addAvailable(t, name, 9100+i)
		start := make(chan struct{})
		var wg sync.WaitGroup
		var createErr, deleteErr error
		wg.Go(func() {
			<-start
			_, _, createErr = srvs.Create(ctx, e.Project, fromSnapshot(fmt.Sprintf("web-%d", i), name))
		})
		wg.Go(func() {
			<-start
			time.Sleep(time.Duration(i) * 400 * time.Microsecond)
			_, deleteErr = e.svc.Delete(ctx, e.Project, id)
		})
		close(start)
		wg.Wait()

		built, err := e.App.Ent.Server.Query().Where(server.SnapshotID(id), server.StatusEQ(servers.StatusInitializing)).Exist(ctx)
		require.NoError(t, err)
		snap := e.App.Ent.Snapshot.GetX(ctx, id)
		if built {
			created++
			require.NoError(t, createErr, "iteration %d", i)
			var ae apierr.E
			require.True(t, errors.As(deleteErr, &ae), "iteration %d: delete must lose: %v", i, deleteErr)
			require.Equal(t, http.StatusConflict, ae.Status, "iteration %d", i)
			require.Equal(t, "snapshot_in_use", ae.Code, "iteration %d", i)
			require.Equal(t, snapshot.StatusAvailable, snap.Status, "iteration %d", i)
		} else {
			deleted++
			require.NoError(t, deleteErr, "iteration %d", i)
			require.Equal(t, snapshot.StatusDeleting, snap.Status, "iteration %d", i)
			var ae apierr.E
			require.True(t, errors.As(createErr, &ae), "iteration %d: create must lose: %v", i, createErr)
			require.Contains(t, []int{http.StatusNotFound, http.StatusUnprocessableEntity, http.StatusConflict}, ae.Status, "iteration %d", i)
		}
		// Reset for the next round.
		e.App.Ent.Server.Delete().ExecX(ctx)
		e.settle(t)
	}
	t.Logf("create won %d, delete won %d of %d", created, deleted, iterations)
}

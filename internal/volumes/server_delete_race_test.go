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
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/volume"
	"github.com/nebla-cloud/nebla/internal/projects"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/nebla-cloud/nebla/internal/volumes"
	"github.com/nebla-cloud/nebla/internal/volumes/vref"
	"github.com/stretchr/testify/require"
)

func TestAttachVsServerDeleteRace(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.pinPVE1(t)
	a := e.App
	proj := projects.New(a.Ent, projects.Options{Hooks: projects.Hooks{Usage: volumes.UsageWith(servers.Usage)}})
	srvs := servers.New(servers.Options{SQL: a.SQL, Ent: a.Ent, Catalog: a.Catalog, Projects: proj, Jobs: a.Jobs})
	require.NoError(t, a.Ent.Project.UpdateOneID(e.Project).SetMaxVcpu(1000).SetMaxMemoryMB(1<<20).SetMaxDiskGB(1<<20).
		SetMaxVolumeGB(1<<20).Exec(ctx))
	pc, err := a.Catalog.ProxmoxFor(ctx, e.Location)
	require.NoError(t, err)
	o := e.opts(nil)
	so := servers.WorkerOptions{SQL: a.SQL, Ent: a.Ent, Catalog: a.Catalog, Logger: quiet(), PollInterval: 10 * time.Millisecond}
	vid := e.put(t, e.Project, "data", "pve1", "local-lvm", 10, nil, "")
	_, err = pc.AllocVolume(ctx, "pve1", "local-lvm", vref.OwnerVMID, vref.Name(vid), 10)
	require.NoError(t, err)

	deleteServer := func(act actions.Action) {
		t.Helper()
		args := queuedArgs[servers.DeleteServerArgs](t, e, act.ID)
		require.NoError(t, servers.NewDeleteServerWorker(so).Work(ctx, jobOf(args, 1)))
		got, err := actions.Get(ctx, a.Ent, e.Project, act.ID)
		require.NoError(t, err)
		require.Equal(t, actions.StatusSuccess, got.Status, "%+v", got.Error)
	}

	const iterations = 20
	attached := 0
	for i := range iterations {
		s := e.RunningServer(fmt.Sprintf("web-%d", i))
		vmid := vmidOf(s)
		start := make(chan struct{})
		var wg sync.WaitGroup
		var delAct, attAct actions.Action
		var delErr, attErr error
		// Stagger either side so both orders, and near-ties, happen.
		delay := time.Duration(i%5) * 700 * time.Microsecond
		wg.Go(func() {
			<-start
			if i%2 == 1 {
				time.Sleep(delay)
			}
			delAct, delErr = srvs.Delete(ctx, e.Project, s.ServerID)
		})
		wg.Go(func() {
			<-start
			if i%2 == 0 {
				time.Sleep(delay)
			}
			attAct, attErr = e.svc.Attach(ctx, e.Project, vid, ref(s.ServerID))
		})
		close(start)
		wg.Wait()

		require.True(t, (delErr == nil) != (attErr == nil), "iteration %d: exactly one wins: delete=%v attach=%v", i, delErr, attErr)
		if delErr != nil {
			requireAPIErr(t, delErr, http.StatusConflict, "locked")
			attached++
			e.ok(t, o, attAct, nil)
			require.NotEmpty(t, e.keysFor(t, vmid, vid))
			r := e.row(t, vid)
			require.Equal(t, volume.StatusAttached, r.Status)
			require.Equal(t, s.ServerID, *r.ServerID)
			delAct, delErr = srvs.Delete(ctx, e.Project, s.ServerID)
			require.NoError(t, delErr)
		} else {
			requireAPIErr(t, attErr, http.StatusConflict, "locked")
		}
		deleteServer(delAct)

		_, err := a.Ent.Server.Get(ctx, s.ServerID)
		require.True(t, ent.IsNotFound(err), "iteration %d", i)
		r := e.row(t, vid)
		require.Equal(t, volume.StatusAvailable, r.Status, "iteration %d", i)
		require.Nil(t, r.ServerID)
		require.Empty(t, r.Device)
		require.Len(t, e.disks(t, vid), 1, "disk kept")
	}
	t.Logf("attach won %d of %d", attached, iterations)
}

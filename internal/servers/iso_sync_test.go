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
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/floatingips"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v4"
)

const wantVendorData = "#cloud-config\nupdates:\n  network:\n    when: [boot]\n"

func genVolid(id int64, gen int) string { return fmt.Sprintf("local:iso/nb-srv-%d-%d.iso", id, gen) }

func instanceID(t *testing.T, files map[string]string) string {
	t.Helper()
	var meta map[string]string
	require.NoError(t, yaml.Unmarshal([]byte(files["meta-data"]), &meta))
	return meta["instance-id"]
}

func srvRow(t *testing.T, e *testutil.ServerEnv, id int64) *ent.Server {
	t.Helper()
	row, err := e.App.Ent.Server.Get(context.Background(), id)
	require.NoError(t, err)
	return row
}

// ide2 is the volid the server's VM has in its cdrom.
func ide2(t *testing.T, e *testutil.ServerEnv, id int64) string {
	t.Helper()
	vm, ok := e.App.PVE.VM(vmidOf(t, e, id))
	require.True(t, ok)
	return vm.Config["ide2"]
}

// queuedSyncs counts the server's sync_iso jobs waiting to run.
func queuedSyncs(t *testing.T, e *testutil.ServerEnv, id int64) int {
	t.Helper()
	var n int
	require.NoError(t, e.App.SQL.QueryRow(`select count(*) from river_job where kind = $1 and (args->>'server_id')::bigint = $2
		and state in ('available', 'scheduled', 'pending')`, servers.KindSyncISO, id).Scan(&n))
	return n
}

func syncNow(t *testing.T, o servers.WorkerOptions, id int64, bump bool) bool {
	t.Helper()
	changed, err := servers.SyncISO(context.Background(), o, id, bump)
	require.NoError(t, err)
	return changed
}

func TestNewServerHasNetRefresh(t *testing.T) {
	e, cw := directEnv(t)
	s := builtServer(t, e, cw, "web")
	row := srvRow(t, e, s.ServerID)
	require.True(t, row.NetRefresh)
	require.Zero(t, row.InstanceGeneration, "0 follows iso_generation")
	require.Equal(t, row.IsoGeneration, servers.InstanceGeneration(row))
	require.NotEmpty(t, row.IsoNetHash)
	files := isoFiles(t, e, isoVolid(s.ServerID))
	require.Equal(t, wantVendorData, files["vendor-data"])
	require.Equal(t, fmt.Sprintf("nb-srv-%d-1", s.ServerID), instanceID(t, files))

	// The stored hash is what the ISO holds, so a sync has nothing to do.
	require.False(t, syncNow(t, opts(e), s.ServerID, false))
}

func TestRebuildSetsNetRefresh(t *testing.T) {
	ctx := context.Background()
	e, s, w := rebuildEnv(t)
	// A server from before IP changes: no opt-in on its ISO.
	require.NoError(t, e.App.Ent.Server.UpdateOneID(s.ServerID).SetNetRefresh(false).SetIsoNetHash("").Exec(ctx))
	id := startAction(t, e, s.ServerID, "rebuild", map[string]any{"image": "debian"})
	require.NoError(t, w.Work(ctx, jobOf(queuedArgs[servers.RebuildServerArgs](t, e, id), 1)))
	row := srvRow(t, e, s.ServerID)
	require.Equal(t, 2, row.IsoGeneration)
	require.True(t, row.NetRefresh)
	require.Equal(t, 2, servers.InstanceGeneration(row))
	require.NotEmpty(t, row.IsoNetHash)
	files := isoFiles(t, e, genVolid(s.ServerID, 2))
	require.Equal(t, wantVendorData, files["vendor-data"])
	require.Equal(t, fmt.Sprintf("nb-srv-%d-2", s.ServerID), instanceID(t, files))
	require.False(t, syncNow(t, opts(e), s.ServerID, false))
}

func TestISOSyncSkipsUnchanged(t *testing.T) {
	e, cw := directEnv(t)
	s := builtServer(t, e, cw, "web")
	before := e.App.PVE.Uploads()
	require.False(t, syncNow(t, opts(e), s.ServerID, false))
	require.Equal(t, before, e.App.PVE.Uploads())
	require.Equal(t, 1, srvRow(t, e, s.ServerID).IsoGeneration)
}

func TestISOSyncRewritesOnFloatingAssign(t *testing.T) {
	f := newFIPEnv(t)
	_, ip := f.settled(t)
	require.Equal(t, 1, queuedSyncs(t, f.ServerEnv, f.a), "the floating IP job queues a sync")

	require.NoError(t, servers.NewSyncISOWorker(f.o).Work(context.Background(), jobOf(servers.SyncISOArgs{ServerID: f.a}, 1)))
	row := srvRow(t, f.ServerEnv, f.a)
	require.Equal(t, 2, row.IsoGeneration)
	require.Equal(t, 1, row.InstanceGeneration, "kept instance is stored before iso_generation moves on")
	require.Equal(t, genVolid(f.a, 2), *row.IsoVolid)
	require.Equal(t, genVolid(f.a, 2)+",media=cdrom", ide2(t, f.ServerEnv, f.a))
	files := isoFiles(t, f.ServerEnv, genVolid(f.a, 2))
	require.Contains(t, files["network-config"], "- "+ip+"/32\n")
	require.Equal(t, fmt.Sprintf("nb-srv-%d-1", f.a), instanceID(t, files), "same instance: user data does not re-run")
	require.Equal(t, wantVendorData, files["vendor-data"])
	_, ok := f.App.PVE.Volume(genVolid(f.a, 1))
	require.False(t, ok, "old ISO deleted")
	vm, _ := f.App.PVE.VM(f.va)
	require.Equal(t, "running", vm.Status, "the cdrom swaps on a running VM")
	require.False(t, syncNow(t, f.o, f.a, false))
}

func TestISOSyncKeepsFollowingInstanceGeneration(t *testing.T) {
	ctx := context.Background()
	f := newFIPEnv(t)
	_, ip := f.settled(t)
	// An old server: instance_generation 0 follows iso_generation, which earlier rebuilds moved to 4.
	require.NoError(t, f.App.Ent.Server.UpdateOneID(f.a).SetIsoGeneration(4).SetInstanceGeneration(0).Exec(ctx))
	require.True(t, syncNow(t, f.o, f.a, false))
	row := srvRow(t, f.ServerEnv, f.a)
	require.Equal(t, 5, row.IsoGeneration)
	require.Equal(t, 4, row.InstanceGeneration)
	files := isoFiles(t, f.ServerEnv, genVolid(f.a, 5))
	require.Contains(t, files["network-config"], "- "+ip+"/32\n")
	require.Equal(t, fmt.Sprintf("nb-srv-%d-4", f.a), instanceID(t, files))
}

func TestISOSyncBumpsInstanceOnOldServer(t *testing.T) {
	ctx := context.Background()
	e, cw := directEnv(t)
	s := builtServer(t, e, cw, "web")
	require.NoError(t, e.App.Ent.Server.UpdateOneID(s.ServerID).SetNetRefresh(false).Exec(ctx))
	require.True(t, syncNow(t, opts(e), s.ServerID, true))
	row := srvRow(t, e, s.ServerID)
	require.Equal(t, 2, row.IsoGeneration)
	require.Equal(t, 2, servers.InstanceGeneration(row))
	files := isoFiles(t, e, genVolid(s.ServerID, 2))
	require.Equal(t, fmt.Sprintf("nb-srv-%d-2", s.ServerID), instanceID(t, files))
	require.NotContains(t, files, "vendor-data")
	require.Equal(t, genVolid(s.ServerID, 2)+",media=cdrom", ide2(t, e, s.ServerID))
}

func TestISOSyncSerializes(t *testing.T) {
	e, cw := directEnv(t)
	s := builtServer(t, e, cw, "web")
	before := len(e.App.PVE.Uploads())
	o := opts(e)
	var wg sync.WaitGroup
	errs := make([]error, 4)
	for i := range errs {
		wg.Go(func() { _, errs[i] = servers.SyncISO(context.Background(), o, s.ServerID, true) })
	}
	wg.Wait()
	for _, err := range errs {
		require.NoError(t, err)
	}
	ups := e.App.PVE.Uploads()[before:]
	require.Len(t, ups, 4)
	require.Len(t, slices.Compact(slices.Sorted(slices.Values(ups))), 4, "never two uploads of one name: %v", ups)
	row := srvRow(t, e, s.ServerID)
	require.Equal(t, 5, row.IsoGeneration)
	require.Equal(t, genVolid(s.ServerID, 5)+",media=cdrom", ide2(t, e, s.ServerID))
	for gen := 1; gen < 5; gen++ {
		_, ok := e.App.PVE.Volume(genVolid(s.ServerID, gen))
		require.False(t, ok, "gen %d deleted", gen)
	}
}

func TestRebuildEndsWithFreshISO(t *testing.T) {
	ctx := context.Background()
	f := newFIPEnv(t)
	f.App.PVE.AddTemplate("pve1", 9001, "ceph")
	f.Admin.CreateImage("debian", f.Location, 9001)
	id := startAction(t, f.ServerEnv, f.a, "rebuild", map[string]any{"image": "debian"})
	w := servers.NewRebuildServerWorker(f.o)
	sw := servers.NewSyncISOWorker(f.o)
	var fip floatingips.FloatingIP
	servers.SetRebuildAfterStep(w, func(n int) {
		if n != servers.StepFirewall {
			return
		}
		// The new VM's ISO is written; an assign commits before the swap.
		var err error
		fip, _, err = f.svc.Create(ctx, f.Project, floatingips.CreateInput{Family: "ipv4", Server: strconv.FormatInt(f.a, 10)})
		require.NoError(t, err)
		ups := len(f.App.PVE.Uploads())
		err = sw.Work(ctx, jobOf(servers.SyncISOArgs{ServerID: f.a}, 1))
		require.True(t, snoozed(err), "a sync mid-rebuild waits: %v", err)
		require.Len(t, f.App.PVE.Uploads(), ups, "a sync mid-rebuild writes no ISO")
	})
	require.NoError(t, w.Work(ctx, jobOf(queuedArgs[servers.RebuildServerArgs](t, f.ServerEnv, id), 1)))
	require.Equal(t, actions.StatusSuccess, f.action(t, id).Status)
	require.NotContains(t, isoFiles(t, f.ServerEnv, genVolid(f.a, 2))["network-config"], fip.IP)
	require.Equal(t, 1, queuedSyncs(t, f.ServerEnv, f.a), "the rebuild queues a sync when it ends")

	require.NoError(t, sw.Work(ctx, jobOf(servers.SyncISOArgs{ServerID: f.a}, 1)))
	row := srvRow(t, f.ServerEnv, f.a)
	require.Equal(t, 3, row.IsoGeneration)
	require.Equal(t, 2, servers.InstanceGeneration(row))
	require.Equal(t, genVolid(f.a, 3)+",media=cdrom", ide2(t, f.ServerEnv, f.a))
	require.Contains(t, isoFiles(t, f.ServerEnv, genVolid(f.a, 3))["network-config"], "- "+fip.IP+"/32\n")
}

func TestISOSyncDoesNotStarvePool(t *testing.T) {
	e, cw := directEnv(t)
	n := int(e.App.Pool.Config().MaxConns) + 2
	e.Admin.SetQuota(e.Project, n, 1000, 1_000_000, 100_000, 2*n)
	e.Admin.CreateIPPool(e.Location, "ipv4", "10.0.1.0/24", "10.0.1.1", "10.0.1.10", "10.0.1.250")
	ids := make([]int64, n)
	for i := range ids {
		ids[i] = builtServer(t, e, cw, fmt.Sprintf("web%d", i)).ServerID
	}
	// Slow Proxmox uploads: a sync must not hold a pool conn while it waits.
	e.App.PVE.SetTaskDelay(300 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	o := opts(e)
	start := time.Now()
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i, id := range ids {
		wg.Go(func() { _, errs[i] = servers.SyncISO(ctx, o, id, true) })
	}
	wg.Wait()
	for _, err := range errs {
		require.NoError(t, err)
	}
	require.Less(t, time.Since(start), 10*time.Second)
	for _, id := range ids {
		require.Equal(t, 2, srvRow(t, e, id).IsoGeneration)
	}
}

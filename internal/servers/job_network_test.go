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
	"database/sql"
	"strconv"
	"testing"
	"time"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/db"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/action"
	"github.com/nebla-cloud/nebla/internal/ent/servernetwork"
	"github.com/nebla-cloud/nebla/internal/firewalls"
	"github.com/nebla-cloud/nebla/internal/jobs"
	"github.com/nebla-cloud/nebla/internal/networks"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/require"
)

const netMAC = "BC:24:11:AA:00:01"

type netEnv struct {
	*testutil.ServerEnv
	w      *servers.NetworkWorker
	srv    int64
	vmid   int
	netID  int64
	vnet   string
	nextIP int
}

func newNetEnv(t *testing.T) *netEnv {
	t.Helper()
	servers.SetSettleWait(t, func(context.Context, time.Duration) error { return nil })
	e, cw := directEnv(t)
	s := builtServer(t, e, cw, "web")
	n := &netEnv{ServerEnv: e, srv: s.ServerID, vmid: vmidOf(t, e, s.ServerID), nextIP: 2}
	o := opts(e)
	o.Jobs = func() *jobs.Jobs { return e.App.Jobs }
	n.w = servers.NewNetworkWorker(o)
	n.netID = n.network(t, "nb0001", "vxlan", 1450)
	return n
}

// network creates a network in a slot and returns its id.
func (n *netEnv) network(t *testing.T, vnet, zoneType string, mtu int) int64 {
	t.Helper()
	ctx := context.Background()
	row := n.App.Ent.Network.Create().SetProjectID(n.Project).SetLocationID(n.Location).SetName(vnet).SetLabels(map[string]string{}).
		SetIPRange("10.9.0.0/24").SaveX(ctx)
	n.App.Ent.VNetSlot.Create().SetLocationID(n.Location).SetVnet(vnet).SetZone("z").SetZoneType(zoneType).SetMtu(mtu).
		SetState("in_use").SetNetworkID(row.ID).ExecX(ctx)
	return row.ID
}

// attachRow adds the server's row on a network.
func (n *netEnv) attachRow(t *testing.T, netID int64, nic int, st servernetwork.State) {
	t.Helper()
	n.App.Ent.ServerNetwork.Create().SetServerID(n.srv).SetNetworkID(netID).SetIP("10.9.0." + strconv.Itoa(n.nextIP)).SetNic(nic).
		SetState(st).SetMAC(netMAC).ExecX(context.Background())
	n.nextIP++
}

func (n *netEnv) start(t *testing.T, kind string, netID int64) int64 {
	t.Helper()
	var act actions.Action
	err := db.WithTx(context.Background(), n.App.SQL, func(tx *sql.Tx, etx *ent.Client) error {
		var err error
		act, err = n.App.Jobs.StartAction(context.Background(), tx, etx, n.Project, kind,
			[]actions.Resource{{ID: n.srv, Type: jobs.ServerResource}, {ID: netID, Type: networks.ResourceType}},
			func(id int64) river.JobArgs {
				if kind == networks.KindAttach {
					return networks.AttachArgs{ActionID: id, ServerID: n.srv, NetworkID: netID}
				}
				return networks.DetachArgs{ActionID: id, ServerID: n.srv, NetworkID: netID}
			}, nil)
		return err
	})
	require.NoError(t, err)
	return act.ID
}

func (n *netEnv) attach(t *testing.T, netID int64, attempt int) (int64, error) {
	t.Helper()
	id := n.start(t, networks.KindAttach, netID)
	return id, n.w.Attach().Work(context.Background(), jobOf(queuedArgs[networks.AttachArgs](t, n.ServerEnv, id), attempt))
}

func (n *netEnv) detachJob(t *testing.T, id int64, attempt int) error {
	t.Helper()
	return n.w.Detach().Work(context.Background(), jobOf(queuedArgs[networks.DetachArgs](t, n.ServerEnv, id), attempt))
}

func (n *netEnv) act(t *testing.T, id int64) *ent.Action {
	t.Helper()
	a, err := n.App.Ent.Action.Get(context.Background(), id)
	require.NoError(t, err)
	return a
}

func (n *netEnv) rowState(t *testing.T, netID int64) (servernetwork.State, bool) {
	t.Helper()
	r, err := n.App.Ent.ServerNetwork.Query().Where(servernetwork.ServerID(n.srv), servernetwork.NetworkID(netID)).Only(context.Background())
	if ent.IsNotFound(err) {
		return "", false
	}
	require.NoError(t, err)
	return r.State, true
}

func TestAttachRunningServer(t *testing.T) {
	n := newNetEnv(t)
	n.App.PVE.SetVMStatus(n.vmid, "running")
	n.attachRow(t, n.netID, 1, servernetwork.StateAttaching)
	id, err := n.attach(t, n.netID, 1)
	require.NoError(t, err)
	vm, _ := n.App.PVE.VM(n.vmid)
	require.Equal(t, "virtio="+netMAC+",bridge=nb0001,firewall=1,mtu=1450", vm.Config["net1"])
	require.Equal(t, []string{"10.9.0.2"}, vm.IPSets["ipfilter-net1"])
	st, ok := n.rowState(t, n.netID)
	require.True(t, ok)
	require.Equal(t, servernetwork.StateAttached, st)
	require.Equal(t, action.StatusSuccess, n.act(t, id).Status)
}

func TestAttachIsIdempotent(t *testing.T) {
	n := newNetEnv(t)
	n.attachRow(t, n.netID, 1, servernetwork.StateAttaching)
	_, err := n.attach(t, n.netID, 1)
	require.NoError(t, err)
	id, err := n.attach(t, n.netID, 1)
	require.NoError(t, err)
	require.Equal(t, action.StatusSuccess, n.act(t, id).Status)
}

func TestAttachRowGoneSucceeds(t *testing.T) {
	n := newNetEnv(t)
	id, err := n.attach(t, n.netID, 1)
	require.NoError(t, err)
	require.Equal(t, action.StatusSuccess, n.act(t, id).Status)
	vm, _ := n.App.PVE.VM(n.vmid)
	require.NotContains(t, vm.Config, "net1")
}

func TestAttachWritesSetBeforeNIC(t *testing.T) {
	n := newNetEnv(t)
	n.attachRow(t, n.netID, 1, servernetwork.StateAttaching)
	var sawSetWithoutNIC bool
	n.App.PVE.OnIPSetWrite(func() {
		vm, _ := n.App.PVE.VM(n.vmid)
		if _, has := vm.Config["net1"]; !has && len(vm.IPSets["ipfilter-net1"]) > 0 {
			sawSetWithoutNIC = true
		}
	})
	_, err := n.attach(t, n.netID, 1)
	require.NoError(t, err)
	require.True(t, sawSetWithoutNIC, "ipfilter-net1 must be written while net1 is absent")
	vm, _ := n.App.PVE.VM(n.vmid)
	require.Contains(t, vm.Config, "net1")
}

func TestAttachMTU(t *testing.T) {
	n := newNetEnv(t)
	simple := n.network(t, "nb0002", "simple", 1500)
	n.attachRow(t, n.netID, 1, servernetwork.StateAttaching)
	n.attachRow(t, simple, 2, servernetwork.StateAttaching)
	_, err := n.attach(t, n.netID, 1)
	require.NoError(t, err)
	_, err = n.attach(t, simple, 1)
	require.NoError(t, err)
	vm, _ := n.App.PVE.VM(n.vmid)
	require.Equal(t, "virtio="+netMAC+",bridge=nb0001,firewall=1,mtu=1450", vm.Config["net1"])
	require.Equal(t, "virtio="+netMAC+",bridge=nb0002,firewall=1", vm.Config["net2"])
}

func TestAttachHotplugOff(t *testing.T) {
	n := newNetEnv(t)
	n.App.PVE.SetVMStatus(n.vmid, "running")
	n.App.PVE.SetVMConfig(n.vmid, "hotplug", "disk")
	n.attachRow(t, n.netID, 1, servernetwork.StateAttaching)
	id, err := n.attach(t, n.netID, 1)
	require.Error(t, err)
	a := n.act(t, id)
	require.Equal(t, action.StatusError, a.Status)
	require.Equal(t, actions.CodePowerOffRequired, *a.ErrorCode)
	_, ok := n.rowState(t, n.netID)
	require.False(t, ok)
	vm, _ := n.App.PVE.VM(n.vmid)
	require.NotContains(t, vm.Config, "net1")
	require.NotContains(t, vm.IPSets, "ipfilter-net1")
}

func TestAttachStoppedServer(t *testing.T) {
	n := newNetEnv(t)
	n.App.PVE.SetVMStatus(n.vmid, "stopped")
	n.App.PVE.SetVMConfig(n.vmid, "hotplug", "disk")
	n.attachRow(t, n.netID, 1, servernetwork.StateAttaching)
	id, err := n.attach(t, n.netID, 1)
	require.NoError(t, err)
	require.Equal(t, action.StatusSuccess, n.act(t, id).Status)
	vm, _ := n.App.PVE.VM(n.vmid)
	require.Contains(t, vm.Config, "net1")
}

func TestAttachMACCaseInsensitive(t *testing.T) {
	n := newNetEnv(t)
	n.attachRow(t, n.netID, 1, servernetwork.StateAttaching)
	n.App.PVE.SetVMConfig(n.vmid, "net1", "virtio=bc:24:11:aa:00:01,bridge=nb0001,firewall=1,mtu=1450")
	before := countCalls(n.ServerEnv, "POST /nodes/pve1/qemu/"+strconv.Itoa(n.vmid)+"/config")
	_, err := n.attach(t, n.netID, 1)
	require.NoError(t, err)
	require.Equal(t, before, countCalls(n.ServerEnv, "POST /nodes/pve1/qemu/"+strconv.Itoa(n.vmid)+"/config"))
}

func TestAttachFinalFailureDropsRow(t *testing.T) {
	n := newNetEnv(t)
	n.attachRow(t, n.netID, 1, servernetwork.StateAttaching)
	require.NoError(t, n.App.Ent.Server.UpdateOneID(n.srv).ClearVmid().Exec(context.Background()))
	id := n.start(t, networks.KindAttach, n.netID)
	args := queuedArgs[networks.AttachArgs](t, n.ServerEnv, id)
	require.Error(t, n.w.Attach().Work(context.Background(), jobOf(args, 1)))
	_, ok := n.rowState(t, n.netID)
	require.True(t, ok)
	require.Error(t, n.w.Attach().Work(context.Background(), jobOf(args, servers.MaxAttempts+1)))
	_, ok = n.rowState(t, n.netID)
	require.False(t, ok)
	require.Equal(t, action.StatusError, n.act(t, id).Status)
}

func (n *netEnv) detachSetup(t *testing.T) {
	t.Helper()
	n.attachRow(t, n.netID, 1, servernetwork.StateAttaching)
	_, err := n.attach(t, n.netID, 1)
	require.NoError(t, err)
	n.App.Ent.ServerNetwork.Update().Where(servernetwork.ServerID(n.srv)).SetState(servernetwork.StateDetaching).ExecX(context.Background())
}

func TestDetachRemovesNICAndSet(t *testing.T) {
	n := newNetEnv(t)
	n.App.PVE.SetVMStatus(n.vmid, "running")
	n.detachSetup(t)
	id := n.start(t, networks.KindDetach, n.netID)
	require.NoError(t, n.detachJob(t, id, 1))
	vm, _ := n.App.PVE.VM(n.vmid)
	require.NotContains(t, vm.Config, "net1")
	require.NotContains(t, vm.IPSets, "ipfilter-net1")
	_, ok := n.rowState(t, n.netID)
	require.False(t, ok)
	require.Equal(t, action.StatusSuccess, n.act(t, id).Status)
}

func TestDetachPendingKeepsSet(t *testing.T) {
	n := newNetEnv(t)
	n.App.PVE.SetVMStatus(n.vmid, "running")
	n.detachSetup(t)
	n.App.PVE.SetPendingUnplug(n.vmid, "net1", 2)
	id := n.start(t, networks.KindDetach, n.netID)
	args := queuedArgs[networks.DetachArgs](t, n.ServerEnv, id)
	for attempt := 1; attempt <= 2; attempt++ {
		require.Error(t, n.detachJob2(args, attempt))
		st, ok := n.rowState(t, n.netID)
		require.True(t, ok)
		require.Equal(t, servernetwork.StateDetaching, st)
		vm, _ := n.App.PVE.VM(n.vmid)
		require.Equal(t, []string{"10.9.0.2"}, vm.IPSets["ipfilter-net1"])
	}
	require.NoError(t, n.detachJob2(args, 3))
	vm, _ := n.App.PVE.VM(n.vmid)
	require.NotContains(t, vm.Config, "net1")
	require.NotContains(t, vm.IPSets, "ipfilter-net1")
	_, ok := n.rowState(t, n.netID)
	require.False(t, ok)
}

func (n *netEnv) detachJob2(args networks.DetachArgs, attempt int) error {
	return n.w.Detach().Work(context.Background(), jobOf(args, attempt))
}

func TestDetachFinalFailureKeepsRow(t *testing.T) {
	n := newNetEnv(t)
	n.App.PVE.SetVMStatus(n.vmid, "running")
	n.detachSetup(t)
	n.App.PVE.SetPendingUnplug(n.vmid, "net1", 100)
	id := n.start(t, networks.KindDetach, n.netID)
	args := queuedArgs[networks.DetachArgs](t, n.ServerEnv, id)
	require.Error(t, n.detachJob2(args, servers.MaxAttempts+1))
	st, ok := n.rowState(t, n.netID)
	require.True(t, ok)
	require.Equal(t, servernetwork.StateDetaching, st)
	require.Equal(t, action.StatusError, n.act(t, id).Status)
}

func TestDetachDuringRebuildWritesBothVMs(t *testing.T) {
	n := newNetEnv(t)
	ctx := context.Background()
	n.detachSetup(t)
	rb := n.vmid + 500
	n.App.PVE.AddVM("pve1", rb, "web-rebuild")
	n.App.PVE.SetVMConfig(rb, "tags", servers.TagNebla+";"+servers.ServerTag(n.srv))
	n.App.PVE.SetVMConfig(rb, "net1", "virtio="+netMAC+",bridge=nb0001,firewall=1,mtu=1450")
	n.App.PVE.SetVMIPSet(rb, "ipfilter-net1", []string{"10.9.0.2"})
	// A finished rebuild can leave its VMID set until cleanup; the server is no longer building.
	require.NoError(t, n.App.Ent.Server.UpdateOneID(n.srv).SetRebuildVmid(rb).Exec(ctx))
	id := n.start(t, networks.KindDetach, n.netID)
	require.NoError(t, n.detachJob(t, id, 1))
	for _, v := range []int{n.vmid, rb} {
		vm, _ := n.App.PVE.VM(v)
		require.NotContains(t, vm.Config, "net1", "vm %d", v)
		require.NotContains(t, vm.IPSets, "ipfilter-net1", "vm %d", v)
	}
}

func TestDetachRowGoneStillApplies(t *testing.T) {
	n := newNetEnv(t)
	n.detachSetup(t)
	n.App.Ent.ServerNetwork.Delete().ExecX(context.Background())
	id := n.start(t, networks.KindDetach, n.netID)
	require.NoError(t, n.detachJob(t, id, 1))
	vm, _ := n.App.PVE.VM(n.vmid)
	require.NotContains(t, vm.IPSets, "ipfilter-net1")
	require.Equal(t, action.StatusSuccess, n.act(t, id).Status)
}

func TestNetworkJobsSnoozeWhileBuilding(t *testing.T) {
	n := newNetEnv(t)
	n.detachSetup(t)
	require.NoError(t, n.App.Ent.Server.UpdateOneID(n.srv).SetStatus(servers.StatusRebuilding).SetRebuildStep(servers.StepClone).Exec(context.Background()))
	id := n.start(t, networks.KindDetach, n.netID)
	var snooze *rivertype.JobSnoozeError
	require.ErrorAs(t, n.detachJob(t, id, 1), &snooze)
	vm, _ := n.App.PVE.VM(n.vmid)
	require.Contains(t, vm.Config, "net1")
	n.App.Ent.ServerNetwork.Update().SetState(servernetwork.StateAttaching).ExecX(context.Background())
	_, err := n.attach(t, n.netID, 1)
	require.ErrorAs(t, err, &snooze)
}

func TestDetachHotplugOffNeedsPowerOff(t *testing.T) {
	n := newNetEnv(t)
	n.App.PVE.SetVMStatus(n.vmid, "running")
	n.detachSetup(t)
	n.App.PVE.SetVMConfig(n.vmid, "hotplug", "disk")
	id := n.start(t, networks.KindDetach, n.netID)
	require.Error(t, n.detachJob(t, id, 1))
	a := n.act(t, id)
	require.Equal(t, action.StatusError, a.Status)
	require.Equal(t, actions.CodePowerOffRequired, *a.ErrorCode)
	st, ok := n.rowState(t, n.netID)
	require.True(t, ok)
	require.Equal(t, servernetwork.StateDetaching, st)
}

// strayAttach leaves the VM holding net1 and its set, as an attempt that failed after the write would.
func (n *netEnv) strayAttach(t *testing.T) int64 {
	t.Helper()
	n.attachRow(t, n.netID, 1, servernetwork.StateAttaching)
	_, err := n.attach(t, n.netID, 1)
	require.NoError(t, err)
	n.App.Ent.ServerNetwork.Update().SetState(servernetwork.StateAttaching).ExecX(context.Background())
	return n.start(t, networks.KindAttach, n.netID)
}

func TestAttachFinalFailureAfterWriteRemovesNICAndSet(t *testing.T) {
	n := newNetEnv(t)
	n.App.PVE.SetVMStatus(n.vmid, "running")
	id := n.strayAttach(t)
	vm, _ := n.App.PVE.VM(n.vmid)
	require.Contains(t, vm.Config, "net1")
	require.Contains(t, vm.IPSets, "ipfilter-net1")
	args := queuedArgs[networks.AttachArgs](t, n.ServerEnv, id)
	require.Error(t, n.w.Attach().Work(context.Background(), jobOf(args, servers.MaxAttempts+1)))
	vm, _ = n.App.PVE.VM(n.vmid)
	require.NotContains(t, vm.Config, "net1")
	require.NotContains(t, vm.IPSets, "ipfilter-net1")
	_, ok := n.rowState(t, n.netID)
	require.False(t, ok)
	require.Equal(t, action.StatusError, n.act(t, id).Status)
}

func TestAttachFinalFailureUnplugFailsQueuesApply(t *testing.T) {
	n := newNetEnv(t)
	ctx := context.Background()
	n.App.PVE.SetVMStatus(n.vmid, "running")
	id := n.strayAttach(t)
	before := n.App.Ent.Server.GetX(ctx, n.srv).FirewallGenDesired
	n.App.PVE.SetPendingUnplug(n.vmid, "net1", 100)
	// Another apply holds the lease, so the inline apply is skipped.
	_, err := n.App.SQL.ExecContext(ctx, `update servers set firewall_lease_until = now() + interval '1 hour' where id = $1`, n.srv)
	require.NoError(t, err)
	args := queuedArgs[networks.AttachArgs](t, n.ServerEnv, id)
	require.Error(t, n.w.Attach().Work(ctx, jobOf(args, servers.MaxAttempts+1)))
	_, ok := n.rowState(t, n.netID)
	require.False(t, ok)
	require.Equal(t, before+1, n.App.Ent.Server.GetX(ctx, n.srv).FirewallGenDesired)
	queued, err := n.App.Ent.Action.Query().Where(servers.ActionsOf(n.Project, n.srv), action.Command(firewalls.KindApply),
		action.StatusEQ(action.StatusRunning)).Count(ctx)
	require.NoError(t, err)
	require.Positive(t, queued)
}

// syncISO runs the server's queued seed ISO sync.
func (n *netEnv) syncISO(t *testing.T) *ent.Server {
	t.Helper()
	o := opts(n.ServerEnv)
	require.NoError(t, servers.NewSyncISOWorker(o).Work(context.Background(), jobOf(servers.SyncISOArgs{ServerID: n.srv}, 1)))
	return srvRow(t, n.ServerEnv, n.srv)
}

func TestAttachAndDetachSyncTheSeedISO(t *testing.T) {
	ctx := context.Background()
	for _, refresh := range []bool{true, false} {
		t.Run("net_refresh="+strconv.FormatBool(refresh), func(t *testing.T) {
			n := newNetEnv(t)
			require.NoError(t, n.App.Ent.Server.UpdateOneID(n.srv).SetNetRefresh(refresh).Exec(ctx))
			before := srvRow(t, n.ServerEnv, n.srv)
			n.App.PVE.SetVMStatus(n.vmid, "stopped")
			require.Zero(t, queuedSyncs(t, n.ServerEnv, n.srv))

			n.attachRow(t, n.netID, 1, servernetwork.StateAttaching)
			_, err := n.attach(t, n.netID, 1)
			require.NoError(t, err)
			require.Equal(t, 1, queuedSyncs(t, n.ServerEnv, n.srv), "a finished attach queues a seed ISO sync")

			row := n.syncISO(t)
			require.Equal(t, before.IsoGeneration+1, row.IsoGeneration)
			require.Equal(t, servers.InstanceGeneration(before), servers.InstanceGeneration(row), "the instance-id is kept")
			require.Equal(t, refresh, row.NetRefresh)
			files := isoFiles(t, n.ServerEnv, *row.IsoVolid)
			nc := files["network-config"]
			require.Contains(t, nc, "eth1:")
			require.Contains(t, nc, "macaddress: bc:24:11:aa:00:01")
			require.Contains(t, nc, "- 10.9.0.2/24")
			require.Contains(t, nc, "mtu: 1450")
			due, err := servers.ISOSyncDue(ctx, n.App.Ent, row)
			require.NoError(t, err)
			require.False(t, due)

			n.App.Ent.ServerNetwork.Update().Where(servernetwork.ServerID(n.srv)).SetState(servernetwork.StateDetaching).ExecX(ctx)
			id := n.start(t, networks.KindDetach, n.netID)
			require.NoError(t, n.detachJob(t, id, 1))
			require.Equal(t, 1, queuedSyncs(t, n.ServerEnv, n.srv), "a finished detach queues a seed ISO sync")
			row = n.syncISO(t)
			require.Equal(t, before.IsoGeneration+2, row.IsoGeneration)
			require.NotContains(t, isoFiles(t, n.ServerEnv, *row.IsoVolid)["network-config"], "eth1")
		})
	}
}

func TestAttachSyncAppliesPendingBump(t *testing.T) {
	ctx := context.Background()
	n := newNetEnv(t)
	require.NoError(t, n.App.Ent.Server.UpdateOneID(n.srv).SetNetRefresh(false).SetInstanceBumpPending(true).Exec(ctx))
	before := srvRow(t, n.ServerEnv, n.srv)
	n.App.PVE.SetVMStatus(n.vmid, "stopped")
	n.attachRow(t, n.netID, 1, servernetwork.StateAttaching)
	_, err := n.attach(t, n.netID, 1)
	require.NoError(t, err)
	require.Equal(t, 1, queuedSyncs(t, n.ServerEnv, n.srv))

	row := n.syncISO(t)
	require.Equal(t, before.IsoGeneration+1, row.IsoGeneration)
	require.Equal(t, row.IsoGeneration, servers.InstanceGeneration(row), "the pending bump moves the instance-id")
	require.NotEqual(t, servers.InstanceGeneration(before), servers.InstanceGeneration(row))
	require.True(t, row.NetRefresh)
	require.False(t, row.InstanceBumpPending)
}

func TestFailedAttachQueuesISOSync(t *testing.T) {
	n := newNetEnv(t)
	n.attachRow(t, n.netID, 1, servernetwork.StateAttaching)
	id := n.start(t, networks.KindAttach, n.netID)
	args := queuedArgs[networks.AttachArgs](t, n.ServerEnv, id)
	// Past the last attempt the worker fails the action and drops the row.
	require.Error(t, n.w.Attach().Work(context.Background(), jobOf(args, servers.MaxAttempts+1)))
	_, ok := n.rowState(t, n.netID)
	require.False(t, ok)
	require.Equal(t, 1, queuedSyncs(t, n.ServerEnv, n.srv))
}

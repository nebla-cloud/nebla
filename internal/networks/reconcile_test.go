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

package networks_test

import (
	"context"
	"testing"
	"time"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/ent/action"
	"github.com/nebla-cloud/nebla/internal/ent/server"
	"github.com/nebla-cloud/nebla/internal/ent/servernetwork"
	"github.com/nebla-cloud/nebla/internal/ent/vnetslot"
	"github.com/nebla-cloud/nebla/internal/jobs"
	"github.com/nebla-cloud/nebla/internal/networks"
	"github.com/nebla-cloud/nebla/internal/pve"
	"github.com/stretchr/testify/require"
)

// coolingEnv has nb0001 and nb0002 discovered, with nb0001 cooling.
func coolingEnv(t *testing.T) *sdnEnv {
	t.Helper()
	e := newSDNEnv(t, "nb0001", "nb0002")
	require.NoError(t, e.svc.Discover(context.Background(), e.pc, e.loc))
	e.App.Ent.VNetSlot.Update().Where(vnetslot.Vnet("nb0001")).SetState(vnetslot.StateCooling).ExecX(context.Background())
	return e
}

func TestCoolingNeedsCleanPass(t *testing.T) {
	ctx := context.Background()
	e := coolingEnv(t)
	e.pv.AddForeignGuest(pve.GuestRef{VMID: 777, Node: "pve1", Kind: "qemu"}, map[string]string{"net0": "virtio=BC:24:11:00:00:01,bridge=vmbr0"},
		map[string]map[string]string{"pre": {"net0": "virtio=BC:24:11:00:00:01,bridge=nb0001"}})
	require.NoError(t, e.svc.Reconcile(ctx, e.pc, e.loc))
	got := e.slots(t)["nb0001"]
	require.Equal(t, vnetslot.StateCooling, got.State)
	require.Equal(t, "still used by VM 777 (snapshot pre)", got.Reason)

	e.pv.AddForeignGuest(pve.GuestRef{VMID: 777, Node: "pve1", Kind: "qemu"}, map[string]string{"net0": "virtio=BC:24:11:00:00:01,bridge=vmbr0"}, nil)
	e.clk.advance(6 * time.Minute)
	require.NoError(t, e.svc.Reconcile(ctx, e.pc, e.loc))
	got = e.slots(t)["nb0001"]
	require.Equal(t, vnetslot.StateFree, got.State)
	require.Empty(t, got.Reason)
}

func TestCoolingScanIsThrottled(t *testing.T) {
	ctx := context.Background()
	e := coolingEnv(t)
	require.NoError(t, e.svc.Reconcile(ctx, e.pc, e.loc))
	require.Equal(t, vnetslot.StateFree, e.slots(t)["nb0001"].State)

	e.App.Ent.VNetSlot.Update().Where(vnetslot.Vnet("nb0001")).SetState(vnetslot.StateCooling).ExecX(ctx)
	e.clk.advance(4 * time.Minute)
	require.NoError(t, e.svc.Reconcile(ctx, e.pc, e.loc))
	require.Equal(t, vnetslot.StateCooling, e.slots(t)["nb0001"].State, "no second scan inside five minutes")
	e.clk.advance(2 * time.Minute)
	require.NoError(t, e.svc.Reconcile(ctx, e.pc, e.loc))
	require.Equal(t, vnetslot.StateFree, e.slots(t)["nb0001"].State)
}

func TestCoolingSeesConfigAndContainers(t *testing.T) {
	ctx := context.Background()
	e := coolingEnv(t)
	e.pv.AddContainer("pve1", 500)
	e.pv.AddForeignGuest(pve.GuestRef{VMID: 600, Node: "pve1", Kind: "lxc"}, map[string]string{"net0": "name=eth0,bridge=nb0001"}, nil)
	require.NoError(t, e.svc.Reconcile(ctx, e.pc, e.loc))
	require.Equal(t, "still used by VM 600 (config)", e.slots(t)["nb0001"].Reason)
}

func TestCoolingListingErrorKeepsCooling(t *testing.T) {
	ctx := context.Background()
	e := coolingEnv(t)
	e.pv.AddForeignGuest(pve.GuestRef{VMID: 777, Node: "pve1", Kind: "qemu"}, map[string]string{"net0": "virtio=BC:24:11:00:00:01,bridge=vmbr0"}, nil)
	e.pv.AddContainer("pve1", 888)
	e.pv.FailNext("GET /nodes/pve1/lxc/888/snapshot", 500)
	require.Error(t, e.svc.Reconcile(ctx, e.pc, e.loc))
	require.Equal(t, vnetslot.StateCooling, e.slots(t)["nb0001"].State)
}

func TestCoolingIgnoresOtherVNets(t *testing.T) {
	ctx := context.Background()
	e := coolingEnv(t)
	e.pv.AddForeignGuest(pve.GuestRef{VMID: 777, Node: "pve1", Kind: "qemu"}, map[string]string{"net0": "virtio=BC:24:11:00:00:01,bridge=nb0002"}, nil)
	require.NoError(t, e.svc.Reconcile(ctx, e.pc, e.loc))
	require.Equal(t, vnetslot.StateFree, e.slots(t)["nb0001"].State)
	require.Equal(t, vnetslot.StateFree, e.slots(t)["nb0002"].State)
}

func TestCoolingScanSkippedWithoutCoolingSlots(t *testing.T) {
	ctx := context.Background()
	e := newSDNEnv(t, "nb0001")
	require.NoError(t, e.svc.Discover(ctx, e.pc, e.loc))
	e.App.Ent.Location.UpdateOneID(e.loc).ClearSdnZone().ExecX(ctx)
	e.pv.SetDown(true)
	require.NoError(t, e.svc.Reconcile(ctx, e.pc, e.loc), "no cooling slots means no Proxmox calls")
}

func (e *sdnEnv) rawNet(t *testing.T, location int64, name, rng string) int64 {
	t.Helper()
	return e.App.Ent.Network.Create().SetProjectID(e.Project).SetLocationID(location).SetName(name).SetLabels(map[string]string{}).
		SetIPRange(rng).SaveX(context.Background()).ID
}

func (e *sdnEnv) attached(t *testing.T, name string, st server.Status, rowState servernetwork.State) (srv, net int64) {
	t.Helper()
	net = e.rawNet(t, e.loc, name, "10.7.0.0/24")
	srv = e.rawServer(t, e.Project, e.loc, "s-"+name, st)
	e.rawAttach(t, srv, net, "10.7.0.5", 1, rowState)
	return srv, net
}

func commands(t *testing.T, e *sdnEnv) []string {
	t.Helper()
	var out []string
	for _, a := range e.App.Ent.Action.Query().Where(action.CommandIn(networks.KindAttach, networks.KindDetach)).AllX(context.Background()) {
		out = append(out, a.Command)
	}
	return out
}

func TestResumeStuckDetach(t *testing.T) {
	ctx := context.Background()
	e := newSDNEnv(t, "nb0001", "nb0002", "nb0003")
	require.NoError(t, e.svc.Discover(ctx, e.pc, e.loc))
	srv, netID := e.attached(t, "one", server.StatusRunning, servernetwork.StateDetaching)
	require.NoError(t, e.svc.Reconcile(ctx, e.pc, e.loc))
	require.Equal(t, []string{networks.KindDetach}, commands(t, e))
	a := e.App.Ent.Action.Query().OnlyX(ctx)
	require.Equal(t, action.StatusRunning, a.Status)
	require.ElementsMatch(t, []actions.Resource{{ID: srv, Type: jobs.ServerResource}, {ID: netID, Type: networks.ResourceType}}, a.Resources)

	require.NoError(t, e.svc.Reconcile(ctx, e.pc, e.loc))
	require.Len(t, commands(t, e), 1, "a row with an unfinished action gets no second one")
}

func TestResumeStuckAttach(t *testing.T) {
	ctx := context.Background()
	e := newSDNEnv(t, "nb0001", "nb0002")
	require.NoError(t, e.svc.Discover(ctx, e.pc, e.loc))
	e.attached(t, "one", server.StatusRunning, servernetwork.StateAttaching)
	e.attached(t, "two", server.StatusRunning, servernetwork.StateAttached)
	require.NoError(t, e.svc.Reconcile(ctx, e.pc, e.loc))
	require.Equal(t, []string{networks.KindAttach}, commands(t, e))
}

func TestResumeSkipsBusyServers(t *testing.T) {
	ctx := context.Background()
	e := newSDNEnv(t, "nb0001", "nb0002", "nb0003")
	require.NoError(t, e.svc.Discover(ctx, e.pc, e.loc))
	e.attached(t, "building", server.StatusInitializing, servernetwork.StateAttaching)
	e.attached(t, "deleting", server.StatusDeleting, servernetwork.StateDetaching)
	busy, _ := e.attached(t, "busy", server.StatusRunning, servernetwork.StateDetaching)
	_, err := actions.Create(ctx, e.App.Ent, e.Project, "restart", []actions.Resource{{ID: busy, Type: jobs.ServerResource}})
	require.NoError(t, err)
	require.NoError(t, e.svc.Reconcile(ctx, e.pc, e.loc))
	require.Empty(t, commands(t, e))
}

func TestResumeOnlyForThisLocation(t *testing.T) {
	ctx := context.Background()
	e := newSDNEnv(t, "nb0001")
	other := e.App.Ent.Location.Create().SetName("other").SetPveURL("https://o.invalid").SetTokenID("t").SetTokenSecret([]byte("s")).
		SetDiskStorage("local").SetIsoStorage("local").SaveX(ctx)
	n := e.rawNet(t, other.ID, "far", "10.8.0.0/24")
	srv := e.rawServer(t, e.Project, other.ID, "far-s", server.StatusRunning)
	e.rawAttach(t, srv, n, "10.8.0.5", 1, servernetwork.StateAttaching)
	require.NoError(t, e.svc.Reconcile(ctx, e.pc, e.loc))
	require.Empty(t, commands(t, e))
}

func (e *sdnEnv) failedAction(t *testing.T, srv, net int64, kind, code string, finished time.Time) {
	t.Helper()
	a, err := actions.Create(context.Background(), e.App.Ent, e.Project, kind,
		[]actions.Resource{{ID: srv, Type: jobs.ServerResource}, {ID: net, Type: networks.ResourceType}})
	require.NoError(t, err)
	e.App.Ent.Action.UpdateOneID(a.ID).SetStatus(action.StatusError).SetErrorCode(code).SetFinished(finished).ExecX(context.Background())
}

func TestResumeBacksOffAfterFailure(t *testing.T) {
	ctx := context.Background()
	e := newSDNEnv(t, "nb0001")
	require.NoError(t, e.svc.Discover(ctx, e.pc, e.loc))
	srv, netID := e.attached(t, "one", server.StatusOff, servernetwork.StateDetaching)
	e.failedAction(t, srv, netID, networks.KindDetach, "failed", time.Now().Add(-time.Minute))
	for range 3 {
		require.NoError(t, e.svc.Reconcile(ctx, e.pc, e.loc))
	}
	require.Equal(t, []string{networks.KindDetach}, commands(t, e), "only the failed one: no new action inside the window")

	e.App.Ent.Action.Update().SetFinished(time.Now().Add(-11 * time.Minute)).ExecX(ctx)
	require.NoError(t, e.svc.Reconcile(ctx, e.pc, e.loc))
	require.Len(t, commands(t, e), 2, "after the window the row is tried again")
}

func TestResumeWaitsForPowerOffWhileRunning(t *testing.T) {
	ctx := context.Background()
	e := newSDNEnv(t, "nb0001")
	require.NoError(t, e.svc.Discover(ctx, e.pc, e.loc))
	srv, netID := e.attached(t, "one", server.StatusRunning, servernetwork.StateDetaching)
	e.failedAction(t, srv, netID, networks.KindDetach, actions.CodePowerOffRequired, time.Now().Add(-time.Hour))
	require.NoError(t, e.svc.Reconcile(ctx, e.pc, e.loc))
	require.Len(t, commands(t, e), 1, "still running: left alone")

	e.App.Ent.Server.UpdateOneID(srv).SetStatus(server.StatusOff).ExecX(ctx)
	require.NoError(t, e.svc.Reconcile(ctx, e.pc, e.loc))
	require.Len(t, commands(t, e), 2)
}

func TestResumeSkipsErroredServerWithoutVM(t *testing.T) {
	ctx := context.Background()
	e := newSDNEnv(t, "nb0001")
	require.NoError(t, e.svc.Discover(ctx, e.pc, e.loc))
	srv, _ := e.attached(t, "one", server.StatusError, servernetwork.StateAttaching)
	require.NoError(t, e.svc.Reconcile(ctx, e.pc, e.loc))
	require.Empty(t, commands(t, e))

	e.App.Ent.Server.UpdateOneID(srv).SetVmid(100).ExecX(ctx)
	require.NoError(t, e.svc.Reconcile(ctx, e.pc, e.loc))
	require.Len(t, commands(t, e), 1, "an errored server that has a VM is still resumed")
}

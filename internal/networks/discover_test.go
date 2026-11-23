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
	"net/http"
	"testing"

	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/vnetslot"
	"github.com/nebla-cloud/nebla/internal/networks"
	"github.com/nebla-cloud/nebla/internal/pve"
	"github.com/nebla-cloud/nebla/internal/pve/fake"
	"github.com/stretchr/testify/require"
)

// sdnEnv is a one-node fake cluster with zone "nebla" and a location row pointing at it.
type sdnEnv struct {
	*env
	pv  *fake.Server
	pc  pve.Client
	loc int64
}

func newSDNEnv(t *testing.T, vnets ...string) *sdnEnv {
	t.Helper()
	e := newEnv(t)
	pv := fake.New(t)
	pv.AddNode("pve1", 8, 32768)
	vs := make([]pve.SDNVNet, len(vnets))
	for i, v := range vnets {
		vs[i] = pve.SDNVNet{VNet: v, Zone: "nebla"}
	}
	pv.AddSDN(pve.SDNZone{Zone: "nebla", Type: "simple"}, vs...)
	loc := e.App.Ent.Location.Create().SetName("sdn").SetPveURL("https://sdn.invalid").SetTokenID("t").SetTokenSecret([]byte("s")).
		SetDiskStorage("local").SetIsoStorage("local").SetSdnZone("nebla").SetVnetPrefix("nb").SaveX(context.Background())
	return &sdnEnv{env: e, pv: pv, pc: pve.New(pv.Conn()), loc: loc.ID}
}

func (e *sdnEnv) slots(t *testing.T) map[string]*ent.VNetSlot {
	t.Helper()
	out := map[string]*ent.VNetSlot{}
	for _, s := range e.App.Ent.VNetSlot.Query().Where(vnetslot.LocationID(e.loc)).AllX(context.Background()) {
		out[s.Vnet] = s
	}
	return out
}

func TestDiscoverAddsSlots(t *testing.T) {
	ctx := context.Background()
	e := newSDNEnv(t, "nb0001", "nb0002", "other1")
	require.NoError(t, e.svc.Discover(ctx, e.pc, e.loc))

	got := e.slots(t)
	require.Len(t, got, 2)
	for _, v := range []string{"nb0001", "nb0002"} {
		require.Equal(t, vnetslot.StateFree, got[v].State, v)
		require.Equal(t, vnetslot.HealthOk, got[v].Health)
		require.Equal(t, 1500, got[v].Mtu)
		require.Equal(t, "nebla", got[v].Zone)
		require.Equal(t, "simple", got[v].ZoneType)
	}
	require.True(t, e.App.Ent.Location.GetX(ctx, e.loc).SdnAuditOk)

	require.NoError(t, e.svc.Discover(ctx, e.pc, e.loc))
	require.Len(t, e.slots(t), 2, "a second pass adds nothing")
}

func TestDiscoverVXLANMTU(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	pv := fake.New(t)
	pv.AddNode("pve1", 8, 32768)
	pv.AddNode("pve2", 8, 32768)
	pv.AddSDN(pve.SDNZone{Zone: "nebla", Type: "vxlan"}, pve.SDNVNet{VNet: "nb0001", Zone: "nebla"})
	loc := e.App.Ent.Location.Create().SetName("vx").SetPveURL("https://vx.invalid").SetTokenID("t").SetTokenSecret([]byte("s")).
		SetDiskStorage("local").SetIsoStorage("local").SetSdnZone("nebla").SaveX(ctx)
	require.NoError(t, e.svc.Discover(ctx, pve.New(pv.Conn()), loc.ID))
	s := e.App.Ent.VNetSlot.Query().OnlyX(ctx)
	require.Equal(t, vnetslot.StateFree, s.State)
	require.Equal(t, 1450, s.Mtu)
}

func TestDiscoverNoAudit(t *testing.T) {
	ctx := context.Background()
	e := newSDNEnv(t, "nb0001")
	require.NoError(t, e.App.Ent.Location.UpdateOneID(e.loc).SetSdnAuditOk(true).Exec(ctx))
	e.pv.DenySDNAudit(true)
	require.NoError(t, e.svc.Discover(ctx, e.pc, e.loc))
	require.Empty(t, e.slots(t))
	require.False(t, e.App.Ent.Location.GetX(ctx, e.loc).SdnAuditOk)
	st, err := e.svc.Pool(ctx, e.loc)
	require.NoError(t, err)
	require.Equal(t, "needs_setup", st.State)
	require.Zero(t, st.Available)
}

func TestDiscoverMarksUnusable(t *testing.T) {
	ctx := context.Background()
	e := newSDNEnv(t, "nb0001", "nb0002")
	e.pv.SetSDNSubnets("nb0002", "10.9.0.0/24")
	require.NoError(t, e.svc.Discover(ctx, e.pc, e.loc))
	got := e.slots(t)
	require.Equal(t, vnetslot.StateFree, got["nb0001"].State)
	require.Equal(t, vnetslot.StateUnusable, got["nb0002"].State)
	require.Equal(t, "has SDN subnets", got["nb0002"].Reason)

	e.pv.SetSDNSubnets("nb0002")
	require.NoError(t, e.svc.Discover(ctx, e.pc, e.loc))
	got = e.slots(t)
	require.Equal(t, vnetslot.StateFree, got["nb0002"].State, "an unusable slot recovers")
	require.Empty(t, got["nb0002"].Reason)
}

func TestDiscoverDegradesInUse(t *testing.T) {
	ctx := context.Background()
	e := newSDNEnv(t, "nb0001")
	require.NoError(t, e.svc.Discover(ctx, e.pc, e.loc))
	n := e.App.Ent.Network.Create().SetProjectID(e.Project).SetLocationID(e.loc).SetName("n").SetLabels(map[string]string{}).
		SetIPRange("10.1.0.0/24").SaveX(ctx)
	e.App.Ent.VNetSlot.Update().SetState(vnetslot.StateInUse).SetNetworkID(n.ID).ExecX(ctx)

	e.pv.SetSDNSubnets("nb0001", "10.1.0.0/24")
	require.NoError(t, e.svc.Discover(ctx, e.pc, e.loc))
	s := e.slots(t)["nb0001"]
	require.Equal(t, vnetslot.StateInUse, s.State)
	require.Equal(t, vnetslot.HealthDegraded, s.Health)
	require.Equal(t, "has SDN subnets", s.Reason)
	st, err := e.svc.Pool(ctx, e.loc)
	require.NoError(t, err)
	require.Equal(t, 1, st.InUse)
	require.Len(t, st.Degraded, 1)
	require.Equal(t, "nb0001", st.Degraded[0].VNet)

	e.pv.SetSDNSubnets("nb0001")
	require.NoError(t, e.svc.Discover(ctx, e.pc, e.loc))
	s = e.slots(t)["nb0001"]
	require.Equal(t, vnetslot.StateInUse, s.State)
	require.Equal(t, vnetslot.HealthOk, s.Health)
	require.Empty(t, s.Reason)
}

func TestDiscoverGone(t *testing.T) {
	ctx := context.Background()
	e := newSDNEnv(t, "nb0001", "nb0002", "nb0003")
	require.NoError(t, e.svc.Discover(ctx, e.pc, e.loc))
	n := e.App.Ent.Network.Create().SetProjectID(e.Project).SetLocationID(e.loc).SetName("n").SetLabels(map[string]string{}).
		SetIPRange("10.1.0.0/24").SaveX(ctx)
	e.App.Ent.VNetSlot.Update().Where(vnetslot.Vnet("nb0002")).SetState(vnetslot.StateInUse).SetNetworkID(n.ID).ExecX(ctx)
	e.App.Ent.VNetSlot.Update().Where(vnetslot.Vnet("nb0003")).SetState(vnetslot.StateCooling).ExecX(ctx)

	e.pv.RemoveSDNVNet("nb0001")
	e.pv.RemoveSDNVNet("nb0002")
	e.pv.RemoveSDNVNet("nb0003")
	require.NoError(t, e.svc.Discover(ctx, e.pc, e.loc))
	got := e.slots(t)
	require.Equal(t, vnetslot.StateGone, got["nb0001"].State)
	require.Equal(t, vnetslot.StateGone, got["nb0002"].State)
	require.Equal(t, n.ID, *got["nb0002"].NetworkID, "a gone slot keeps its network")
	require.Equal(t, vnetslot.StateCooling, got["nb0003"].State, "cooling is left to the reconciler")

	st, err := e.svc.Pool(ctx, e.loc)
	require.NoError(t, err)
	require.Equal(t, 1, st.InUse, "a gone VNet that still has a network counts as in use")
	require.Len(t, st.Degraded, 1)
	require.Equal(t, "nb0002", st.Degraded[0].VNet)
	require.Equal(t, "n", st.Degraded[0].Network)
	require.NotEmpty(t, st.Degraded[0].Reason)
	require.Len(t, st.Unusable, 1)
	require.Equal(t, "nb0001", st.Unusable[0].VNet)
}

func TestDiscoverFencedAgainstZoneChange(t *testing.T) {
	ctx := context.Background()
	for name, change := range map[string]func(e *sdnEnv){
		"zone cleared": func(e *sdnEnv) {
			_, err := e.svc.SetLocationSDN(ctx, e.loc, nil, "nb")
			require.NoError(t, err)
		},
		"prefix changed": func(e *sdnEnv) {
			require.NoError(t, e.App.Ent.Location.UpdateOneID(e.loc).SetVnetPrefix("zz").Exec(ctx))
		},
	} {
		t.Run(name, func(t *testing.T) {
			e := newSDNEnv(t, "nb0001", "nb0002")
			restore := networks.SetDiscoverHook(func(phase string) {
				if phase == "read" {
					change(e)
				}
			})
			defer restore()
			require.NoError(t, e.svc.Discover(ctx, e.pc, e.loc))
			require.Empty(t, e.slots(t), "a Discover that read the old zone writes nothing")
		})
	}
}

func TestCreateNotBlockedByDiscover(t *testing.T) {
	ctx := context.Background()
	e := newSDNEnv(t, "nb0001", "nb0002")
	require.NoError(t, e.App.Ent.Location.UpdateOneID(e.loc).SetSdnAuditOk(true).SetFirewallEnabled(true).Exec(ctx))
	require.NoError(t, e.svc.Discover(ctx, e.pc, e.loc))
	e.pv.RemoveSDNVNet("nb0002")

	var created error
	restore := networks.SetDiscoverHook(func(phase string) {
		if phase == "write" {
			_, created = e.svc.Create(ctx, e.Project, networks.CreateInput{Name: "n", Location: "sdn"})
		}
	})
	defer restore()
	require.NoError(t, e.svc.Discover(ctx, e.pc, e.loc))
	require.NoError(t, created, "a create doesn't wait on, or lose to, a discovery in flight")
	got := e.slots(t)
	require.Equal(t, vnetslot.StateInUse, got["nb0001"].State)
	require.Equal(t, vnetslot.StateGone, got["nb0002"].State)
}

func TestSetLocationSDNDiscovers(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.App.PVE.AddSDN(pve.SDNZone{Zone: "nebla", Type: "vxlan"}, pve.SDNVNet{VNet: "nb0001", Zone: "nebla"},
		pve.SDNVNet{VNet: "zz0001", Zone: "nebla"})
	_, err := e.svc.SetLocationSDN(ctx, e.Location, nil, "nb")
	require.NoError(t, err)

	zone := "nebla"
	st, err := e.svc.SetLocationSDN(ctx, e.Location, &zone, "nb")
	require.NoError(t, err)
	require.Equal(t, "firewall_off", st.State, "the firewall state isn't known yet")
	require.Equal(t, 1, st.Available)
	require.Equal(t, "nebla", *st.Zone)
	require.Equal(t, "nb", st.Prefix)

	e.setFirewall(t, new(true))
	st, err = e.svc.Pool(ctx, e.Location)
	require.NoError(t, err)
	require.Equal(t, "ready", st.State)

	// Changing the prefix replaces the slots.
	st, err = e.svc.SetLocationSDN(ctx, e.Location, &zone, "zz")
	require.NoError(t, err)
	require.Equal(t, 1, st.Available)
	require.Equal(t, "zz0001", e.App.Ent.VNetSlot.Query().OnlyX(ctx).Vnet)

	_, err = e.svc.SetLocationSDN(ctx, e.Location, &zone, "9bad")
	requireAPIErr(t, err, http.StatusUnprocessableEntity, "invalid_input", "")
	st, err = e.svc.SetLocationSDN(ctx, e.Location, nil, "nb")
	require.NoError(t, err)
	require.Equal(t, "not_set_up", st.State)
	require.Nil(t, st.Zone)
}

func TestSetLocationSDNRefusesChangeInUse(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.ready(t, 2)
	e.App.Ent.VNetSlot.Update().Where(vnetslot.Vnet("nb0001")).SetState(vnetslot.StateCooling).ExecX(ctx)
	zone := "nebla"
	other := "other"
	for _, c := range []struct {
		zone   *string
		prefix string
	}{{&other, "nb"}, {&zone, "xx"}, {nil, "nb"}} {
		_, err := e.svc.SetLocationSDN(ctx, e.Location, c.zone, c.prefix)
		requireAPIErr(t, err, http.StatusConflict, "conflict", "This location's private networks are in use; the zone and prefix can't change.")
	}
	e.App.Ent.VNetSlot.Update().SetState(vnetslot.StateInUse).ExecX(ctx)
	_, err := e.svc.SetLocationSDN(ctx, e.Location, &other, "nb")
	requireAPIErr(t, err, http.StatusConflict, "conflict", "")

	// Saving the same values is fine.
	_, err = e.svc.SetLocationSDN(ctx, e.Location, &zone, "nb")
	require.NoError(t, err)
	require.Equal(t, 2, e.App.Ent.VNetSlot.Query().CountX(ctx))
}

func TestSetLocationSDNZoneTakenOnSameCluster(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	loc2 := e.Admin.CreateLocation("eu-2")
	zone := "nebla"
	_, err := e.svc.SetLocationSDN(ctx, e.Location, &zone, "nb")
	require.NoError(t, err)

	_, err = e.svc.SetLocationSDN(ctx, loc2, &zone, "nb")
	requireAPIErr(t, err, http.StatusUnprocessableEntity, "invalid_input", "Another location already uses this zone.")
	require.Nil(t, e.App.Ent.Location.GetX(ctx, loc2).SdnZone)

	// A location on another cluster may reuse the name.
	far := e.App.Ent.Location.Create().SetName("far").SetPveURL("https://elsewhere.invalid").SetTokenID("t").SetTokenSecret([]byte("s")).
		SetDiskStorage("local").SetIsoStorage("local").SaveX(ctx)
	require.NoError(t, e.App.Ent.Location.UpdateOneID(far.ID).SetSdnZone("nebla").Exec(ctx))
}

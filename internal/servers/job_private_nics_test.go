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
	"bytes"
	"context"
	"github.com/nebla-cloud/nebla/internal/actions"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/kdomanski/iso9660"
	"github.com/nebla-cloud/nebla/internal/ent/network"
	"github.com/nebla-cloud/nebla/internal/ent/servernetwork"
	"github.com/nebla-cloud/nebla/internal/networks"
	"github.com/nebla-cloud/nebla/internal/projects"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/stretchr/testify/require"
)

// privEnv is a queue-only env whose location can host private networks nb0001 (vxlan, 1450) and nb0002 (simple, 1500).
type privEnv struct {
	*testutil.ServerEnv
	svc *servers.Service
	w   *servers.CreateServerWorker
}

func newPrivEnv(t *testing.T) *privEnv {
	t.Helper()
	servers.SetSettleWait(t, func(context.Context, time.Duration) error { return nil })
	e, w := directEnv(t)
	ctx := context.Background()
	require.NoError(t, e.App.Ent.Location.UpdateOneID(e.Location).SetSdnZone("nebla").SetSdnAuditOk(true).SetFirewallEnabled(true).Exec(ctx))
	for _, n := range []struct {
		name, vnet, zone, rng string
		mtu                   int
	}{{"alpha", "nb0001", "vxlan", "10.9.0.0/24", 1450}, {"beta", "nb0002", "simple", "172.16.0.0/16", 1500}} {
		row := e.App.Ent.Network.Create().SetProjectID(e.Project).SetLocationID(e.Location).SetName(n.name).SetLabels(map[string]string{}).
			SetIPRange(n.rng).SaveX(ctx)
		e.App.Ent.VNetSlot.Create().SetLocationID(e.Location).SetVnet(n.vnet).SetZone("z").SetZoneType(n.zone).SetMtu(n.mtu).
			SetState("in_use").SetNetworkID(row.ID).ExecX(ctx)
	}
	proj := projects.New(e.App.Ent, projects.Options{})
	nw := networks.New(networks.Options{SQL: e.App.SQL, Ent: e.App.Ent, Catalog: e.App.Catalog, Projects: proj, Jobs: e.App.Jobs})
	svc := servers.New(servers.Options{SQL: e.App.SQL, Ent: e.App.Ent, Catalog: e.App.Catalog, Projects: proj, Jobs: e.App.Jobs, Networks: nw})
	return &privEnv{ServerEnv: e, svc: svc, w: w}
}

// create makes a server on the named networks and returns it with its create action.
func (p *privEnv) create(t *testing.T, name string, refs ...string) testutil.CreatedServer {
	t.Helper()
	in := servers.CreateInput{Name: name, ServerType: "small", Image: "ubuntu", Location: "eu-1", IPv4: true, StartAfter: true}
	for _, r := range refs {
		in.Networks = append(in.Networks, networks.CreateRef{Network: r})
	}
	srv, act, err := p.svc.Create(context.Background(), p.Project, in)
	require.NoError(t, err)
	return testutil.CreatedServer{ServerID: srv.ID, ActionID: act.ID}
}

func isoFile(t *testing.T, raw []byte, name string) string {
	t.Helper()
	img, err := iso9660.OpenImage(bytes.NewReader(raw))
	require.NoError(t, err)
	root, err := img.RootDir()
	require.NoError(t, err)
	kids, err := root.GetChildren()
	require.NoError(t, err)
	for _, f := range kids {
		if f.Name() == name {
			b, err := io.ReadAll(f.Reader())
			require.NoError(t, err)
			return string(b)
		}
	}
	t.Fatalf("no %s in the ISO", name)
	return ""
}

func nicRows(t *testing.T, e *testutil.ServerEnv, id int64) []networks.NIC {
	t.Helper()
	nics, err := networks.PrivateNICs(context.Background(), e.App.Ent, id)
	require.NoError(t, err)
	return nics
}

func TestCreateWithNetworks(t *testing.T) {
	p := newPrivEnv(t)
	ctx := context.Background()
	s := p.create(t, "web", "alpha", "beta")
	nics := nicRows(t, p.ServerEnv, s.ServerID)
	require.Len(t, nics, 2)
	require.Equal(t, "attaching", nics[0].State)

	var cfg map[string]string
	var sets map[string][]string
	var iso []byte
	servers.SetAfterStep(p.w, func(n int) {
		if n != servers.StepFirewall {
			return
		}
		vm, ok := p.App.PVE.VM(vmidOf(t, p.ServerEnv, s.ServerID))
		require.True(t, ok)
		require.Equal(t, "stopped", vm.Status, "nothing started yet")
		cfg, sets = vm.Config, vm.IPSets
		iso, _ = p.App.PVE.Volume(isoVolid(s.ServerID))
	})
	require.NoError(t, p.w.Work(ctx, job(s, 1)))

	require.Equal(t, "virtio="+nics[0].MAC+",bridge=nb0001,firewall=1,mtu=1450", cfg["net1"])
	require.Equal(t, "virtio="+nics[1].MAC+",bridge=nb0002,firewall=1", cfg["net2"])
	require.Equal(t, []string{nics[0].IP.String()}, sets["ipfilter-net1"])
	require.Equal(t, []string{nics[1].IP.String()}, sets["ipfilter-net2"])
	nc := isoFile(t, iso, "network-config")
	require.Contains(t, nc, "eth1:")
	require.Contains(t, nc, "- "+nics[0].IP.String()+"/24")
	require.Contains(t, nc, "mtu: 1450")
	require.Contains(t, nc, "eth2:")
	require.Contains(t, nc, "- "+nics[1].IP.String()+"/16")

	for _, n := range nicRows(t, p.ServerEnv, s.ServerID) {
		require.Equal(t, "attached", n.State)
	}
	n, err := p.App.Ent.ServerNetwork.Query().Where(servernetwork.StateEQ(servernetwork.StateAttached)).Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, n)
}

func TestCreateNetworksDisabled(t *testing.T) {
	e := newEnv(t)
	svc := servers.New(servers.Options{SQL: e.App.SQL, Ent: e.App.Ent, Catalog: e.App.Catalog,
		Projects: projects.New(e.App.Ent, projects.Options{}), Jobs: e.App.Jobs})
	_, _, err := svc.Create(context.Background(), e.Project, servers.CreateInput{Name: "web", ServerType: "small", Image: "ubuntu",
		Location: "eu-1", Networks: []networks.CreateRef{{Network: "alpha"}}})
	require.ErrorContains(t, err, "Private networks aren't available in this location.")
}

func TestTemplateNet1Removed(t *testing.T) {
	e, w := directEnv(t)
	e.App.PVE.SetVMConfig(testutil.TemplateVMID, "net1", "virtio=BC:24:11:00:00:09,bridge=vmbr9")
	s := builtServer(t, e, w, "web")
	vm, _ := e.App.PVE.VM(vmidOf(t, e, s.ServerID))
	require.NotContains(t, vm.Config, "net1")
}

func TestConfigureMergesHotplug(t *testing.T) {
	e, w := directEnv(t)
	e.App.PVE.SetVMConfig(testutil.TemplateVMID, "hotplug", "disk,usb")
	s := builtServer(t, e, w, "web")
	vm, _ := e.App.PVE.VM(vmidOf(t, e, s.ServerID))
	require.Equal(t, "disk,usb,network", vm.Config["hotplug"])

	e2, w2 := directEnv(t)
	s2 := builtServer(t, e2, w2, "web")
	vm2, _ := e2.App.PVE.VM(vmidOf(t, e2, s2.ServerID))
	require.NotContains(t, vm2.Config, "hotplug")
}

func TestRebuildKeepsNICs(t *testing.T) {
	p := newPrivEnv(t)
	ctx := context.Background()
	p.App.PVE.AddTemplate("pve1", 9001, "ceph")
	p.Admin.CreateImage("debian", p.Location, 9001)
	s := p.create(t, "web", "alpha", "beta")
	require.NoError(t, p.w.Work(ctx, job(s, 1)))
	before := nicRows(t, p.ServerEnv, s.ServerID)
	old, _ := p.App.PVE.VM(vmidOf(t, p.ServerEnv, s.ServerID))

	id := startAction(t, p.ServerEnv, s.ServerID, "rebuild", map[string]any{"image": "debian"})
	rw := servers.NewRebuildServerWorker(opts(p.ServerEnv))
	var cfg map[string]string
	var sets map[string][]string
	var iso []byte
	servers.SetRebuildAfterStep(rw, func(n int) {
		if n != servers.StepFirewall {
			return
		}
		row, err := p.App.Ent.Server.Get(ctx, s.ServerID)
		require.NoError(t, err)
		vm, ok := p.App.PVE.VM(*row.RebuildVmid)
		require.True(t, ok)
		require.Equal(t, "stopped", vm.Status)
		cfg, sets = vm.Config, vm.IPSets
		iso, _ = p.App.PVE.Volume("local:iso/nb-srv-" + sid(s.ServerID) + "-2.iso")
	})
	require.NoError(t, rw.Work(ctx, jobOf(queuedArgs[servers.RebuildServerArgs](t, p.ServerEnv, id), 1)))

	for _, k := range []string{"net1", "net2"} {
		require.Equal(t, old.Config[k], cfg[k])
	}
	require.Equal(t, old.IPSets["ipfilter-net1"], sets["ipfilter-net1"])
	require.Equal(t, old.IPSets["ipfilter-net2"], sets["ipfilter-net2"])
	nc := isoFile(t, iso, "network-config")
	require.Contains(t, nc, strings.ToLower(before[0].MAC))
	require.Contains(t, nc, strings.ToLower(before[1].MAC))
	require.Equal(t, before, nicRows(t, p.ServerEnv, s.ServerID))
	vm, _ := p.App.PVE.VM(vmidOf(t, p.ServerEnv, s.ServerID))
	require.Equal(t, old.Config["net1"], vm.Config["net1"])
}

func TestServerDeleteFreesAttachments(t *testing.T) {
	p := newPrivEnv(t)
	ctx := context.Background()
	s := p.create(t, "web", "alpha")
	require.Len(t, nicRows(t, p.ServerEnv, s.ServerID), 1)
	require.NoError(t, p.App.Ent.Server.DeleteOneID(s.ServerID).Exec(ctx))
	n, err := p.App.Ent.ServerNetwork.Query().Count(ctx)
	require.NoError(t, err)
	require.Zero(t, n)
	s2 := p.create(t, "web2", "alpha")
	require.Equal(t, "10.9.0.2", nicRows(t, p.ServerEnv, s2.ServerID)[0].IP.String(), "the address is reusable")
}

func TestSyncNICsRewrites(t *testing.T) {
	p := newPrivEnv(t)
	ctx := context.Background()
	s := p.create(t, "web")
	require.NoError(t, p.w.Work(ctx, job(s, 1)))
	vmid := vmidOf(t, p.ServerEnv, s.ServerID)
	nw := p.App.Ent.Network.Query().Where(network.Name("alpha")).OnlyX(ctx)
	p.App.Ent.ServerNetwork.Create().SetServerID(s.ServerID).SetNetworkID(nw.ID).SetIP("10.9.0.2").SetNic(1).
		SetState(servernetwork.StateAttached).SetMAC("BC:24:11:AA:00:01").ExecX(ctx)
	want := "virtio=BC:24:11:AA:00:01,bridge=nb0001,firewall=1,mtu=1450"
	p.App.PVE.SetVMConfig(vmid, "net2", "virtio=BC:24:11:00:00:77,bridge=nb0099,firewall=1")

	o := opts(p.ServerEnv)
	pc, err := p.App.Catalog.ProxmoxFor(ctx, p.Location)
	require.NoError(t, err)
	require.NoError(t, o.SyncNICs(ctx, pc, "pve1", vmid, s.ServerID))
	vm, _ := p.App.PVE.VM(vmid)
	require.Equal(t, want, vm.Config["net1"])
	require.NotContains(t, vm.Config, "net2")
	require.Equal(t, []string{"10.9.0.2"}, vm.IPSets["ipfilter-net1"])

	// An error from Proxmox is returned.
	p.App.PVE.SetVMConfig(vmid, "net3", "virtio=BC:24:11:00:00:78,bridge=nb0098,firewall=1")
	p.App.PVE.FailNext("/config", http.StatusInternalServerError)
	require.Error(t, o.SyncNICs(ctx, pc, "pve1", vmid, s.ServerID))
	vm, _ = p.App.PVE.VM(vmid)
	require.Contains(t, vm.Config, "net3", "the failed write changed nothing")
}

func TestCreateFailsWhenSlotMissing(t *testing.T) {
	p := newPrivEnv(t)
	ctx := context.Background()
	s := p.create(t, "web", "alpha")
	p.App.Ent.VNetSlot.Delete().ExecX(ctx)
	_ = p.w.Work(ctx, job(s, 1))
	a := testutil.WaitAction(t, p.App.Ent, s.ActionID)
	require.Equal(t, actions.StatusError, a.Status)
	require.Contains(t, a.Error.Detail, "has no private network slot")
}

func TestFailedCreateDropsAttachingRows(t *testing.T) {
	p := newPrivEnv(t)
	ctx := context.Background()
	s := p.create(t, "web", "alpha")
	require.Len(t, nicRows(t, p.ServerEnv, s.ServerID), 1)
	p.App.PVE.FailNext("/resize", http.StatusInternalServerError)
	_ = p.w.Work(ctx, job(s, 1))
	require.Equal(t, servers.StatusError, string(p.App.Ent.Server.GetX(ctx, s.ServerID).Status))
	require.Empty(t, nicRows(t, p.ServerEnv, s.ServerID))
}

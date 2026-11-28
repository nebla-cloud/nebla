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

package paneltest

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/nebla-cloud/nebla/internal/ent/server"
	"github.com/nebla-cloud/nebla/internal/ent/vnetslot"
	"github.com/nebla-cloud/nebla/internal/httpapi"
	"github.com/nebla-cloud/nebla/internal/pve"
	"github.com/nebla-cloud/nebla/internal/ratelimit"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/nebla-cloud/nebla/sdk/nebla"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

const (
	pollEvery = 20 * time.Millisecond
	testKey   = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl test@example"
)

type panel struct {
	t        *testing.T
	c        *nebla.Client
	readOnly *nebla.Client
	env      *testutil.ServerEnv
}

func newPanel(t *testing.T) *panel {
	t.Helper()
	// Tests here must not run in parallel: FirewallSettle is a package variable.
	old := servers.FirewallSettle
	servers.FirewallSettle = 0
	t.Cleanup(func() { servers.FirewallSettle = old })
	// The default API limiter (3600/h, burst 100) would answer 429 to the polling.
	app := testutil.NewAppWithWorkers(t, testutil.AllWorkers,
		func(d *httpapi.Deps) { d.APILimiter = ratelimit.New(rate.Limit(10000), 100000) })
	env := testutil.NewServerEnv(app)
	env.Admin.SetQuota(env.Project, 50, 200, 262144, 4000, 50)
	rw := nebla.NewClient(nebla.WithEndpoint(app.URL), nebla.WithToken(env.C.CreateToken(env.Project, "read_write")))
	ro := nebla.NewClient(nebla.WithEndpoint(app.URL), nebla.WithToken(env.C.CreateToken(env.Project, "read")))
	return &panel{t: t, c: rw, readOnly: ro, env: env}
}

func (p *panel) wait(actions ...nebla.Action) {
	p.t.Helper()
	require.NoError(p.t, p.c.Actions.WaitFor(p.t.Context(), actions, nebla.PollInterval(pollEvery)))
}

// server creates a server and waits until it is running.
func (p *panel) server(name string) *nebla.Server {
	p.t.Helper()
	res, err := p.c.Servers.Create(p.t.Context(), nebla.ServerCreateOpts{Name: name, ServerType: "small", Image: "ubuntu", Location: "eu-1"})
	require.NoError(p.t, err)
	p.wait(append([]nebla.Action{res.Action}, res.NextActions...)...)
	s, err := p.c.Servers.Get(p.t.Context(), res.Server.ID)
	require.NoError(p.t, err)
	require.Equal(p.t, nebla.ServerStatusRunning, s.Status)
	return s
}

// bootDisk gives a server's VM the boot disk and NIC a snapshot job copies.
func (p *panel) bootDisk(s *nebla.Server) {
	p.t.Helper()
	ctx := context.Background()
	vmid := *p.env.App.Ent.Server.Query().Where(server.ID(s.ID)).OnlyX(ctx).Vmid
	pc, err := p.env.App.Catalog.ProxmoxFor(ctx, p.env.Location)
	require.NoError(p.t, err)
	boot, err := pc.AllocVolume(ctx, "pve1", "local-lvm", vmid, fmt.Sprintf("vm-%d-disk-0", vmid), 10)
	require.NoError(p.t, err)
	for k, v := range map[string]string{
		"scsi0":  boot + ",discard=on,size=10G",
		"net0":   "virtio=BC:24:11:00:00:01,bridge=vmbr0,firewall=1,tag=10",
		"boot":   "order=scsi0;net0",
		"scsihw": "virtio-scsi-single",
		"ostype": "l26",
		"agent":  "1",
	} {
		p.env.App.PVE.SetVMConfig(vmid, k, v)
	}
}

// privateNetworks sets up eu-1 with zone nebla and three free VNets.
func (p *panel) privateNetworks() {
	p.t.Helper()
	ctx := context.Background()
	p.env.App.PVE.AddSDN(pve.SDNZone{Zone: "nebla", Type: "vxlan"},
		pve.SDNVNet{VNet: "nb0001", Zone: "nebla"}, pve.SDNVNet{VNet: "nb0002", Zone: "nebla"}, pve.SDNVNet{VNet: "nb0003", Zone: "nebla"})
	require.NoError(p.t, p.env.App.Ent.Location.UpdateOneID(p.env.Location).SetSdnZone("nebla").SetSdnAuditOk(true).SetFirewallEnabled(true).Exec(ctx))
	for i := range 3 {
		_, err := p.env.App.Ent.VNetSlot.Create().SetLocationID(p.env.Location).SetVnet(fmt.Sprintf("nb%04d", i+1)).SetZone("nebla").
			SetZoneType("vxlan").SetMtu(1450).SetState(vnetslot.StateFree).Save(ctx)
		require.NoError(p.t, err)
	}
}

func TestPanel(t *testing.T) {
	p := newPanel(t)
	ctx := t.Context()

	t.Run("servers", func(t *testing.T) {
		p := &panel{t: t, c: p.c, env: p.env}
		s := p.server("srv-a")
		got, err := p.c.Servers.GetByName(ctx, "srv-a")
		require.NoError(t, err)
		assert.Equal(t, s.ID, got.ID)

		upd, err := p.c.Servers.Update(ctx, s.ID, nebla.ServerUpdateOpts{Labels: nebla.Ptr(map[string]string{"env": "test"})})
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"env": "test"}, upd.Labels)
		upd, err = p.c.Servers.Update(ctx, s.ID, nebla.ServerUpdateOpts{Labels: nebla.Ptr(map[string]string{})})
		require.NoError(t, err)
		assert.Empty(t, upd.Labels)

		a, err := p.c.Servers.PowerOff(ctx, s.ID)
		require.NoError(t, err)
		p.wait(*a)
		got, err = p.c.Servers.Get(ctx, s.ID)
		require.NoError(t, err)
		assert.Equal(t, nebla.ServerStatusOff, got.Status)
		a, err = p.c.Servers.PowerOn(ctx, s.ID)
		require.NoError(t, err)
		p.wait(*a)
		got, err = p.c.Servers.Get(ctx, s.ID)
		require.NoError(t, err)
		assert.Equal(t, nebla.ServerStatusRunning, got.Status)

		p.server("srv-b")
		p.server("srv-c")
		all, err := p.c.Servers.All(ctx, nebla.ServerListOpts{})
		require.NoError(t, err)
		assert.Len(t, all, 3)

		del, err := p.c.Servers.Delete(ctx, s.ID)
		require.NoError(t, err)
		p.wait(*del)
		_, err = p.c.Servers.Get(ctx, s.ID)
		assert.True(t, errors.Is(err, nebla.ErrNotFound), "got %v", err)
	})

	t.Run("ssh keys", func(t *testing.T) {
		k, err := p.c.SSHKeys.Create(ctx, nebla.SSHKeyCreateOpts{Name: "laptop", PublicKey: testKey})
		require.NoError(t, err)
		_, err = p.c.SSHKeys.Create(ctx, nebla.SSHKeyCreateOpts{Name: "laptop", PublicKey: testKey})
		assert.True(t, nebla.IsCode(err, nebla.CodeUniquenessError), "got %v", err)
		require.NoError(t, p.c.SSHKeys.Delete(ctx, k.ID))
	})

	t.Run("volumes", func(t *testing.T) {
		p := &panel{t: t, c: p.c, env: p.env}
		s := p.server("vol-srv")
		res, err := p.c.Volumes.Create(ctx, nebla.VolumeCreateOpts{Name: "data", Size: 10, Location: "eu-1", Server: s.Name})
		require.NoError(t, err)
		p.wait(res.Action)
		v, err := p.c.Volumes.Get(ctx, res.Volume.ID)
		require.NoError(t, err)
		assert.Equal(t, nebla.VolumeStatusAttached, v.Status)

		a, err := p.c.Volumes.Resize(ctx, v.ID, nebla.VolumeResizeOpts{Size: 20})
		require.NoError(t, err)
		p.wait(*a)
		a, err = p.c.Volumes.Detach(ctx, v.ID)
		require.NoError(t, err)
		p.wait(*a)
		v, err = p.c.Volumes.Get(ctx, v.ID)
		require.NoError(t, err)
		assert.Equal(t, nebla.VolumeStatusAvailable, v.Status)
		assert.Equal(t, 20, v.Size)

		a, err = p.c.Volumes.Delete(ctx, v.ID)
		require.NoError(t, err)
		p.wait(*a)
		_, err = p.c.Volumes.Get(ctx, v.ID)
		assert.True(t, errors.Is(err, nebla.ErrNotFound), "got %v", err)
	})

	t.Run("snapshots", func(t *testing.T) {
		p := &panel{t: t, c: p.c, env: p.env}
		s := p.server("snap-srv")
		p.bootDisk(s)
		res, err := p.c.Snapshots.Create(ctx, nebla.SnapshotCreateOpts{Server: s.Name, Name: "before-upgrade"})
		require.NoError(t, err)
		p.wait(res.Action)
		snap, err := p.c.Snapshots.GetByName(ctx, "before-upgrade")
		require.NoError(t, err)
		assert.Equal(t, nebla.SnapshotStatusAvailable, snap.Status)
		snap, err = p.c.Snapshots.Update(ctx, snap.ID, nebla.SnapshotUpdateOpts{Description: nebla.Ptr("pre upgrade")})
		require.NoError(t, err)
		assert.Equal(t, "pre upgrade", snap.Description)
		a, err := p.c.Snapshots.Delete(ctx, snap.ID)
		require.NoError(t, err)
		p.wait(*a)
		_, err = p.c.Snapshots.Get(ctx, snap.ID)
		assert.True(t, errors.Is(err, nebla.ErrNotFound), "got %v", err)
	})

	t.Run("networks", func(t *testing.T) {
		p := &panel{t: t, c: p.c, env: p.env}
		p.privateNetworks()
		s := p.server("net-srv")
		n, err := p.c.Networks.Create(ctx, nebla.NetworkCreateOpts{Name: "backend", Location: "eu-1"})
		require.NoError(t, err)
		a, cfg, err := p.c.Servers.AttachToNetwork(ctx, s.ID, nebla.ServerAttachNetworkOpts{Network: n.Name})
		require.NoError(t, err)
		assert.NotNil(t, cfg)
		p.wait(*a)
		a, _, err = p.c.Servers.DetachFromNetwork(ctx, s.ID, nebla.ServerDetachNetworkOpts{Network: n.Name})
		require.NoError(t, err)
		p.wait(*a)
		require.NoError(t, p.c.Networks.Delete(ctx, n.ID))
	})

	t.Run("firewalls", func(t *testing.T) {
		p := &panel{t: t, c: p.c, env: p.env}
		s := p.server("fw-srv")
		target := []nebla.FirewallResource{{Type: nebla.FirewallResourceTypeServer, Server: &nebla.FirewallResourceServer{ID: s.ID}}}
		res, err := p.c.Firewalls.Create(ctx, nebla.FirewallCreateOpts{Name: "web", Rules: []nebla.FirewallRule{{
			Direction: nebla.FirewallDirectionIn, Protocol: nebla.FirewallProtocolTCP, Port: "443", SourceIPs: []string{"0.0.0.0/0"}}}})
		require.NoError(t, err)
		acts, err := p.c.Firewalls.ApplyToResources(ctx, res.Firewall.ID, target)
		require.NoError(t, err)
		p.wait(acts...)
		acts, err = p.c.Firewalls.RemoveFromResources(ctx, res.Firewall.ID, target)
		require.NoError(t, err)
		p.wait(acts...)
		acts, err = p.c.Firewalls.Delete(ctx, res.Firewall.ID)
		require.NoError(t, err)
		p.wait(acts...)
	})

	t.Run("floating ips", func(t *testing.T) {
		p := &panel{t: t, c: p.c, env: p.env}
		s := p.server("fip-srv")
		res, err := p.c.FloatingIPs.Create(ctx, nebla.FloatingIPCreateOpts{Type: nebla.IPTypeIPv4, Location: "eu-1"})
		require.NoError(t, err)
		a, err := p.c.FloatingIPs.Assign(ctx, res.FloatingIP.ID, nebla.FloatingIPAssignOpts{Server: s.Name})
		require.NoError(t, err)
		p.wait(*a)
		a, err = p.c.FloatingIPs.Unassign(ctx, res.FloatingIP.ID)
		require.NoError(t, err)
		p.wait(*a)
		a, err = p.c.FloatingIPs.Delete(ctx, res.FloatingIP.ID)
		require.NoError(t, err)
		if a != nil {
			p.wait(*a)
		}
	})

	t.Run("primary ips", func(t *testing.T) {
		ip, err := p.c.PrimaryIPs.Create(ctx, nebla.PrimaryIPCreateOpts{Type: nebla.IPTypeIPv4, Location: "eu-1"})
		require.NoError(t, err)
		require.NoError(t, p.c.PrimaryIPs.Delete(ctx, ip.ID))
		_, err = p.c.PrimaryIPs.Get(ctx, ip.ID)
		assert.True(t, errors.Is(err, nebla.ErrNotFound), "got %v", err)
	})

	t.Run("read token", func(t *testing.T) {
		_, err := p.readOnly.Servers.Create(ctx, nebla.ServerCreateOpts{Name: "nope", ServerType: "small", Image: "ubuntu", Location: "eu-1"})
		var e *nebla.Error
		require.ErrorAs(t, err, &e)
		assert.Equal(t, http.StatusForbidden, e.StatusCode)
		assert.True(t, nebla.IsCode(err, nebla.CodeForbidden), "got %v", err)
	})
}

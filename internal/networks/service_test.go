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
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/apierr"
	"github.com/nebla-cloud/nebla/internal/catalog"
	"github.com/nebla-cloud/nebla/internal/db"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/action"
	"github.com/nebla-cloud/nebla/internal/ent/location"
	"github.com/nebla-cloud/nebla/internal/ent/server"
	"github.com/nebla-cloud/nebla/internal/ent/servernetwork"
	"github.com/nebla-cloud/nebla/internal/ent/vnetslot"
	"github.com/nebla-cloud/nebla/internal/ext"
	"github.com/nebla-cloud/nebla/internal/httpapi"
	"github.com/nebla-cloud/nebla/internal/networks"
	"github.com/nebla-cloud/nebla/internal/projects"
	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/stretchr/testify/require"
)

const (
	msgNotSetUp   = "Private networks aren't available in this location."
	msgPaused     = "Private networks are paused in this location."
	msgPoolEmpty  = "No private networks are available in this location right now."
	msgNICLimit   = "This server is already on 3 private networks."
	msgUnavail    = "This network can't take new servers right now."
	msgOtherLoc   = "This server is in a different location than the network."
	msgAlready    = "The server is already on this network."
	msgNotOn      = "The server isn't on this network."
	msgBadIP      = "That address isn't usable on this network."
	msgIPTaken    = "That address is already used on this network."
	msgDetachAll  = "Detach all servers from this network first"
	msgOverlap    = "That range overlaps another network or this location's public addresses."
	msgServerBusy = "the server has a running action"
)

type bus struct {
	mu     sync.Mutex
	events []ext.Event
}

func (b *bus) Publish(_ context.Context, e ext.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, e)
}

func (b *bus) all() []ext.Event {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]ext.Event(nil), b.events...)
}

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

type env struct {
	*testutil.ServerEnv
	clk   *clock
	svc   *networks.Service
	proj  *projects.Service
	hooks projects.Hooks
	bus   *bus
	n     int
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := testutil.NewServerEnv(testutil.NewApp(t, testutil.QueueOnlyJobs(t)))
	a := e.App
	hooks, _ := httpapi.ServerHooks(projects.Hooks{}, catalog.Hooks{})
	proj := projects.New(a.Ent, projects.Options{Hooks: hooks})
	b := &bus{}
	clk := &clock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	svc := networks.New(networks.Options{SQL: a.SQL, Ent: a.Ent, Catalog: a.Catalog, Projects: proj, Jobs: a.Jobs, Bus: b, Now: clk.Now})
	return &env{ServerEnv: e, clk: clk, svc: svc, proj: proj, hooks: hooks, bus: b}
}

// ready sets up eu-1 for private networks with n free slots nb0001...
func (e *env) ready(t *testing.T, n int) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, e.App.Ent.Location.UpdateOneID(e.Location).SetSdnZone("nebla").SetSdnAuditOk(true).SetFirewallEnabled(true).Exec(ctx))
	for i := range n {
		e.slot(t, e.Location, fmt.Sprintf("nb%04d", i+1), vnetslot.StateFree)
	}
}

func (e *env) slot(t *testing.T, location int64, vnet string, st vnetslot.State) *ent.VNetSlot {
	t.Helper()
	s, err := e.App.Ent.VNetSlot.Create().SetLocationID(location).SetVnet(vnet).SetZone("nebla").SetZoneType("simple").SetMtu(1500).
		SetState(st).Save(context.Background())
	require.NoError(t, err)
	return s
}

func (e *env) setFirewall(t *testing.T, v *bool) {
	t.Helper()
	u := e.App.Ent.Location.UpdateOneID(e.Location)
	if v == nil {
		u.ClearFirewallEnabled()
	} else {
		u.SetFirewallEnabled(*v)
	}
	require.NoError(t, u.Exec(context.Background()))
}

func requireAPIErr(t *testing.T, err error, status int, code, msg string) {
	t.Helper()
	var ae apierr.E
	require.True(t, errors.As(err, &ae), "%v", err)
	require.Equal(t, status, ae.Status, ae.Msg)
	require.Equal(t, code, ae.Code, ae.Msg)
	if msg != "" {
		require.Equal(t, msg, ae.Msg)
	}
}

func ref(id int64) string { return strconv.FormatInt(id, 10) }

func (e *env) rawServer(t *testing.T, project, location int64, name string, st server.Status) int64 {
	t.Helper()
	e.n++
	s, err := e.App.Ent.Server.Create().SetProjectID(project).SetName(name).SetLocationID(location).SetTemplateVmid(9000).
		SetServerTypeID(e.Type).SetImageID(e.Image).SetMAC(fmt.Sprintf("bc:24:11:00:ff:%02x", e.n)).SetLabels(map[string]string{}).
		SetCores(1).SetMemoryMB(512).SetDiskGB(10).SetSSHKeys([]string{}).SetStatus(st).Save(context.Background())
	require.NoError(t, err)
	return s.ID
}

// rawAttach inserts an attachment row with no job behind it.
func (e *env) rawAttach(t *testing.T, srv, net int64, ip string, nic int, st servernetwork.State) {
	t.Helper()
	e.n++
	require.NoError(t, e.App.Ent.ServerNetwork.Create().SetServerID(srv).SetNetworkID(net).SetIP(ip).SetNic(nic).SetState(st).
		SetMAC(fmt.Sprintf("BC:24:11:aa:00:%02X", e.n)).Exec(context.Background()))
}

func (e *env) createNet(t *testing.T, project int64, name, rng string) networks.Network {
	t.Helper()
	n, err := e.svc.Create(context.Background(), project, networks.CreateInput{Name: name, Location: "eu-1", IPRange: rng})
	require.NoError(t, err)
	return n
}

func (e *env) rows(t *testing.T, srv int64) []*ent.ServerNetwork {
	t.Helper()
	rows, err := e.App.Ent.ServerNetwork.Query().Where(servernetwork.ServerID(srv)).Order(ent.Asc(servernetwork.FieldNic)).All(context.Background())
	require.NoError(t, err)
	return rows
}

func TestCreateClaimsSlot(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.ready(t, 2)

	n, err := e.svc.Create(ctx, e.Project, networks.CreateInput{Name: "backend", Location: "eu-1", IPRange: "10.20.0.0/16",
		Labels: map[string]string{"env": "prod"}})
	require.NoError(t, err)
	require.Equal(t, netip.MustParsePrefix("10.20.0.0/16"), n.IPRange)
	require.Equal(t, "eu-1", n.Location.Name)
	require.Equal(t, map[string]string{"env": "prod"}, n.Labels)
	require.Empty(t, n.Servers)

	s := e.App.Ent.VNetSlot.Query().Where(vnetslot.NetworkID(n.ID)).OnlyX(ctx)
	require.Equal(t, "nb0001", s.Vnet)
	require.Equal(t, vnetslot.StateInUse, s.State)
	ev := e.bus.all()
	require.Len(t, ev, 1)
	require.Equal(t, ext.NetworkCreated, ev[0].Kind)
	require.Equal(t, networks.ResourceType, ev[0].ResourceType)
	require.Equal(t, n.ID, ev[0].ResourceID)
	require.Equal(t, e.Project, ev[0].ProjectID)

	got, err := e.svc.Get(ctx, e.Project, n.ID)
	require.NoError(t, err)
	require.Equal(t, n, got)
	_, err = e.svc.Get(ctx, e.Admin.CreateProject("q"), n.ID)
	requireAPIErr(t, err, http.StatusNotFound, "not_found", "")
}

func TestCreateDefaultRangeAvoidsPool(t *testing.T) {
	e := newEnv(t)
	e.ready(t, 2)
	a := e.createNet(t, e.Project, "a", "")
	require.Equal(t, netip.MustParsePrefix("10.1.0.0/16"), a.IPRange, "the location pool is 10.0.0.0/24")
	b := e.createNet(t, e.Project, "b", "")
	require.Equal(t, netip.MustParsePrefix("10.2.0.0/16"), b.IPRange, "and the project's other networks")
}

func TestCreateRefusals(t *testing.T) {
	ctx := context.Background()
	f := false
	cases := []struct {
		name   string
		setup  func(t *testing.T, e *env)
		in     networks.CreateInput
		status int
		code   string
		msg    string
	}{
		{"not set up", func(t *testing.T, e *env) {
			e.ready(t, 1)
			require.NoError(t, e.App.Ent.Location.UpdateOneID(e.Location).ClearSdnZone().Exec(ctx))
		}, networks.CreateInput{Name: "a", Location: "eu-1"}, 422, "invalid_input", msgNotSetUp},
		{"needs setup", func(t *testing.T, e *env) {
			e.ready(t, 1)
			require.NoError(t, e.App.Ent.Location.UpdateOneID(e.Location).SetSdnAuditOk(false).Exec(ctx))
		}, networks.CreateInput{Name: "a", Location: "eu-1"}, 422, "invalid_input", msgNotSetUp},
		{"firewall off", func(t *testing.T, e *env) { e.ready(t, 1); e.setFirewall(t, &f) },
			networks.CreateInput{Name: "a", Location: "eu-1"}, 422, "invalid_input", msgPaused},
		{"firewall unknown", func(t *testing.T, e *env) { e.ready(t, 1); e.setFirewall(t, nil) },
			networks.CreateInput{Name: "a", Location: "eu-1"}, 422, "invalid_input", msgPaused},
		{"pool empty", func(t *testing.T, e *env) { e.ready(t, 0) },
			networks.CreateInput{Name: "a", Location: "eu-1"}, 409, "no_private_networks_available", msgPoolEmpty},
		{"only unusable slots", func(t *testing.T, e *env) {
			e.ready(t, 0)
			e.slot(t, e.Location, "nb0001", vnetslot.StateUnusable)
			e.slot(t, e.Location, "nb0002", vnetslot.StateCooling)
		}, networks.CreateInput{Name: "a", Location: "eu-1"}, 409, "no_private_networks_available", msgPoolEmpty},
		{"overlaps pool", func(t *testing.T, e *env) { e.ready(t, 1) },
			networks.CreateInput{Name: "a", Location: "eu-1", IPRange: "10.0.0.0/16"}, 422, "invalid_input", msgOverlap},
		{"overlaps own network", func(t *testing.T, e *env) {
			e.ready(t, 2)
			e.createNet(t, e.Project, "first", "10.5.0.0/16")
		}, networks.CreateInput{Name: "a", Location: "eu-1", IPRange: "10.5.1.0/24"}, 422, "invalid_input", msgOverlap},
		{"public range", func(t *testing.T, e *env) { e.ready(t, 1) },
			networks.CreateInput{Name: "a", Location: "eu-1", IPRange: "8.8.8.0/24"}, 422, "invalid_input", ""},
		{"too small", func(t *testing.T, e *env) { e.ready(t, 1) },
			networks.CreateInput{Name: "a", Location: "eu-1", IPRange: "10.5.0.0/30"}, 422, "invalid_input", ""},
		{"bad name", func(t *testing.T, e *env) { e.ready(t, 1) },
			networks.CreateInput{Name: "Bad Name", Location: "eu-1"}, 422, "invalid_input", ""},
		{"no location", func(t *testing.T, e *env) { e.ready(t, 1) },
			networks.CreateInput{Name: "a"}, 422, "invalid_input", ""},
		{"unknown location", func(t *testing.T, e *env) { e.ready(t, 1) },
			networks.CreateInput{Name: "a", Location: "nowhere"}, 404, "not_found", ""},
		{"quota", func(t *testing.T, e *env) {
			e.ready(t, 2)
			require.NoError(t, e.App.Ent.Project.UpdateOneID(e.Project).SetMaxNetworks(1).Exec(ctx))
			e.createNet(t, e.Project, "first", "")
		}, networks.CreateInput{Name: "second", Location: "eu-1"}, 422, "quota_exceeded", ""},
		{"duplicate name", func(t *testing.T, e *env) {
			e.ready(t, 2)
			e.createNet(t, e.Project, "dup", "")
		}, networks.CreateInput{Name: "dup", Location: "eu-1"}, 409, "uniqueness_error", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newEnv(t)
			c.setup(t, e)
			free := e.App.Ent.VNetSlot.Query().Where(vnetslot.StateEQ(vnetslot.StateFree)).CountX(ctx)
			nets := e.App.Ent.Network.Query().CountX(ctx)
			_, err := e.svc.Create(ctx, e.Project, c.in)
			requireAPIErr(t, err, c.status, c.code, c.msg)
			require.Equal(t, free, e.App.Ent.VNetSlot.Query().Where(vnetslot.StateEQ(vnetslot.StateFree)).CountX(ctx), "no slot is claimed")
			require.Equal(t, nets, e.App.Ent.Network.Query().CountX(ctx), "no network is left behind")
		})
	}
}

func TestConcurrentCreatesGetDistinctSlots(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.ready(t, 2)
	projs := []int64{e.Project, e.Admin.CreateProject("p2"), e.Admin.CreateProject("p3"), e.Admin.CreateProject("p4")}

	errs := make([]error, 8)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = e.svc.Create(ctx, projs[i%4], networks.CreateInput{Name: fmt.Sprintf("n%d", i), Location: "eu-1",
				IPRange: fmt.Sprintf("10.%d.0.0/16", 10+i)})
		}()
	}
	wg.Wait()

	ok := 0
	for _, err := range errs {
		if err == nil {
			ok++
			continue
		}
		requireAPIErr(t, err, http.StatusConflict, "no_private_networks_available", msgPoolEmpty)
	}
	require.Equal(t, 2, ok)
	seen := map[int64]bool{}
	for _, s := range e.App.Ent.VNetSlot.Query().AllX(ctx) {
		require.Equal(t, vnetslot.StateInUse, s.State)
		require.NotNil(t, s.NetworkID)
		require.False(t, seen[*s.NetworkID])
		seen[*s.NetworkID] = true
	}
	require.Equal(t, 2, e.App.Ent.Network.Query().CountX(ctx))
}

func TestUpdateAndList(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.ready(t, 3)
	a := e.createNet(t, e.Project, "alpha", "")
	b, err := e.svc.Create(ctx, e.Project, networks.CreateInput{Name: "beta", Location: "eu-1", Labels: map[string]string{"env": "prod"}})
	require.NoError(t, err)

	name := "gamma"
	lbls := map[string]string{"team": "x"}
	got, err := e.svc.Update(ctx, e.Project, a.ID, networks.UpdateInput{Name: &name, Labels: &lbls})
	require.NoError(t, err)
	require.Equal(t, "gamma", got.Name)
	require.Equal(t, lbls, got.Labels)
	dup := "beta"
	_, err = e.svc.Update(ctx, e.Project, a.ID, networks.UpdateInput{Name: &dup})
	requireAPIErr(t, err, http.StatusConflict, "uniqueness_error", "")
	bad := "Bad Name"
	_, err = e.svc.Update(ctx, e.Project, a.ID, networks.UpdateInput{Name: &bad})
	requireAPIErr(t, err, http.StatusUnprocessableEntity, "invalid_input", "")
	_, err = e.svc.Update(ctx, e.Admin.CreateProject("q"), a.ID, networks.UpdateInput{Name: &name})
	requireAPIErr(t, err, http.StatusNotFound, "not_found", "")

	for _, c := range []struct {
		f    networks.ListFilter
		want []int64
	}{
		{networks.ListFilter{}, []int64{a.ID, b.ID}},
		{networks.ListFilter{Name: "beta"}, []int64{b.ID}},
		{networks.ListFilter{LabelSelector: "env=prod"}, []int64{b.ID}},
		{networks.ListFilter{LabelSelector: "!env"}, []int64{a.ID}},
		{networks.ListFilter{Location: "eu-1"}, []int64{a.ID, b.ID}},
		{networks.ListFilter{Location: "nowhere"}, []int64{}},
	} {
		list, total, err := e.svc.List(ctx, e.Project, c.f, 1, 10)
		require.NoError(t, err)
		ids := []int64{}
		for _, n := range list {
			ids = append(ids, n.ID)
		}
		require.Equal(t, c.want, ids, "%+v", c.f)
		require.Equal(t, len(c.want), total)
	}
}

func TestAttachRefusals(t *testing.T) {
	ctx := context.Background()
	type tc struct {
		name   string
		setup  func(t *testing.T, e *env) (project, srv int64, netRef, ip string)
		status int
		code   string
		msg    string
	}
	base := func(t *testing.T, e *env) (int64, int64, networks.Network) {
		e.ready(t, 4)
		n := e.createNet(t, e.Project, "net1", "10.1.0.0/24")
		return e.Project, e.rawServer(t, e.Project, e.Location, "web", server.StatusRunning), n
	}
	cases := []tc{
		{"other project's network", func(t *testing.T, e *env) (int64, int64, string, string) {
			e.ready(t, 1)
			other := e.Admin.CreateProject("other")
			n := e.createNet(t, other, "theirs", "")
			return e.Project, e.rawServer(t, e.Project, e.Location, "web", server.StatusRunning), ref(n.ID), ""
		}, 404, "not_found", ""},
		{"other project's server", func(t *testing.T, e *env) (int64, int64, string, string) {
			p, _, n := base(t, e)
			other := e.Admin.CreateProject("other")
			return p, e.rawServer(t, other, e.Location, "theirs", server.StatusRunning), ref(n.ID), ""
		}, 404, "not_found", "server not found"},
		{"other location", func(t *testing.T, e *env) (int64, int64, string, string) {
			p, _, n := base(t, e)
			loc2 := e.Admin.CreateLocation("eu-2")
			return p, e.rawServer(t, p, loc2, "far", server.StatusRunning), ref(n.ID), ""
		}, 422, "invalid_input", msgOtherLoc},
		{"already attached", func(t *testing.T, e *env) (int64, int64, string, string) {
			p, s, n := base(t, e)
			e.rawAttach(t, s, n.ID, "10.1.0.2", 1, servernetwork.StateAttached)
			return p, s, ref(n.ID), ""
		}, 422, "invalid_input", msgAlready},
		{"already attaching", func(t *testing.T, e *env) (int64, int64, string, string) {
			p, s, n := base(t, e)
			e.rawAttach(t, s, n.ID, "10.1.0.2", 1, servernetwork.StateAttaching)
			return p, s, n.Name, ""
		}, 422, "invalid_input", msgAlready},
		{"fourth NIC", func(t *testing.T, e *env) (int64, int64, string, string) {
			p, s, n := base(t, e)
			for i, r := range []string{"10.2.0.0/24", "10.3.0.0/24", "10.4.0.0/24"} {
				m := e.createNet(t, p, fmt.Sprintf("m%d", i), r)
				e.rawAttach(t, s, m.ID, fmt.Sprintf("10.%d.0.2", i+2), i+1, servernetwork.StateAttached)
			}
			return p, s, ref(n.ID), ""
		}, 422, "limit_exceeded", msgNICLimit},
		{"ip outside range", func(t *testing.T, e *env) (int64, int64, string, string) {
			p, s, n := base(t, e)
			return p, s, ref(n.ID), "10.9.9.9"
		}, 422, "invalid_input", msgBadIP},
		{"reserved ip", func(t *testing.T, e *env) (int64, int64, string, string) {
			p, s, n := base(t, e)
			return p, s, ref(n.ID), "10.1.0.1"
		}, 422, "invalid_input", msgBadIP},
		{"garbage ip", func(t *testing.T, e *env) (int64, int64, string, string) {
			p, s, n := base(t, e)
			return p, s, ref(n.ID), "nope"
		}, 422, "invalid_input", msgBadIP},
		{"ip taken", func(t *testing.T, e *env) (int64, int64, string, string) {
			p, s, n := base(t, e)
			e.rawAttach(t, e.rawServer(t, p, e.Location, "db", server.StatusRunning), n.ID, "10.1.0.5", 1, servernetwork.StateAttached)
			return p, s, ref(n.ID), "10.1.0.5"
		}, 409, "conflict", msgIPTaken},
		{"degraded slot", func(t *testing.T, e *env) (int64, int64, string, string) {
			p, s, n := base(t, e)
			e.App.Ent.VNetSlot.Update().Where(vnetslot.NetworkID(n.ID)).SetHealth(vnetslot.HealthDegraded).ExecX(ctx)
			return p, s, ref(n.ID), ""
		}, 422, "invalid_input", msgUnavail},
		{"gone slot", func(t *testing.T, e *env) (int64, int64, string, string) {
			p, s, n := base(t, e)
			e.App.Ent.VNetSlot.Update().Where(vnetslot.NetworkID(n.ID)).SetState(vnetslot.StateGone).ExecX(ctx)
			return p, s, ref(n.ID), ""
		}, 422, "invalid_input", msgUnavail},
		{"firewall off", func(t *testing.T, e *env) (int64, int64, string, string) {
			p, s, n := base(t, e)
			f := false
			e.setFirewall(t, &f)
			return p, s, ref(n.ID), ""
		}, 422, "invalid_input", msgPaused},
		{"server rebuilding", func(t *testing.T, e *env) (int64, int64, string, string) {
			p, _, n := base(t, e)
			return p, e.rawServer(t, p, e.Location, "busy", server.StatusRebuilding), ref(n.ID), ""
		}, 409, "locked", ""},
		{"server initializing", func(t *testing.T, e *env) (int64, int64, string, string) {
			p, _, n := base(t, e)
			return p, e.rawServer(t, p, e.Location, "new", server.StatusInitializing), ref(n.ID), ""
		}, 409, "locked", ""},
		{"server running an action", func(t *testing.T, e *env) (int64, int64, string, string) {
			p, s, n := base(t, e)
			_, err := actions.Create(ctx, e.App.Ent, p, "power_off", []actions.Resource{{ID: s, Type: "server"}})
			require.NoError(t, err)
			return p, s, ref(n.ID), ""
		}, 409, "locked", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newEnv(t)
			p, s, netRef, ip := c.setup(t, e)
			before := e.App.Ent.ServerNetwork.Query().CountX(ctx)
			acts := e.App.Ent.Action.Query().CountX(ctx)
			_, _, err := e.svc.Attach(ctx, p, s, netRef, ip)
			requireAPIErr(t, err, c.status, c.code, c.msg)
			require.Equal(t, before, e.App.Ent.ServerNetwork.Query().CountX(ctx))
			require.Equal(t, acts, e.App.Ent.Action.Query().CountX(ctx))
		})
	}
}

// settle finishes an action the queue-only runtime never works.
func (e *env) settle(t *testing.T, srv int64, act actions.Action) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, actions.Succeed(ctx, e.App.Ent, act.ID))
	e.App.Ent.ServerNetwork.Update().Where(servernetwork.ServerID(srv), servernetwork.StateEQ(servernetwork.StateAttaching)).
		SetState(servernetwork.StateAttached).ExecX(ctx)
}

func TestAttachPicksLowestIPAndNIC(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.ready(t, 3)
	n1 := e.createNet(t, e.Project, "n1", "10.1.0.0/24")
	n2 := e.createNet(t, e.Project, "n2", "10.2.0.0/24")
	n3 := e.createNet(t, e.Project, "n3", "10.3.0.0/24")
	s1 := e.rawServer(t, e.Project, e.Location, "s1", server.StatusRunning)
	s2 := e.rawServer(t, e.Project, e.Location, "s2", server.StatusOff)

	act, g1, err := e.svc.Attach(ctx, e.Project, s1, ref(n1.ID), "")
	require.NoError(t, err)
	require.NotNil(t, g1, "the commands come back from the starting transaction")
	require.Contains(t, g1.Apply, "10.1.0.2/24")
	require.Empty(t, g1.Note)
	require.Equal(t, networks.KindAttach, act.Command)
	require.ElementsMatch(t, []actions.Resource{{ID: s1, Type: "server"}, {ID: n1.ID, Type: networks.ResourceType}}, act.Resources)
	rows := e.rows(t, s1)
	require.Len(t, rows, 1)
	require.Equal(t, "10.1.0.2", rows[0].IP)
	require.Equal(t, 1, rows[0].Nic)
	require.Equal(t, servernetwork.StateAttaching, rows[0].State)
	require.Regexp(t, `^BC:24:11(:[0-9A-F]{2}){3}$`, rows[0].MAC)
	e.settle(t, s1, act)

	act, g2, err := e.svc.Attach(ctx, e.Project, s2, n1.Name, "")
	require.NoError(t, err)
	require.NotNil(t, g2)
	require.Equal(t, "Run these after starting the server.", g2.Note)
	require.Equal(t, "10.1.0.3", e.rows(t, s2)[0].IP)
	e.settle(t, s2, act)

	act, _, err = e.svc.Attach(ctx, e.Project, s1, ref(n2.ID), "10.2.0.50")
	require.NoError(t, err)
	e.settle(t, s1, act)
	rows = e.rows(t, s1)
	require.Len(t, rows, 2)
	require.Equal(t, "10.2.0.50", rows[1].IP)
	require.Equal(t, 2, rows[1].Nic)
	require.NotEqual(t, rows[0].MAC, rows[1].MAC)

	// Detaching nic 1 frees it for the next attach.
	act, _, err = e.svc.Detach(ctx, e.Project, s1, ref(n1.ID))
	require.NoError(t, err)
	require.Equal(t, networks.KindDetach, act.Command)
	require.Equal(t, servernetwork.StateDetaching, e.rows(t, s1)[0].State)
	require.NoError(t, actions.Succeed(ctx, e.App.Ent, act.ID))
	e.App.Ent.ServerNetwork.Delete().Where(servernetwork.ServerID(s1), servernetwork.NetworkID(n1.ID)).ExecX(ctx)

	_, _, err = e.svc.Attach(ctx, e.Project, s1, ref(n3.ID), "")
	require.NoError(t, err)
	got := e.rows(t, s1)
	require.Len(t, got, 2)
	require.Equal(t, 1, got[0].Nic)
	require.Equal(t, n3.ID, got[0].NetworkID)

	acts, total, err := e.svc.Actions(ctx, e.Project, n1.ID, actions.ListFilter{}, 1, 10)
	require.NoError(t, err)
	require.Equal(t, 3, total)
	require.Len(t, acts, 3)
	_, _, err = e.svc.Actions(ctx, e.Admin.CreateProject("q"), n1.ID, actions.ListFilter{}, 1, 10)
	requireAPIErr(t, err, http.StatusNotFound, "not_found", "")
	require.Equal(t, 5, e.App.Ent.Action.Query().Where(action.CommandIn(networks.KindAttach, networks.KindDetach)).CountX(ctx))
}

func TestDetachRefusals(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.ready(t, 1)
	n := e.createNet(t, e.Project, "n", "10.1.0.0/24")
	s := e.rawServer(t, e.Project, e.Location, "web", server.StatusRunning)

	_, _, err := e.svc.Detach(ctx, e.Project, s, ref(n.ID))
	requireAPIErr(t, err, http.StatusUnprocessableEntity, "invalid_input", msgNotOn)
	e.rawAttach(t, s, n.ID, "10.1.0.2", 1, servernetwork.StateAttaching)
	_, _, err = e.svc.Detach(ctx, e.Project, s, ref(n.ID))
	requireAPIErr(t, err, http.StatusConflict, "locked", "")
	_, _, err = e.svc.Detach(ctx, e.Admin.CreateProject("q"), s, ref(n.ID))
	requireAPIErr(t, err, http.StatusNotFound, "not_found", "")
}

func TestDetachWhileFirewallOff(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.ready(t, 1)
	n := e.createNet(t, e.Project, "n", "10.1.0.0/24")
	s := e.rawServer(t, e.Project, e.Location, "web", server.StatusRunning)
	e.rawAttach(t, s, n.ID, "10.1.0.2", 1, servernetwork.StateAttached)
	f := false
	e.setFirewall(t, &f)

	act, _, err := e.svc.Detach(ctx, e.Project, s, ref(n.ID))
	require.NoError(t, err)
	require.Equal(t, networks.KindDetach, act.Command)
	require.Equal(t, servernetwork.StateDetaching, e.rows(t, s)[0].State)
}

func TestDeleteRefusedWhileAttached(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.ready(t, 1)
	n := e.createNet(t, e.Project, "n", "10.1.0.0/24")
	s := e.rawServer(t, e.Project, e.Location, "web-1", server.StatusRunning)
	e.rawAttach(t, s, n.ID, "10.1.0.2", 1, servernetwork.StateAttached)

	err := e.svc.Delete(ctx, e.Project, n.ID)
	requireAPIErr(t, err, http.StatusConflict, "conflict", "")
	var ae apierr.E
	require.True(t, errors.As(err, &ae))
	require.Equal(t, msgDetachAll+": web-1.", ae.Msg)
	require.Equal(t, vnetslot.StateInUse, e.App.Ent.VNetSlot.Query().OnlyX(ctx).State)

	require.ErrorAs(t, e.svc.Delete(ctx, e.Admin.CreateProject("q"), n.ID), &ae)
	require.Equal(t, http.StatusNotFound, ae.Status)

	e.App.Ent.ServerNetwork.Delete().ExecX(ctx)
	require.NoError(t, e.svc.Delete(ctx, e.Project, n.ID))
	sl := e.App.Ent.VNetSlot.Query().OnlyX(ctx)
	require.Equal(t, vnetslot.StateCooling, sl.State)
	require.Nil(t, sl.NetworkID)
	require.Zero(t, e.App.Ent.Network.Query().CountX(ctx))
	ev := e.bus.all()
	require.Len(t, ev, 2)
	require.Equal(t, ext.NetworkDeleted, ev[1].Kind)
	require.Equal(t, n.ID, ev[1].ResourceID)
	require.Equal(t, networks.ResourceType, ev[1].ResourceType)
}

func (e *env) reserve(t *testing.T, project, location, srv int64, refs []networks.CreateRef) error {
	t.Helper()
	return db.WithTx(context.Background(), e.App.SQL, func(_ *sql.Tx, etx *ent.Client) error {
		return e.svc.ReserveForCreate(context.Background(), etx, project, location, srv, refs)
	})
}

func TestReserveForCreate(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.ready(t, 4)
	n1 := e.createNet(t, e.Project, "n1", "10.1.0.0/24")
	n2 := e.createNet(t, e.Project, "n2", "10.2.0.0/24")
	s := e.rawServer(t, e.Project, e.Location, "web", server.StatusInitializing)

	reserve := func(srv int64, refs ...networks.CreateRef) error {
		return e.reserve(t, e.Project, e.Location, srv, refs)
	}
	require.NoError(t, reserve(s, networks.CreateRef{Network: n2.Name}, networks.CreateRef{Network: ref(n1.ID), IP: "10.1.0.20"}))
	rows := e.rows(t, s)
	require.Len(t, rows, 2)
	require.Equal(t, []int{1, 2}, []int{rows[0].Nic, rows[1].Nic})
	require.Equal(t, n2.ID, rows[0].NetworkID, "nic numbers follow the order given")
	require.Equal(t, "10.2.0.2", rows[0].IP)
	require.Equal(t, "10.1.0.20", rows[1].IP)
	for _, r := range rows {
		require.Equal(t, servernetwork.StateAttaching, r.State)
		require.Regexp(t, `^BC:24:11(:[0-9A-F]{2}){3}$`, r.MAC)
	}
	require.Equal(t, 0, e.App.Ent.Action.Query().CountX(ctx), "the create flow owns the action")

	other := e.rawServer(t, e.Project, e.Location, "other", server.StatusInitializing)
	for name, c := range map[string]struct {
		refs []networks.CreateRef
		code string
		msg  string
	}{
		"duplicate": {[]networks.CreateRef{{Network: n1.Name}, {Network: ref(n1.ID)}}, "invalid_input", msgAlready},
		"bad ip":    {[]networks.CreateRef{{Network: n1.Name, IP: "10.1.0.1"}}, "invalid_input", msgBadIP},
		"ip taken":  {[]networks.CreateRef{{Network: n1.Name, IP: "10.1.0.20"}}, "conflict", msgIPTaken},
		"too many":  {[]networks.CreateRef{{Network: "n1"}, {Network: "n2"}, {Network: "n1"}, {Network: "n2"}}, "limit_exceeded", msgNICLimit},
		"unknown":   {[]networks.CreateRef{{Network: "nope"}}, "not_found", ""},
	} {
		err := reserve(other, c.refs...)
		var ae apierr.E
		require.ErrorAs(t, err, &ae, name)
		require.Equal(t, c.code, ae.Code, name)
		if c.msg != "" {
			require.Equal(t, c.msg, ae.Msg, name)
		}
	}
	require.Empty(t, e.rows(t, other))

	loc2 := e.Admin.CreateLocation("eu-2")
	requireAPIErr(t, e.reserve(t, e.Project, loc2, other, []networks.CreateRef{{Network: n1.Name}}),
		http.StatusUnprocessableEntity, "invalid_input", msgOtherLoc)
	f := false
	e.setFirewall(t, &f)
	requireAPIErr(t, reserve(other, networks.CreateRef{Network: n1.Name}), http.StatusUnprocessableEntity, "invalid_input", msgPaused)
	e.setFirewall(t, new(true))
	e.App.Ent.VNetSlot.Update().Where(vnetslot.NetworkID(n1.ID)).SetHealth(vnetslot.HealthDegraded).ExecX(ctx)
	requireAPIErr(t, reserve(other, networks.CreateRef{Network: n1.Name}), http.StatusUnprocessableEntity, "invalid_input", msgUnavail)

	pn, err := e.svc.ForServers(ctx, e.App.Ent, []int64{s, other})
	require.NoError(t, err)
	require.Equal(t, []networks.PrivateNet{
		{NetworkID: n2.ID, Network: "n2", IP: netip.MustParseAddr("10.2.0.2")},
		{NetworkID: n1.ID, Network: "n1", IP: netip.MustParseAddr("10.1.0.20")},
	}, pn[s])
	require.Empty(t, pn[other])
}

func TestAvailable(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	ok, err := e.svc.Available(ctx, e.Location)
	require.NoError(t, err)
	require.False(t, ok, "not set up")
	e.ready(t, 1)
	ok, err = e.svc.Available(ctx, e.Location)
	require.NoError(t, err)
	require.True(t, ok)
	e.createNet(t, e.Project, "n", "10.1.0.0/24")
	ok, err = e.svc.Available(ctx, e.Location)
	require.NoError(t, err)
	require.True(t, ok, "a full pool still lets servers join existing networks")
	f := false
	e.setFirewall(t, &f)
	ok, err = e.svc.Available(ctx, e.Location)
	require.NoError(t, err)
	require.False(t, ok, "firewall off")
}

func TestUsageCountsNetworks(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.ready(t, 2)
	e.createNet(t, e.Project, "a", "")
	e.createNet(t, e.Project, "b", "")
	u, err := e.hooks.Usage(ctx, e.App.Ent, e.Project)
	require.NoError(t, err)
	require.Equal(t, 2, u.Networks)
	u, err = e.hooks.Usage(ctx, e.App.Ent, e.Admin.CreateProject("q"))
	require.NoError(t, err)
	require.Zero(t, u.Networks)
}

func TestProjectNotEmpty(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.ready(t, 1)
	p := e.Admin.CreateProject("solo")
	_, err := e.svc.Create(ctx, p, networks.CreateInput{Name: "n", Location: "eu-1"})
	require.NoError(t, err)
	err = e.proj.AdminDelete(ctx, p)
	requireAPIErr(t, err, http.StatusConflict, "project_not_empty", "Delete the project's servers, volumes, floating IPs and networks first.")
}

func (e *env) bareLocation(t *testing.T, name string) int64 {
	t.Helper()
	return e.App.Ent.Location.Create().SetName(name).SetPveURL("https://" + name + ".invalid").SetTokenID("t").SetTokenSecret([]byte("s")).
		SetDiskStorage("local").SetIsoStorage("local").SaveX(context.Background()).ID
}

func TestDeleteLocationWithNetworkNamesReason(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	loc := e.bareLocation(t, "doomed")
	e.slot(t, loc, "nb0001", vnetslot.StateInUse)
	n := e.App.Ent.Network.Create().SetProjectID(e.Project).SetLocationID(loc).SetName("n").SetLabels(map[string]string{}).
		SetIPRange("10.1.0.0/24").SaveX(ctx)
	e.App.Ent.VNetSlot.Update().Where(vnetslot.LocationID(loc)).SetNetworkID(n.ID).ExecX(ctx)

	err := e.App.Catalog.DeleteLocation(ctx, loc)
	requireAPIErr(t, err, http.StatusConflict, "location_in_use", "Delete this location's private networks first.")
}

func TestDeleteLocationWithCoolingSlotNamesReason(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	loc := e.bareLocation(t, "doomed")
	s := e.slot(t, loc, "nb0001", vnetslot.StateCooling)
	e.App.Ent.VNetSlot.UpdateOne(s).SetReason("still used by VM 7 (snapshot)").ExecX(ctx)

	err := e.App.Catalog.DeleteLocation(ctx, loc)
	requireAPIErr(t, err, http.StatusConflict, "location_in_use",
		"Private networks in this location are still being released (nb0001: still used by VM 7 (snapshot)).")
	require.Equal(t, 1, e.App.Ent.VNetSlot.Query().Where(vnetslot.LocationID(loc)).CountX(ctx))
}

func TestDeleteLocationDropsIdleSlots(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	loc := e.bareLocation(t, "doomed")
	e.slot(t, loc, "nb0001", vnetslot.StateFree)
	e.slot(t, loc, "nb0002", vnetslot.StateUnusable)
	e.slot(t, loc, "nb0003", vnetslot.StateGone)

	require.NoError(t, e.App.Catalog.DeleteLocation(ctx, loc))
	require.Zero(t, e.App.Ent.VNetSlot.Query().Where(vnetslot.LocationID(loc)).CountX(ctx))
	require.False(t, e.App.Ent.Location.Query().Where(location.ID(loc)).ExistX(ctx))
}

func TestAttachRefusedWhenLocationNotReady(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.ready(t, 1)
	n := e.createNet(t, e.Project, "n", "10.1.0.0/24")
	srv := e.rawServer(t, e.Project, e.Location, "s", server.StatusOff)
	e.App.Ent.Location.UpdateOneID(e.Location).ClearSdnZone().ExecX(ctx)
	_, _, err := e.svc.Attach(ctx, e.Project, srv, ref(n.ID), "")
	requireAPIErr(t, err, http.StatusUnprocessableEntity, "invalid_input", msgNotSetUp)
}

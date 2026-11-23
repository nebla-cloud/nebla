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

package floatingips_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/apierr"
	"github.com/nebla-cloud/nebla/internal/catalog"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/action"
	"github.com/nebla-cloud/nebla/internal/ent/ipallocation"
	"github.com/nebla-cloud/nebla/internal/ent/server"
	"github.com/nebla-cloud/nebla/internal/ext"
	"github.com/nebla-cloud/nebla/internal/floatingips"
	"github.com/nebla-cloud/nebla/internal/httpapi"
	"github.com/nebla-cloud/nebla/internal/projects"
	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/stretchr/testify/require"
)

const (
	msgSegment   = "This floating IP can't be used with that server; it's in a different network."
	msgLimit     = "A server can have at most 10 floating IPs."
	msgReleasing = "The address is still being released from the previous server."
	msgBusy      = "the floating IP has a running action"
	msgDeleting  = "server is being deleted"
	msgSame      = "The floating IP is already assigned to that server."
	msgNotAssign = "The floating IP isn't assigned."
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

type env struct {
	*testutil.ServerEnv
	svc  *floatingips.Service
	proj *projects.Service
	bus  *bus
	n    int
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := testutil.NewServerEnv(testutil.NewApp(t, testutil.QueueOnlyJobs(t)))
	a := e.App
	hooks, _ := httpapi.ServerHooks(projects.Hooks{}, catalog.Hooks{})
	proj := projects.New(a.Ent, projects.Options{Hooks: hooks})
	b := &bus{}
	svc := floatingips.New(floatingips.Options{SQL: a.SQL, Ent: a.Ent, Catalog: a.Catalog, Projects: proj, Jobs: a.Jobs, Bus: b})
	return &env{ServerEnv: e, svc: svc, proj: proj, bus: b}
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

// rawServer inserts a server holding ip from poolID, with no job behind it.
func (e *env) rawServer(t *testing.T, project, location, poolID int64, name, ip string, st server.Status) int64 {
	t.Helper()
	ctx := context.Background()
	e.n++
	s, err := e.App.Ent.Server.Create().SetProjectID(project).SetName(name).SetLocationID(location).SetTemplateVmid(9000).
		SetServerTypeID(e.Type).SetImageID(e.Image).SetMAC(fmt.Sprintf("bc:24:11:00:ff:%02x", e.n)).SetLabels(map[string]string{}).
		SetCores(1).SetMemoryMB(512).SetDiskGB(10).SetSSHKeys([]string{}).SetStatus(st).Save(ctx)
	require.NoError(t, err)
	if ip != "" {
		require.NoError(t, e.App.Ent.IPAllocation.Create().SetPoolID(poolID).SetIP(ip).SetServerID(s.ID).
			SetOwner(ipallocation.OwnerServer).Exec(ctx))
	}
	return s.ID
}

// rawFIP inserts a floating IP of the project from poolID; srv and releasing may be nil.
func (e *env) rawFIP(t *testing.T, project, poolID int64, ip string, srv, releasing *int64) int64 {
	t.Helper()
	ctx := context.Background()
	pool := e.App.Ent.IPPool.GetX(ctx, poolID)
	a := e.App.Ent.IPAllocation.Create().SetPoolID(poolID).SetIP(ip).SetOwner(ipallocation.OwnerFloating).SaveX(ctx)
	row, err := e.App.Ent.IPAddress.Create().SetProjectID(project).SetLocationID(pool.LocationID).SetAllocationID(a.ID).
		SetFamily(pool.Family).SetIP(ip).SetName("raw-" + strings.NewReplacer(".", "-", ":", "-").Replace(ip)).
		SetLabels(map[string]string{}).SetNillableServerID(srv).SetNillableReleasingFromID(releasing).Save(ctx)
	require.NoError(t, err)
	return row.ID
}

func (e *env) row(t *testing.T, id int64) *ent.IPAddress {
	t.Helper()
	r, err := e.App.Ent.IPAddress.Get(context.Background(), id)
	require.NoError(t, err)
	return r
}

func TestCreateInLocation(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.RunningServer("web")

	f, act, err := e.svc.Create(ctx, e.Project, floatingips.CreateInput{Family: "ipv4", Location: "eu-1",
		Description: "front door", Labels: map[string]string{"env": "prod"}})
	require.NoError(t, err)
	require.Nil(t, act)
	require.Equal(t, "10.0.0.11", f.IP)
	require.Equal(t, "ipv4", f.Family)
	require.Equal(t, "fip-"+ref(f.ID), f.Name)
	require.Equal(t, "front door", f.Description)
	require.Equal(t, map[string]string{"env": "prod"}, f.Labels)
	require.Equal(t, "active", f.Status)
	require.Equal(t, "eu-1", f.Location.Name)
	require.Equal(t, e.Location, f.Location.ID)
	require.Nil(t, f.ServerID)
	require.Nil(t, f.Guest)

	evs := e.bus.all()
	require.Len(t, evs, 1)
	require.Equal(t, ext.FloatingIPCreated, evs[0].Kind)
	require.Equal(t, "floating_ip", evs[0].ResourceType)
	require.Equal(t, f.ID, evs[0].ResourceID)
	require.Equal(t, e.Project, evs[0].ProjectID)

	named, _, err := e.svc.Create(ctx, e.Project, floatingips.CreateInput{Name: "edge", Family: "ipv6", Location: ref(e.Location)})
	require.NoError(t, err)
	require.Equal(t, "edge", named.Name)
	require.Equal(t, "2001:db8::10", named.IP)

	_, _, err = e.svc.Create(ctx, e.Project, floatingips.CreateInput{Name: "edge", Family: "ipv4", Location: "eu-1"})
	requireAPIErr(t, err, http.StatusConflict, "uniqueness_error", "")
	for _, in := range []floatingips.CreateInput{
		{Name: "Bad", Family: "ipv4", Location: "eu-1"},
		{Family: "ipv5", Location: "eu-1"},
		{Family: "ipv4"},
		{Family: "ipv4", Location: "eu-1", Server: "web"},
		{Family: "ipv4", Location: "eu-1", Description: strings.Repeat("x", 256)},
		{Family: "ipv4", Location: "eu-1", Labels: map[string]string{"bad key": "v"}},
	} {
		_, _, err = e.svc.Create(ctx, e.Project, in)
		requireAPIErr(t, err, http.StatusUnprocessableEntity, "invalid_input", "")
	}
}

func TestCreateOnServer(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	web := e.RunningServer("web")

	f, act, err := e.svc.Create(ctx, e.Project, floatingips.CreateInput{Family: "ipv4", Server: "web"})
	require.NoError(t, err)
	require.NotNil(t, act)
	require.Equal(t, floatingips.KindAssign, act.Command)
	require.Equal(t, []actions.Resource{{ID: f.ID, Type: "floating_ip"}, {ID: web.ServerID, Type: "server"}}, act.Resources)
	require.Equal(t, "10.0.0.11", f.IP)
	require.Equal(t, &web.ServerID, f.ServerID)
	require.NotNil(t, f.Guest)
	require.Equal(t, "web", f.Guest.Server)
	require.Contains(t, f.Guest.Apply, "sudo ip addr add 10.0.0.11/32 dev eth0")
	require.NotEmpty(t, f.Guest.Persist)
	require.Empty(t, f.Guest.PreviousServer)

	srv := e.App.Ent.Server.GetX(ctx, web.ServerID)
	r := e.row(t, f.ID)
	require.NotNil(t, r.PendingGen)
	require.Equal(t, srv.FirewallGenDesired, *r.PendingGen)
	require.Nil(t, r.PreviousServerID)
	require.Len(t, e.bus.all(), 1)

	// An IPv6 floating IP needs an IPv6 primary on the server.
	_, _, err = e.svc.Create(ctx, e.Project, floatingips.CreateInput{Family: "ipv6", Server: "web"})
	requireAPIErr(t, err, http.StatusUnprocessableEntity, "invalid_input", msgSegment)
}

func TestQuotaCountsFloating(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.Admin.SetQuota(e.Project, 10, 100, 100_000, 1000, 2)
	e.RunningServer("web")

	_, _, err := e.svc.Create(ctx, e.Project, floatingips.CreateInput{Family: "ipv4", Location: "eu-1"})
	require.NoError(t, err)
	_, _, err = e.svc.Create(ctx, e.Project, floatingips.CreateInput{Family: "ipv4", Location: "eu-1"})
	requireAPIErr(t, err, http.StatusUnprocessableEntity, "quota_exceeded", "")
	n, err := e.App.Ent.IPAllocation.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, n)
}

func TestAssignRefusals(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	web := e.RunningServer("web").ServerID
	other := e.Admin.CreateProject("other")
	otherFIP := e.rawFIP(t, other, e.Pool4, "10.0.0.30", nil, nil)
	otherServer := e.rawServer(t, other, e.Location, e.Pool4, "theirs", "10.0.0.31", server.StatusRunning)

	loc2 := e.App.Ent.Location.Create().SetName("eu-2").SetPveURL("https://pve2.invalid").SetTokenID("t").SetTokenSecret([]byte("s")).
		SetDiskStorage("local-lvm").SetIsoStorage("local").SaveX(ctx)
	pool2 := e.App.Ent.IPPool.Create().SetLocationID(loc2.ID).SetFamily("ipv4").SetBridge("vmbr0").SetCidr("10.0.0.0/24").
		SetGateway("10.0.0.1").SetDNS([]string{"10.0.0.1"}).SetRangeStart("10.0.0.10").SetRangeEnd("10.0.0.20").SaveX(ctx)
	e.rawServer(t, e.Project, loc2.ID, pool2.ID, "far", "10.0.0.10", server.StatusRunning)
	v6 := e.rawServer(t, e.Project, e.Location, e.Pool6, "v6only", "2001:db8::40", server.StatusRunning)
	full := e.rawServer(t, e.Project, e.Location, e.Pool4, "full", "10.0.0.41", server.StatusRunning)
	for i := range floatingips.MaxPerServer {
		e.rawFIP(t, e.Project, e.Pool4, fmt.Sprintf("10.0.0.%d", 100+i), &full, nil)
	}
	gone := e.rawServer(t, e.Project, e.Location, e.Pool4, "gone", "10.0.0.42", server.StatusDeleting)
	newPool := func(bridge, cidr string, vlan *int) int64 {
		prefix := strings.TrimSuffix(cidr, "0/24")
		return e.App.Ent.IPPool.Create().SetLocationID(e.Location).SetFamily("ipv4").SetBridge(bridge).SetNillableVlanTag(vlan).
			SetCidr(cidr).SetGateway(prefix + "1").SetDNS([]string{prefix + "1"}).SetRangeStart(prefix + "10").SetRangeEnd(prefix + "20").SaveX(ctx).ID
	}
	vlan := 7
	e.rawServer(t, e.Project, e.Location, newPool("vmbr1", "10.1.0.0/24", nil), "otherbridge", "10.1.0.10", server.StatusRunning)
	e.rawServer(t, e.Project, e.Location, newPool("vmbr0", "10.2.0.0/24", &vlan), "tagged", "10.2.0.10", server.StatusRunning)

	fip := func(ip string, srv, releasing *int64) int64 {
		return e.rawFIP(t, e.Project, e.Pool4, ip, srv, releasing)
	}
	onWeb := fip("10.0.0.50", &web, nil)
	releasing := fip("10.0.0.51", nil, &full)
	busy := fip("10.0.0.52", nil, nil)
	_, err := actions.Create(ctx, e.App.Ent, e.Project, floatingips.KindAssign, []actions.Resource{{ID: busy, Type: floatingips.ResourceType}})
	require.NoError(t, err)
	onGone := fip("10.0.0.53", &gone, nil)
	free := fip("10.0.0.54", nil, nil)

	unassign := func(id int64) func() error {
		return func() error { _, err := e.svc.Unassign(ctx, e.Project, id); return err }
	}
	assign := func(id int64, to string) func() error {
		return func() error { _, err := e.svc.Assign(ctx, e.Project, id, to); return err }
	}
	cases := []struct {
		name   string
		run    func() error
		status int
		code   string
		msg    string
	}{
		{"other project's floating IP", assign(otherFIP, "web"), 404, "not_found", ""},
		{"other project's server", assign(free, ref(otherServer)), 404, "not_found", ""},
		{"other location", assign(free, "far"), 422, "invalid_input", msgSegment},
		{"no primary of the family", assign(free, ref(v6)), 422, "invalid_input", msgSegment},
		{"other bridge", assign(free, "otherbridge"), 422, "invalid_input", msgSegment},
		{"other vlan", assign(free, "tagged"), 422, "invalid_input", msgSegment},
		{"same server", assign(onWeb, "web"), 422, "invalid_input", msgSame},
		{"eleventh on a server", assign(free, "full"), 422, "limit_exceeded", msgLimit},
		{"still releasing", assign(releasing, "web"), 409, "locked", msgReleasing},
		{"running action", assign(busy, "web"), 409, "locked", msgBusy},
		{"target deleting", assign(free, "gone"), 409, "locked", msgDeleting},
		{"current server deleting on move", assign(onGone, "web"), 409, "locked", msgDeleting},
		{"current server deleting on unassign", unassign(onGone), 409, "locked", msgDeleting},
		{"other project's floating IP unassign", unassign(otherFIP), 404, "not_found", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { requireAPIErr(t, c.run(), c.status, c.code, c.msg) })
	}
	n, err := e.App.Ent.Action.Query().Where(action.CommandIn(floatingips.KindAssign, floatingips.KindUnassign)).Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n, "no refused request starts an action")
}

func TestMoveRecordsRelease(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	a := e.RunningServer("a").ServerID
	b := e.RunningServer("b").ServerID
	f, _, err := e.svc.Create(ctx, e.Project, floatingips.CreateInput{Name: "vip", Family: "ipv4", Location: "eu-1"})
	require.NoError(t, err)

	act, err := e.svc.Assign(ctx, e.Project, f.ID, "a")
	require.NoError(t, err)
	require.Equal(t, []actions.Resource{{ID: f.ID, Type: "floating_ip"}, {ID: a, Type: "server"}}, act.Resources)
	require.NoError(t, actions.Succeed(ctx, e.App.Ent, act.ID))

	move, err := e.svc.Assign(ctx, e.Project, f.ID, "vip-b-does-not-exist")
	requireAPIErr(t, err, http.StatusNotFound, "not_found", "")
	require.Empty(t, move.Command)

	move, err = e.svc.Assign(ctx, e.Project, f.ID, ref(b))
	require.NoError(t, err)
	require.Equal(t, floatingips.KindAssign, move.Command)
	require.ElementsMatch(t, []actions.Resource{{ID: f.ID, Type: "floating_ip"}, {ID: a, Type: "server"}, {ID: b, Type: "server"}}, move.Resources)

	r := e.row(t, f.ID)
	require.Equal(t, &a, r.ReleasingFromID)
	require.Equal(t, &b, r.ServerID)
	require.Equal(t, &a, r.PreviousServerID)
	require.Nil(t, r.PendingGen)
	got, err := e.svc.Get(ctx, e.Project, f.ID)
	require.NoError(t, err)
	require.Equal(t, "releasing", got.Status)
	require.Equal(t, "b", got.Guest.Server)
	require.Equal(t, "a", got.Guest.PreviousServer)
	require.Contains(t, got.Guest.Remove, "sudo ip addr del 10.0.0.12/32 dev eth0")
	allowed, err := floatingips.AllowedIPs(ctx, e.App.Ent, b)
	require.NoError(t, err)
	require.Empty(t, allowed)

	list, total, err := e.svc.Actions(ctx, e.Project, f.ID, actions.ListFilter{}, 1, 10)
	require.NoError(t, err)
	require.Equal(t, 2, total)
	require.Len(t, list, 2)
}

func TestUnassignNotAssigned(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	f, _, err := e.svc.Create(ctx, e.Project, floatingips.CreateInput{Family: "ipv4", Location: "eu-1"})
	require.NoError(t, err)
	_, err = e.svc.Unassign(ctx, e.Project, f.ID)
	requireAPIErr(t, err, http.StatusUnprocessableEntity, "invalid_input", msgNotAssign)
}

func TestUnassignAndDelete(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	web := e.RunningServer("web").ServerID
	f, act, err := e.svc.Create(ctx, e.Project, floatingips.CreateInput{Family: "ipv4", Server: "web"})
	require.NoError(t, err)
	require.NoError(t, actions.Succeed(ctx, e.App.Ent, act.ID))

	un, err := e.svc.Unassign(ctx, e.Project, f.ID)
	require.NoError(t, err)
	require.Equal(t, floatingips.KindUnassign, un.Command)
	require.Equal(t, []actions.Resource{{ID: f.ID, Type: "floating_ip"}, {ID: web, Type: "server"}}, un.Resources)
	r := e.row(t, f.ID)
	require.Nil(t, r.ServerID)
	require.Equal(t, &web, r.ReleasingFromID)
	require.Equal(t, &web, r.PreviousServerID)
	got, err := e.svc.Get(ctx, e.Project, f.ID)
	require.NoError(t, err)
	require.Empty(t, got.Guest.Server)
	require.Equal(t, "web", got.Guest.PreviousServer)

	_, err = e.svc.Delete(ctx, e.Project, f.ID)
	requireAPIErr(t, err, http.StatusConflict, "locked", msgBusy)
	require.NoError(t, actions.Succeed(ctx, e.App.Ent, un.ID))
	del, err := e.svc.Delete(ctx, e.Project, f.ID)
	require.NoError(t, err, "a delete waits for the release")
	require.Equal(t, floatingips.KindDelete, del.Command)
	r = e.row(t, f.ID)
	require.True(t, r.Deleting)
	require.Equal(t, &web, r.ReleasingFromID)
	require.Equal(t, 1, e.App.Ent.IPAllocation.Query().Where(ipallocation.OwnerEQ(ipallocation.OwnerFloating)).CountX(ctx), "the worker frees it")

	// Deleting an assigned floating IP unassigns it too.
	g, act, err := e.svc.Create(ctx, e.Project, floatingips.CreateInput{Family: "ipv4", Server: "web"})
	require.NoError(t, err)
	require.NoError(t, actions.Succeed(ctx, e.App.Ent, act.ID))
	del, err = e.svc.Delete(ctx, e.Project, g.ID)
	require.NoError(t, err)
	require.Equal(t, []actions.Resource{{ID: g.ID, Type: "floating_ip"}, {ID: web, Type: "server"}}, del.Resources)
	r = e.row(t, g.ID)
	require.True(t, r.Deleting)
	require.Nil(t, r.ServerID)
	require.Equal(t, &web, r.ReleasingFromID)
	got, err = e.svc.Get(ctx, e.Project, g.ID)
	require.NoError(t, err)
	require.Equal(t, "deleting", got.Status)
}

func TestUpdateAndList(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.RunningServer("web")
	f, act, err := e.svc.Create(ctx, e.Project, floatingips.CreateInput{Family: "ipv4", Server: "web", Labels: map[string]string{"env": "prod"}})
	require.NoError(t, err)
	g, _, err := e.svc.Create(ctx, e.Project, floatingips.CreateInput{Name: "spare", Family: "ipv6", Location: "eu-1"})
	require.NoError(t, err)

	name, desc := "vip", "the vip"
	_, err = e.svc.Update(ctx, e.Project, f.ID, floatingips.UpdateInput{Name: &name})
	requireAPIErr(t, err, http.StatusConflict, "locked", msgBusy)
	require.NoError(t, actions.Succeed(ctx, e.App.Ent, act.ID))
	u, err := e.svc.Update(ctx, e.Project, f.ID, floatingips.UpdateInput{Name: &name, Description: &desc})
	require.NoError(t, err)
	require.Equal(t, "vip", u.Name)
	require.Equal(t, "the vip", u.Description)
	require.Equal(t, map[string]string{"env": "prod"}, u.Labels)
	taken := "spare"
	_, err = e.svc.Update(ctx, e.Project, f.ID, floatingips.UpdateInput{Name: &taken})
	requireAPIErr(t, err, http.StatusConflict, "uniqueness_error", "")

	all, total, err := e.svc.List(ctx, e.Project, floatingips.ListFilter{}, 1, 10)
	require.NoError(t, err)
	require.Equal(t, 2, total)
	require.Equal(t, []int64{f.ID, g.ID}, []int64{all[0].ID, all[1].ID})
	for _, c := range []struct {
		f    floatingips.ListFilter
		want []int64
	}{
		{floatingips.ListFilter{Name: "spare"}, []int64{g.ID}},
		{floatingips.ListFilter{LabelSelector: "env=prod"}, []int64{f.ID}},
		{floatingips.ListFilter{LabelSelector: "!env"}, []int64{g.ID}},
		{floatingips.ListFilter{Server: "web"}, []int64{f.ID}},
		{floatingips.ListFilter{Server: "nope"}, []int64{}},
		{floatingips.ListFilter{Location: "eu-1"}, []int64{f.ID, g.ID}},
		{floatingips.ListFilter{Location: "nowhere"}, []int64{}},
	} {
		got, _, err := e.svc.List(ctx, e.Project, c.f, 1, 10)
		require.NoError(t, err)
		ids := []int64{}
		for _, x := range got {
			ids = append(ids, x.ID)
		}
		require.Equal(t, c.want, ids, "%+v", c.f)
	}
	_, err = e.svc.Get(ctx, e.Admin.CreateProject("q"), f.ID)
	requireAPIErr(t, err, http.StatusNotFound, "not_found", "")
}

func TestResumeStuck(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	web := e.RunningServer("web").ServerID
	pending := e.rawFIP(t, e.Project, e.Pool4, "10.0.0.60", &web, nil)
	require.NoError(t, e.App.Ent.IPAddress.UpdateOneID(pending).SetPendingGen(3).Exec(ctx))
	releasing := e.rawFIP(t, e.Project, e.Pool4, "10.0.0.61", nil, &web)
	deleting := e.rawFIP(t, e.Project, e.Pool4, "10.0.0.62", nil, nil)
	require.NoError(t, e.App.Ent.IPAddress.UpdateOneID(deleting).SetDeleting(true).Exec(ctx))
	held := e.rawFIP(t, e.Project, e.Pool4, "10.0.0.63", nil, &web)
	_, err := actions.Create(ctx, e.App.Ent, e.Project, floatingips.KindUnassign, []actions.Resource{{ID: held, Type: floatingips.ResourceType}})
	require.NoError(t, err)
	e.rawFIP(t, e.Project, e.Pool4, "10.0.0.64", &web, nil)

	require.NoError(t, e.svc.ResumeStuck(ctx))
	for id, kind := range map[int64]string{pending: floatingips.KindAssign, releasing: floatingips.KindUnassign, deleting: floatingips.KindDelete} {
		acts, _, err := e.svc.Actions(ctx, e.Project, id, actions.ListFilter{}, 1, 10)
		require.NoError(t, err)
		require.Len(t, acts, 1)
		require.Equal(t, kind, acts[0].Command)
	}
	require.Equal(t, 4, e.App.Ent.Action.Query().Where(action.CommandIn(floatingips.KindAssign, floatingips.KindUnassign, floatingips.KindDelete)).CountX(ctx))
	require.NoError(t, e.svc.ResumeStuck(ctx))
	require.Equal(t, 4, e.App.Ent.Action.Query().Where(action.CommandIn(floatingips.KindAssign, floatingips.KindUnassign, floatingips.KindDelete)).CountX(ctx))
}

func TestProjectNotEmpty(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	p := e.Admin.CreateProject("solo")
	_, _, err := e.svc.Create(ctx, p, floatingips.CreateInput{Family: "ipv4", Location: "eu-1"})
	require.NoError(t, err)
	err = e.proj.AdminDelete(ctx, p)
	requireAPIErr(t, err, http.StatusConflict, "project_not_empty", "Delete the project's servers, volumes, floating IPs and networks first.")
}

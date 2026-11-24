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

package primaryips_test

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/apierr"
	"github.com/nebla-cloud/nebla/internal/catalog"
	"github.com/nebla-cloud/nebla/internal/ent/action"
	"github.com/nebla-cloud/nebla/internal/ent/ipaddress"
	"github.com/nebla-cloud/nebla/internal/ent/ipallocation"
	"github.com/nebla-cloud/nebla/internal/ent/server"
	"github.com/nebla-cloud/nebla/internal/ext"
	"github.com/nebla-cloud/nebla/internal/floatingips"
	"github.com/nebla-cloud/nebla/internal/httpapi"
	"github.com/nebla-cloud/nebla/internal/ipam"
	"github.com/nebla-cloud/nebla/internal/primaryips"
	"github.com/nebla-cloud/nebla/internal/projects"
	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/stretchr/testify/require"
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
	svc  *primaryips.Service
	proj *projects.Service
	bus  *bus
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := testutil.NewServerEnv(testutil.NewApp(t, testutil.QueueOnlyJobs(t)))
	a := e.App
	hooks, _ := httpapi.ServerHooks(projects.Hooks{}, catalog.Hooks{})
	proj := projects.New(a.Ent, projects.Options{Hooks: hooks})
	b := &bus{}
	svc := primaryips.New(primaryips.Options{SQL: a.SQL, Ent: a.Ent, Catalog: a.Catalog, Projects: proj, Jobs: a.Jobs, Bus: b})
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

func (e *env) create(t *testing.T, in primaryips.CreateInput) primaryips.PrimaryIP {
	t.Helper()
	if in.Type == "" {
		in.Type = "ipv4"
	}
	in.Location = "eu-1"
	p, err := e.svc.Create(context.Background(), e.Project, in)
	require.NoError(t, err)
	return p
}

func TestCreateStandalone(t *testing.T) {
	e := newEnv(t)
	p := e.create(t, primaryips.CreateInput{})
	require.Equal(t, "ip-ipv4-"+strconv.FormatInt(p.ID, 10), p.Name)
	require.Nil(t, p.Server)
	require.True(t, p.AutoDelete)
	require.Equal(t, "active", p.Status)
	require.Equal(t, "ipv4", p.Type)
	require.Equal(t, "10.0.0.10", p.IP)
	require.Equal(t, "eu-1", p.Location.Name)

	named := e.create(t, primaryips.CreateInput{Type: "ipv6", Name: "keep", AutoDelete: new(false), Labels: map[string]string{"a": "b"}})
	require.Equal(t, "keep", named.Name)
	require.False(t, named.AutoDelete)
	require.Equal(t, map[string]string{"a": "b"}, named.Labels)

	evs := e.bus.all()
	require.Len(t, evs, 2)
	require.Equal(t, ext.PrimaryIPCreated, evs[0].Kind)
	require.Equal(t, "primary_ip", evs[0].ResourceType)
	require.Equal(t, p.ID, evs[0].ResourceID)
	require.Equal(t, e.Project, evs[0].ProjectID)

	row := e.App.Ent.IPAddress.GetX(context.Background(), p.ID)
	require.Equal(t, ipaddress.KindPrimary, row.Kind)
}

func TestCreateValidation(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	_, err := e.svc.Create(ctx, e.Project, primaryips.CreateInput{Type: "ipv5", Location: "eu-1"})
	requireAPIErr(t, err, http.StatusUnprocessableEntity, "invalid_input", "")
	_, err = e.svc.Create(ctx, e.Project, primaryips.CreateInput{Type: "ipv4"})
	requireAPIErr(t, err, http.StatusUnprocessableEntity, "invalid_input", "")
	_, err = e.svc.Create(ctx, e.Project, primaryips.CreateInput{Type: "ipv4", Location: "eu-1", Name: "Bad_Name"})
	requireAPIErr(t, err, http.StatusUnprocessableEntity, "invalid_input", "")
	e.create(t, primaryips.CreateInput{Name: "dup"})
	_, err = e.svc.Create(ctx, e.Project, primaryips.CreateInput{Type: "ipv4", Location: "eu-1", Name: "dup"})
	requireAPIErr(t, err, http.StatusConflict, "uniqueness_error", "primary IP name is already taken")
}

func TestCreateQuota(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.Admin.SetQuota(e.Project, 10, 100, 100_000, 1000, 1)
	e.RunningServer("web")
	_, err := e.svc.Create(ctx, e.Project, primaryips.CreateInput{Type: "ipv4", Location: "eu-1"})
	requireAPIErr(t, err, http.StatusUnprocessableEntity, "quota_exceeded", "")
}

func TestCrossProject404(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	p := e.create(t, primaryips.CreateInput{})
	other := e.Admin.CreateProject("other")
	_, err := e.svc.Get(ctx, other, p.ID)
	requireAPIErr(t, err, http.StatusNotFound, "not_found", "")
	_, err = e.svc.Update(ctx, other, p.ID, primaryips.UpdateInput{Description: new("x")})
	requireAPIErr(t, err, http.StatusNotFound, "not_found", "")
	requireAPIErr(t, e.svc.Delete(ctx, other, p.ID), http.StatusNotFound, "not_found", "")
	_, _, err = e.svc.Actions(ctx, other, p.ID, actions.ListFilter{}, 1, 10)
	requireAPIErr(t, err, http.StatusNotFound, "not_found", "")
	_, err = primaryips.ByRef(ctx, e.App.Ent, other, p.Name)
	requireAPIErr(t, err, http.StatusNotFound, "not_found", "")
}

func TestDeleteAssignedRefused(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	web := e.RunningServer("web")
	row, err := primaryips.ByRef(ctx, e.App.Ent, e.Project, "web-ipv4")
	require.NoError(t, err)
	require.Equal(t, web.ServerID, *row.ServerID)
	requireAPIErr(t, e.svc.Delete(ctx, e.Project, row.ID), http.StatusConflict, "ip_in_use", "Unassign it first.")
	p, err := e.svc.Get(ctx, e.Project, row.ID)
	require.NoError(t, err)
	require.Equal(t, &primaryips.Ref{ID: web.ServerID, Name: "web"}, p.Server)
}

func TestDeleteReleasingRefused(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	web := e.RunningServer("web")
	p := e.create(t, primaryips.CreateInput{})
	e.App.Ent.IPAddress.UpdateOneID(p.ID).SetReleasingFromID(web.ServerID).ExecX(ctx)
	requireAPIErr(t, e.svc.Delete(ctx, e.Project, p.ID), http.StatusConflict, "locked", "The address is still being released from the previous server.")
	got, err := e.svc.Get(ctx, e.Project, p.ID)
	require.NoError(t, err)
	require.Equal(t, "releasing", got.Status)
}

func TestDeleteFreesAndPublishes(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	p := e.create(t, primaryips.CreateInput{})
	require.NoError(t, e.svc.Delete(ctx, e.Project, p.ID))
	_, err := e.svc.Get(ctx, e.Project, p.ID)
	requireAPIErr(t, err, http.StatusNotFound, "not_found", "")
	n, err := e.App.Ent.IPAllocation.Query().Where(ipallocation.OwnerEQ(ipallocation.OwnerPrimary)).Count(ctx)
	require.NoError(t, err)
	require.Zero(t, n)
	evs := e.bus.all()
	require.Len(t, evs, 2)
	require.Equal(t, ext.PrimaryIPDeleted, evs[1].Kind)
	require.Equal(t, p.ID, evs[1].ResourceID)
	require.Equal(t, "10.0.0.10", evs[1].Data["ip"])
	again := e.create(t, primaryips.CreateInput{})
	require.Equal(t, "10.0.0.10", again.IP)
}

func TestDeleteBusy(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	p := e.create(t, primaryips.CreateInput{})
	_, err := actions.Create(ctx, e.App.Ent, e.Project, primaryips.KindAssign, []actions.Resource{{ID: p.ID, Type: primaryips.ResourceType}})
	require.NoError(t, err)
	requireAPIErr(t, e.svc.Delete(ctx, e.Project, p.ID), http.StatusConflict, "locked", "the primary IP has a running action")
}

func TestUpdateAutoDelete(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	p := e.create(t, primaryips.CreateInput{})
	got, err := e.svc.Update(ctx, e.Project, p.ID, primaryips.UpdateInput{AutoDelete: new(false)})
	require.NoError(t, err)
	require.False(t, got.AutoDelete)
	require.Equal(t, p.Name, got.Name)
	lbls := map[string]string{"env": "prod"}
	got, err = e.svc.Update(ctx, e.Project, p.ID, primaryips.UpdateInput{Name: new("renamed"), Description: new("d"), Labels: &lbls})
	require.NoError(t, err)
	require.False(t, got.AutoDelete, "untouched fields stay")
	require.Equal(t, "renamed", got.Name)
	require.Equal(t, "d", got.Description)
	require.Equal(t, lbls, got.Labels)
	got, err = e.svc.Update(ctx, e.Project, p.ID, primaryips.UpdateInput{AutoDelete: new(true)})
	require.NoError(t, err)
	require.True(t, got.AutoDelete)
	_, err = e.svc.Update(ctx, e.Project, p.ID, primaryips.UpdateInput{Name: new("Bad_Name")})
	requireAPIErr(t, err, http.StatusUnprocessableEntity, "invalid_input", "")
}

func TestListFilters(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.RunningServer("web")
	a := e.create(t, primaryips.CreateInput{Labels: map[string]string{"tier": "db"}})
	b := e.create(t, primaryips.CreateInput{Type: "ipv6"})
	fip, _, err := floatingips.New(floatingips.Options{SQL: e.App.SQL, Ent: e.App.Ent, Catalog: e.App.Catalog, Projects: e.proj, Jobs: e.App.Jobs}).
		Create(ctx, e.Project, floatingips.CreateInput{Family: "ipv4", Location: "eu-1"})
	require.NoError(t, err)

	all, total, err := e.svc.List(ctx, e.Project, primaryips.ListFilter{}, 1, 50)
	require.NoError(t, err)
	require.Equal(t, 3, total, "web's IPv4 plus the two standalone; no floating IP")
	for _, p := range all {
		require.NotEqual(t, fip.ID, p.ID)
	}

	got, total, err := e.svc.List(ctx, e.Project, primaryips.ListFilter{Type: "ipv6"}, 1, 50)
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Len(t, got, 1)
	got, _, err = e.svc.List(ctx, e.Project, primaryips.ListFilter{Server: "web", Type: "ipv4"}, 1, 50)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "web-ipv4", got[0].Name)
	got, total, err = e.svc.List(ctx, e.Project, primaryips.ListFilter{LabelSelector: "tier=db"}, 1, 50)
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Equal(t, a.ID, got[0].ID)
	got, _, err = e.svc.List(ctx, e.Project, primaryips.ListFilter{Name: b.Name}, 1, 50)
	require.NoError(t, err)
	require.Len(t, got, 1)
	got, total, err = e.svc.List(ctx, e.Project, primaryips.ListFilter{Server: "nope"}, 1, 50)
	require.NoError(t, err)
	require.Zero(t, total)
	require.Empty(t, got)
	got, total, err = e.svc.List(ctx, e.Project, primaryips.ListFilter{}, 2, 2)
	require.NoError(t, err)
	require.Equal(t, 3, total)
	require.Len(t, got, 1)
}

func TestByRefIgnoresFloating(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	fip, _, err := floatingips.New(floatingips.Options{SQL: e.App.SQL, Ent: e.App.Ent, Catalog: e.App.Catalog, Projects: e.proj, Jobs: e.App.Jobs}).
		Create(ctx, e.Project, floatingips.CreateInput{Family: "ipv4", Location: "eu-1", Name: "float"})
	require.NoError(t, err)
	_, err = primaryips.ByRef(ctx, e.App.Ent, e.Project, strconv.FormatInt(fip.ID, 10))
	requireAPIErr(t, err, http.StatusNotFound, "not_found", "")
	_, err = primaryips.ByRef(ctx, e.App.Ent, e.Project, "float")
	requireAPIErr(t, err, http.StatusNotFound, "not_found", "")
	_, err = e.svc.Get(ctx, e.Project, fip.ID)
	requireAPIErr(t, err, http.StatusNotFound, "not_found", "")
	requireAPIErr(t, e.svc.Delete(ctx, e.Project, fip.ID), http.StatusNotFound, "not_found", "")

	p := e.create(t, primaryips.CreateInput{Name: "mine"})
	byID, err := primaryips.ByRef(ctx, e.App.Ent, e.Project, strconv.FormatInt(p.ID, 10))
	require.NoError(t, err)
	byName, err := primaryips.ByRef(ctx, e.App.Ent, e.Project, "mine")
	require.NoError(t, err)
	require.Equal(t, byID.ID, byName.ID)
}

func (e *env) off(t *testing.T, id int64) {
	t.Helper()
	require.NoError(t, e.App.Ent.Server.UpdateOneID(id).SetStatus(server.StatusOff).Exec(context.Background()))
}

func (e *env) rowOf(t *testing.T, ref string) int64 {
	t.Helper()
	row, err := primaryips.ByRef(context.Background(), e.App.Ent, e.Project, ref)
	require.NoError(t, err)
	return row.ID
}

func TestAssignRefusals(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	web1 := e.RunningServer("web1")
	off1 := e.RunningServer("off1")
	e.off(t, off1.ServerID)
	busySrv := e.RunningServer("busy")
	e.off(t, busySrv.ServerID)
	assign := func(id int64, srv string) error {
		_, _, err := e.svc.Assign(ctx, e.Project, id, srv)
		return err
	}

	t.Run("not found", func(t *testing.T) {
		p := e.create(t, primaryips.CreateInput{Type: "ipv6"})
		other := e.Admin.CreateProject("other")
		_, _, err := e.svc.Assign(ctx, other, p.ID, "off1")
		requireAPIErr(t, err, http.StatusNotFound, "not_found", "")
		requireAPIErr(t, assign(999999, "off1"), http.StatusNotFound, "not_found", "")
		requireAPIErr(t, assign(p.ID, "nope"), http.StatusNotFound, "not_found", "")
	})
	t.Run("busy", func(t *testing.T) {
		p := e.create(t, primaryips.CreateInput{Type: "ipv6"})
		_, err := actions.Create(ctx, e.App.Ent, e.Project, primaryips.KindAssign, []actions.Resource{{ID: p.ID, Type: primaryips.ResourceType}})
		require.NoError(t, err)
		requireAPIErr(t, assign(p.ID, "off1"), http.StatusConflict, "locked", "the primary IP has a running action")
	})
	t.Run("releasing", func(t *testing.T) {
		p := e.create(t, primaryips.CreateInput{Type: "ipv6"})
		e.App.Ent.IPAddress.UpdateOneID(p.ID).SetReleasingFromID(web1.ServerID).ExecX(ctx)
		requireAPIErr(t, assign(p.ID, "off1"), http.StatusConflict, "locked", "The address is still being released from the previous server.")
	})
	t.Run("ip_in_use", func(t *testing.T) {
		requireAPIErr(t, assign(e.rowOf(t, "web1-ipv4"), "off1"), http.StatusConflict, "ip_in_use", "Unassign it from web1 first.")
	})
	t.Run("location", func(t *testing.T) {
		loc2 := e.Admin.CreateLocation("eu-2")
		e.Admin.CreateIPPool(loc2, "ipv6", "2001:db8:2::/64", "2001:db8:2::1", "2001:db8:2::10", "2001:db8:2::20")
		p, err := e.svc.Create(ctx, e.Project, primaryips.CreateInput{Type: "ipv6", Location: "eu-2"})
		require.NoError(t, err)
		requireAPIErr(t, assign(p.ID, "off1"), http.StatusUnprocessableEntity, "invalid_input", "That IP is in a different location.")
	})
	t.Run("server_not_stopped", func(t *testing.T) {
		p := e.create(t, primaryips.CreateInput{Type: "ipv6"})
		requireAPIErr(t, assign(p.ID, "web1"), http.StatusConflict, "server_not_stopped", "Stop the server first.")
	})
	t.Run("server_has_ip", func(t *testing.T) {
		p := e.create(t, primaryips.CreateInput{})
		requireAPIErr(t, assign(p.ID, "off1"), http.StatusConflict, "server_has_ip", "This server already has a primary IPv4.")
	})
	t.Run("segment", func(t *testing.T) {
		e.Admin.CreateSegmentPool(e.Location, "ipv6", "vmbr1", new(42), "2001:db8:1::/64", "2001:db8:1::1", "2001:db8:1::10", "2001:db8:1::20")
		id, _ := e.PrimaryIPOn("ipv6", ipam.Segment{Bridge: "vmbr1", VLAN: new(42)})
		requireAPIErr(t, assign(id, "off1"), http.StatusUnprocessableEntity, "invalid_input",
			"This IP can't be used with that server; it's in a different network.")
	})
	t.Run("server busy", func(t *testing.T) {
		p := e.create(t, primaryips.CreateInput{Type: "ipv6"})
		_, err := actions.Create(ctx, e.App.Ent, e.Project, "reboot_server", []actions.Resource{{ID: busySrv.ServerID, Type: "server"}})
		require.NoError(t, err)
		requireAPIErr(t, assign(p.ID, "busy"), http.StatusConflict, "locked", "the server has a running action")
	})
	t.Run("ok", func(t *testing.T) {
		p := e.create(t, primaryips.CreateInput{Type: "ipv6"})
		e.App.Ent.Server.UpdateOneID(off1.ServerID).SetNetRefresh(true).ExecX(ctx)
		act, warns, err := e.svc.Assign(ctx, e.Project, p.ID, "off1")
		require.NoError(t, err)
		require.Empty(t, warns)
		require.Equal(t, primaryips.KindAssign, act.Command)
		require.ElementsMatch(t, []actions.Resource{{ID: p.ID, Type: primaryips.ResourceType}, {ID: off1.ServerID, Type: "server"}}, act.Resources)
		row := e.App.Ent.IPAddress.GetX(ctx, p.ID)
		require.Equal(t, &off1.ServerID, row.ServerID)
		require.NotNil(t, row.PendingGen)
		require.Nil(t, row.PreviousServerID)
	})
}

func TestUnassignRefusals(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.RunningServer("web1")
	off1 := e.RunningServer("off1")
	e.off(t, off1.ServerID)
	busySrv := e.RunningServer("busy")
	e.off(t, busySrv.ServerID)
	unassign := func(id int64) error {
		_, _, err := e.svc.Unassign(ctx, e.Project, id)
		return err
	}

	other := e.Admin.CreateProject("other")
	_, _, err := e.svc.Unassign(ctx, other, e.rowOf(t, "off1-ipv4"))
	requireAPIErr(t, err, http.StatusNotFound, "not_found", "")
	_, err = actions.Create(ctx, e.App.Ent, e.Project, "reboot_server", []actions.Resource{{ID: busySrv.ServerID, Type: "server"}})
	require.NoError(t, err)
	requireAPIErr(t, unassign(e.rowOf(t, "busy-ipv4")), http.StatusConflict, "locked", "the server has a running action")

	p := e.create(t, primaryips.CreateInput{Type: "ipv6"})
	requireAPIErr(t, unassign(p.ID), http.StatusUnprocessableEntity, "invalid_input", "The primary IP isn't assigned.")
	requireAPIErr(t, unassign(e.rowOf(t, "web1-ipv4")), http.StatusConflict, "server_not_stopped", "Stop the server first.")

	rel := e.create(t, primaryips.CreateInput{Type: "ipv6"})
	e.App.Ent.IPAddress.UpdateOneID(rel.ID).SetReleasingFromID(off1.ServerID).ExecX(ctx)
	requireAPIErr(t, unassign(rel.ID), http.StatusConflict, "locked", "The address is still being released from the previous server.")

	fsvc := floatingips.New(floatingips.Options{SQL: e.App.SQL, Ent: e.App.Ent, Catalog: e.App.Catalog, Projects: e.proj, Jobs: e.App.Jobs})
	_, fact, err := fsvc.Create(ctx, e.Project, floatingips.CreateInput{Family: "ipv4", Server: "off1"})
	require.NoError(t, err)
	require.NoError(t, actions.Succeed(ctx, e.App.Ent, fact.ID))
	requireAPIErr(t, unassign(e.rowOf(t, "off1-ipv4")), http.StatusConflict, "floating_ips_attached",
		"Unassign this server's IPv4 floating IPs first.")
}

func TestResumeGivesUpAfterRepeatedAssignFailures(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	off1 := e.RunningServer("off1")
	e.off(t, off1.ServerID)
	p := e.create(t, primaryips.CreateInput{Type: "ipv6"})
	res := []actions.Resource{{ID: p.ID, Type: primaryips.ResourceType}, {ID: off1.ServerID, Type: "server"}}
	running := func() int {
		n, err := e.App.Ent.Action.Query().Where(floatingips.ActionsOfType(e.Project, p.ID, primaryips.ResourceType),
			action.StatusEQ(action.StatusRunning)).Count(ctx)
		require.NoError(t, err)
		return n
	}
	// fail records n more failed assigns, all finished two hours ago.
	fail := func(n int) {
		for range n {
			a, err := actions.Create(ctx, e.App.Ent, e.Project, primaryips.KindAssign, res)
			require.NoError(t, err)
			require.NoError(t, actions.Fail(ctx, e.App.Ent, a.ID, actions.CodeFailed, "the vm has no network interface"))
		}
		require.NoError(t, e.App.Ent.Action.Update().Where(floatingips.ActionsOfType(e.Project, p.ID, primaryips.ResourceType)).
			SetFinished(time.Now().Add(-2*time.Hour)).Exec(ctx))
	}
	e.App.Ent.IPAddress.UpdateOneID(p.ID).SetServerID(off1.ServerID).SetPendingGen(1).ExecX(ctx)

	fail(primaryips.MaxAssignFailures - 1)
	require.NoError(t, e.svc.ResumeStuck(ctx))
	require.Equal(t, 1, running(), "below the cap the assign resumes")

	acts, err := e.App.Ent.Action.Query().Where(floatingips.ActionsOfType(e.Project, p.ID, primaryips.ResourceType),
		action.StatusEQ(action.StatusRunning)).All(ctx)
	require.NoError(t, err)
	require.NoError(t, actions.Fail(ctx, e.App.Ent, acts[0].ID, actions.CodeFailed, "the vm has no network interface"))
	fail(0)
	require.NoError(t, e.svc.ResumeStuck(ctx))
	require.Zero(t, running(), "the fifth failure in a row stops the retries")
	row := e.App.Ent.IPAddress.GetX(ctx, p.ID)
	require.Nil(t, row.PendingGen)
	require.Equal(t, &off1.ServerID, row.ServerID, "the IP stays assigned")
	require.NoError(t, e.svc.ResumeStuck(ctx))
	require.Zero(t, running())
}

// An old server is warned once: after its ISO sync opts it into network refresh, later changes warn no more.
func TestOldServerWarnsOnce(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	off1 := e.RunningServer("off1")
	e.off(t, off1.ServerID)
	e.App.Ent.Server.UpdateOneID(off1.ServerID).SetNetRefresh(false).ExecX(ctx)
	p := e.create(t, primaryips.CreateInput{Type: "ipv6"})
	act, warns, err := e.svc.Assign(ctx, e.Project, p.ID, "off1")
	require.NoError(t, err)
	require.NoError(t, actions.Succeed(ctx, e.App.Ent, act.ID))
	require.Equal(t, []string{primaryips.OldServerWarning}, warns)
	require.True(t, e.App.Ent.Server.GetX(ctx, off1.ServerID).InstanceBumpPending)

	// What the sync_iso job does when it applies the bump.
	e.App.Ent.Server.UpdateOneID(off1.ServerID).SetNetRefresh(true).SetInstanceBumpPending(false).ExecX(ctx)
	e.App.Ent.IPAddress.UpdateOneID(p.ID).ClearServerID().ClearPendingGen().ExecX(ctx)
	q := e.create(t, primaryips.CreateInput{Type: "ipv6"})
	_, warns, err = e.svc.Assign(ctx, e.Project, q.ID, "off1")
	require.NoError(t, err)
	require.Empty(t, warns)
	require.False(t, e.App.Ent.Server.GetX(ctx, off1.ServerID).InstanceBumpPending)
}

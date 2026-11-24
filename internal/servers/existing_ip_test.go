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
	"errors"
	"net/http"
	"strconv"
	"testing"

	"github.com/nebla-cloud/nebla/internal/catalog"
	"github.com/nebla-cloud/nebla/internal/ent/ipaddress"
	"github.com/nebla-cloud/nebla/internal/ent/server"
	"github.com/nebla-cloud/nebla/internal/ext"
	"github.com/nebla-cloud/nebla/internal/httpapi"
	"github.com/nebla-cloud/nebla/internal/ipam"
	"github.com/nebla-cloud/nebla/internal/projects"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/riverqueue/river"
	"github.com/stretchr/testify/require"
)

var seg0 = ipam.Segment{Bridge: "vmbr0"}

func busService(e *testutil.ServerEnv, bus *fipBus) *servers.Service {
	hooks, _ := httpapi.ServerHooks(projects.Hooks{}, catalog.Hooks{})
	return servers.New(servers.Options{SQL: e.App.SQL, Ent: e.App.Ent, Catalog: e.App.Catalog,
		Projects: projects.New(e.App.Ent, projects.Options{Hooks: hooks}), Jobs: e.App.Jobs, Bus: bus})
}

func createIn(name string, v4, v6 servers.IPChoice) servers.CreateInput {
	return servers.CreateInput{Name: name, ServerType: "small", Image: "ubuntu", Location: "eu-1", IPv4: v4, IPv6: v6}
}

func existing(id int64) servers.IPChoice {
	return servers.IPChoice{Existing: strconv.FormatInt(id, 10)}
}

func TestCreateWithExistingIP(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	bus := &fipBus{}
	svc := busService(e, bus)
	id, addr := e.PrimaryIPOn("ipv4", seg0)
	e.Admin.SetQuota(e.Project, 10, 100, 100000, 1000, 1)

	srv, _, err := svc.Create(ctx, e.Project, createIn("web", existing(id), servers.IPChoice{}))
	require.NoError(t, err, "the kept IP already counts, so quota is unchanged")
	require.Equal(t, addr, srv.IPv4.String())
	row := e.App.Ent.IPAddress.GetX(ctx, id)
	require.NotNil(t, row.ServerID)
	require.Equal(t, srv.ID, *row.ServerID)
	require.Equal(t, 1, e.App.Ent.IPAllocation.Query().CountX(ctx))
	require.Empty(t, bus.of(ext.PrimaryIPCreated), "no new address, no created event")

	_, _, err = svc.Create(ctx, e.Project, createIn("web2", servers.IPChoice{New: true}, servers.IPChoice{}))
	requireAPIErr(t, err, http.StatusUnprocessableEntity, "quota_exceeded", "")
}

func TestCreateWithExistingIPRefusals(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	svc := busService(e, &fipBus{})
	other := e.App.Ent.Project.Create().SetName("other").SaveX(ctx).ID
	far := e.Admin.CreateLocation("far-1")

	taken, _ := e.PrimaryIPOn("ipv4", seg0)
	holder, _, err := svc.Create(ctx, e.Project, createIn("holder", existing(taken), servers.IPChoice{}))
	require.NoError(t, err)

	elsewhere, _ := e.PrimaryIPOn("ipv4", seg0)
	_, err = e.App.SQL.ExecContext(ctx, "UPDATE ip_addresses SET location_id = $1 WHERE id = $2", far, elsewhere)
	require.NoError(t, err)
	foreign, _ := e.PrimaryIPOn("ipv4", seg0)
	_, err = e.App.SQL.ExecContext(ctx, "UPDATE ip_addresses SET project_id = $1 WHERE id = $2", other, foreign)
	require.NoError(t, err)
	v4, _ := e.PrimaryIPOn("ipv4", seg0)
	releasing, _ := e.PrimaryIPOn("ipv4", seg0)
	e.App.Ent.IPAddress.UpdateOneID(releasing).SetReleasingFromID(holder.ID).ExecX(ctx)

	cases := []struct {
		name         string
		v4, v6       servers.IPChoice
		status       int
		code, detail string
	}{
		{"assigned", existing(taken), servers.IPChoice{}, http.StatusConflict, "ip_in_use", "Unassign it from holder first."},
		{"location", existing(elsewhere), servers.IPChoice{}, http.StatusUnprocessableEntity, "invalid_input", "That IP is in a different location."},
		{"family", servers.IPChoice{}, existing(v4), http.StatusUnprocessableEntity, "invalid_input", "That IP isn't an IPv6 address."},
		{"project", existing(foreign), servers.IPChoice{}, http.StatusNotFound, "not_found", ""},
		{"releasing", existing(releasing), servers.IPChoice{}, http.StatusConflict, "locked", "The address is still being released from the previous server."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, _, err := svc.Create(ctx, e.Project, createIn("n-"+c.name, c.v4, c.v6))
			requireAPIErr(t, err, c.status, c.code, c.detail)
		})
	}
	require.Equal(t, 1, e.App.Ent.Server.Query().CountX(ctx), "a refused create leaves no server")
}

func TestCreatePublishesPrimaryCreated(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	bus := &fipBus{}
	kept, _ := e.PrimaryIPOn("ipv6", seg0)
	srv, _, err := busService(e, bus).Create(ctx, e.Project, createIn("web", servers.IPChoice{New: true}, existing(kept)))
	require.NoError(t, err)
	evs := bus.of(ext.PrimaryIPCreated)
	require.Len(t, evs, 1, "only the new IPv4")
	row := e.App.Ent.IPAddress.Query().Where(ipaddress.ServerID(srv.ID), ipaddress.Family("ipv4")).OnlyX(ctx)
	require.Equal(t, "primary_ip", evs[0].ResourceType)
	require.Equal(t, row.ID, evs[0].ResourceID)
	require.Equal(t, e.Project, evs[0].ProjectID)
}

func TestFailedCreateReturnsExistingIP(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	bus := &fipBus{}
	svc := busService(e, bus)
	kept, _ := e.PrimaryIPOn("ipv4", seg0)
	srv, act, err := svc.Create(ctx, e.Project, createIn("web", existing(kept), servers.IPChoice{New: true}))
	require.NoError(t, err)
	newV6 := e.App.Ent.IPAddress.Query().Where(ipaddress.ServerID(srv.ID), ipaddress.Family("ipv6")).OnlyX(ctx).ID

	o := opts(e)
	o.Bus = bus
	e.App.PVE.FailNext("/clone", http.StatusInternalServerError)
	err = servers.NewCreateServerWorker(o).Work(ctx, jobOf(queuedArgs[servers.CreateServerArgs](t, e, act.ID), 1))
	var cancel *river.JobCancelError
	require.True(t, errors.As(err, &cancel), "%v", err)

	row, err := e.App.Ent.IPAddress.Get(ctx, kept)
	require.NoError(t, err, "the kept IP survives even with auto_delete")
	require.Nil(t, row.ServerID)
	require.True(t, row.AutoDelete)
	require.Equal(t, server.StatusError, e.App.Ent.Server.GetX(ctx, srv.ID).Status)
	require.Equal(t, 1, e.App.Ent.IPAllocation.Query().CountX(ctx), "the new IPv6 is freed")
	del := bus.of(ext.PrimaryIPDeleted)
	require.Len(t, del, 1)
	require.Equal(t, newV6, del[0].ResourceID)
}

func TestDeletePublishesPrimaryDeleted(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	bus := &fipBus{}
	svc := busService(e, bus)
	keep, _ := e.PrimaryIPOn("ipv6", seg0)
	e.App.Ent.IPAddress.UpdateOneID(keep).SetAutoDelete(false).ExecX(ctx)
	srv, act, err := svc.Create(ctx, e.Project, createIn("web", servers.IPChoice{New: true}, existing(keep)))
	require.NoError(t, err)
	auto := e.App.Ent.IPAddress.Query().Where(ipaddress.ServerID(srv.ID), ipaddress.Family("ipv4")).OnlyX(ctx).ID
	o := opts(e)
	o.Bus = bus
	require.NoError(t, servers.NewCreateServerWorker(o).Work(ctx, jobOf(queuedArgs[servers.CreateServerArgs](t, e, act.ID), 1)))

	delAct, err := svc.Delete(ctx, e.Project, srv.ID)
	require.NoError(t, err)
	require.NoError(t, servers.NewDeleteServerWorker(o).Work(ctx, jobOf(queuedArgs[servers.DeleteServerArgs](t, e, delAct.ID), 1)))

	del := bus.of(ext.PrimaryIPDeleted)
	require.Len(t, del, 1, "only the auto_delete row")
	require.Equal(t, auto, del[0].ResourceID)
	require.Equal(t, "primary_ip", del[0].ResourceType)
	kept := e.App.Ent.IPAddress.GetX(ctx, keep)
	require.Nil(t, kept.ServerID)
}

// segB is a second network with an IPv4 and an IPv6 pool.
func segB(e *testutil.ServerEnv) ipam.Segment {
	e.Admin.CreateSegmentPool(e.Location, "ipv4", "vmbr1", new(42), "10.1.0.0/24", "10.1.0.1", "10.1.0.10", "10.1.0.20")
	e.Admin.CreateSegmentPool(e.Location, "ipv6", "vmbr1", new(42), "2001:db9::/64", "2001:db9::1", "2001:db9::10", "2001:db9::ff")
	return ipam.Segment{Bridge: "vmbr1", VLAN: new(42)}
}

func TestCreateNewIPFollowsKeptIPSegment(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	seg := segB(e)
	kept, _ := e.PrimaryIPOn("ipv4", seg)
	srv, _, err := busService(e, &fipBus{}).Create(ctx, e.Project, createIn("web", existing(kept), servers.IPChoice{New: true}))
	require.NoError(t, err)
	v6 := e.App.Ent.IPAddress.Query().Where(ipaddress.ServerID(srv.ID), ipaddress.Family("ipv6")).OnlyX(ctx)
	pool := e.App.Ent.IPAllocation.GetX(ctx, v6.AllocationID).QueryPool().OnlyX(ctx)
	require.Equal(t, "vmbr1", pool.Bridge)
	require.NotNil(t, pool.VlanTag)
	require.Equal(t, 42, *pool.VlanTag)
}

func TestCreateKeptIPsOnDifferentSegmentsRefused(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	seg := segB(e)
	v4, _ := e.PrimaryIPOn("ipv4", seg)
	v6, _ := e.PrimaryIPOn("ipv6", seg0)
	_, _, err := busService(e, &fipBus{}).Create(ctx, e.Project, createIn("web", existing(v4), existing(v6)))
	requireAPIErr(t, err, http.StatusUnprocessableEntity, "invalid_input", "This IP can't be used with that server; it's in a different network.")
	require.Zero(t, e.App.Ent.Server.Query().CountX(ctx))
}

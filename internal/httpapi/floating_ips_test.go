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

package httpapi_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/ent/ipaddress"
	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/stretchr/testify/require"
)

type apiFIP struct {
	ID          int64             `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Labels      map[string]string `json:"labels"`
	Type        string            `json:"type"`
	IP          string            `json:"ip"`
	Server      *int64            `json:"server"`
	Status      string            `json:"status"`
	Location    struct {
		Name string `json:"name"`
	} `json:"location"`
	GuestConfig *struct {
		Server         *string `json:"server"`
		Apply          *string `json:"apply"`
		Persist        *string `json:"persist"`
		PreviousServer *string `json:"previous_server"`
		Remove         *string `json:"remove"`
	} `json:"guest_config"`
}

type apiSrvFIPs struct {
	PublicNet struct {
		FloatingIPs []struct {
			ID int64  `json:"id"`
			IP string `json:"ip"`
		} `json:"floating_ips"`
	} `json:"public_net"`
}

func newFIPEnv(t *testing.T) *testutil.ServerEnv {
	t.Helper()
	return testutil.NewServerEnv(testutil.NewApp(t, testutil.QueueOnlyJobs(t)))
}

func TestFloatingIPsAPI(t *testing.T) {
	e := newFIPEnv(t)
	ctx := context.Background()
	web := e.RunningServer("web")
	fp := func(id int64) string { return "/v1/floating_ips/" + itoa(id) }

	created := decodeJSON[struct {
		FloatingIP apiFIP  `json:"floating_ip"`
		Action     *apiAct `json:"action"`
	}](t, e.App, e.C.Send(http.MethodPost, "/v1/floating_ips", map[string]any{"type": "ipv4", "location": "eu-1", "name": "vip",
		"description": "front", "labels": map[string]string{"env": "prod"}}), http.StatusCreated)
	f := created.FloatingIP
	require.Nil(t, created.Action, "no server, no action")
	require.Equal(t, "vip", f.Name)
	require.Equal(t, "front", f.Description)
	require.Equal(t, "ipv4", f.Type)
	require.Equal(t, "active", f.Status)
	require.Equal(t, "eu-1", f.Location.Name)
	require.Nil(t, f.Server)
	require.Nil(t, f.GuestConfig)
	require.Equal(t, map[string]string{"env": "prod"}, f.Labels)

	list := decodeJSON[struct {
		FloatingIPs []apiFIP `json:"floating_ips"`
		Meta        struct {
			Pagination struct {
				TotalEntries int `json:"total_entries"`
			} `json:"pagination"`
		} `json:"meta"`
	}](t, e.App, e.C.Get("/v1/floating_ips?name=vip&label_selector=env%3Dprod&location=eu-1"), http.StatusOK)
	require.Len(t, list.FloatingIPs, 1)
	require.Equal(t, 1, list.Meta.Pagination.TotalEntries)
	empty := decodeJSON[struct {
		FloatingIPs []apiFIP `json:"floating_ips"`
	}](t, e.App, e.C.Get("/v1/floating_ips?server=web"), http.StatusOK)
	require.Empty(t, empty.FloatingIPs)

	a := decodeJSON[struct {
		Action apiAct `json:"action"`
	}](t, e.App, e.C.Send(http.MethodPost, fp(f.ID)+"/actions/assign", map[string]string{"server": "web"}), http.StatusCreated).Action
	require.Equal(t, "assign_floating_ip", a.Command)
	require.Equal(t, "running", a.Status)
	require.NoError(t, actions.Succeed(ctx, e.App.Ent, a.ID))

	got := decodeJSON[apiFIP](t, e.App, e.C.Get(fp(f.ID)), http.StatusOK)
	require.Equal(t, web.ServerID, *got.Server)
	require.NotNil(t, got.GuestConfig)
	require.Equal(t, "web", *got.GuestConfig.Server)
	require.NotEmpty(t, *got.GuestConfig.Apply)
	require.Nil(t, got.GuestConfig.PreviousServer)
	require.Nil(t, got.GuestConfig.Remove)
	require.Equal(t, []apiFIP{got}, decodeJSON[struct {
		FloatingIPs []apiFIP `json:"floating_ips"`
	}](t, e.App, e.C.Get("/v1/floating_ips?server=web"), http.StatusOK).FloatingIPs)

	srv := decodeJSON[apiSrvFIPs](t, e.App, e.C.Get("/v1/servers/"+itoa(web.ServerID)), http.StatusOK)
	require.Len(t, srv.PublicNet.FloatingIPs, 1)
	require.Equal(t, f.ID, srv.PublicNet.FloatingIPs[0].ID)
	require.Equal(t, got.IP, srv.PublicNet.FloatingIPs[0].IP)
	all := decodeJSON[struct {
		Servers []apiSrvFIPs `json:"servers"`
	}](t, e.App, e.C.Get("/v1/servers"), http.StatusOK)
	require.Len(t, all.Servers, 1)
	require.Len(t, all.Servers[0].PublicNet.FloatingIPs, 1)

	put := decodeJSON[apiFIP](t, e.App, e.C.Send(http.MethodPut, fp(f.ID), map[string]any{"name": "front-vip"}), http.StatusOK)
	require.Equal(t, "front-vip", put.Name)
	require.Equal(t, "front", put.Description, "omitted fields stay")

	acts := decodeJSON[struct {
		Actions []apiAct `json:"actions"`
	}](t, e.App, e.C.Get(fp(f.ID)+"/actions"), http.StatusOK)
	require.Len(t, acts.Actions, 1)
	require.Equal(t, a.ID, acts.Actions[0].ID)

	un := decodeJSON[struct {
		Action apiAct `json:"action"`
	}](t, e.App, e.C.Send(http.MethodPost, fp(f.ID)+"/actions/unassign", nil), http.StatusCreated).Action
	require.Equal(t, "unassign_floating_ip", un.Command)
	require.NoError(t, actions.Succeed(ctx, e.App.Ent, un.ID))
	after := decodeJSON[apiFIP](t, e.App, e.C.Get(fp(f.ID)), http.StatusOK)
	require.Nil(t, after.Server)
	srv = decodeJSON[apiSrvFIPs](t, e.App, e.C.Get("/v1/servers/"+itoa(web.ServerID)), http.StatusOK)
	require.Empty(t, srv.PublicNet.FloatingIPs)

	res := e.C.Send(http.MethodPost, fp(f.ID)+"/actions/unassign", nil)
	require.Equal(t, http.StatusUnprocessableEntity, res.StatusCode, e.App.Body(res))

	del := decodeJSON[struct {
		Action apiAct `json:"action"`
	}](t, e.App, e.C.Send(http.MethodDelete, fp(f.ID), nil), http.StatusOK).Action
	require.Equal(t, "delete_floating_ip", del.Command)
	require.Equal(t, "deleting", decodeJSON[apiFIP](t, e.App, e.C.Get(fp(f.ID)), http.StatusOK).Status)
}

func TestCreateFloatingIPOnServer(t *testing.T) {
	e := newFIPEnv(t)
	web := e.RunningServer("web")
	out := decodeJSON[struct {
		FloatingIP apiFIP  `json:"floating_ip"`
		Action     *apiAct `json:"action"`
	}](t, e.App, e.C.Send(http.MethodPost, "/v1/floating_ips", map[string]any{"type": "ipv4", "server": "web"}), http.StatusCreated)
	require.NotNil(t, out.Action)
	require.Equal(t, "assign_floating_ip", out.Action.Command)
	require.Equal(t, web.ServerID, *out.FloatingIP.Server)
	require.Equal(t, "ipv4", out.FloatingIP.Type)

	res := e.C.Send(http.MethodPost, "/v1/floating_ips", map[string]any{"type": "ipv6", "server": "web"})
	require.Equal(t, http.StatusUnprocessableEntity, res.StatusCode, "the server has no IPv6 address: %s", e.App.Body(res))
}

func TestReadOnlyTokenCannotWriteFloatingIPs(t *testing.T) {
	e := newFIPEnv(t)
	e.RunningServer("web")
	out := decodeJSON[struct {
		FloatingIP apiFIP `json:"floating_ip"`
	}](t, e.App, e.C.Send(http.MethodPost, "/v1/floating_ips", map[string]any{"type": "ipv4", "location": "eu-1"}), http.StatusCreated)
	fp := "/v1/floating_ips/" + itoa(out.FloatingIP.ID)
	ro := e.App.TokenClient(e.Admin.CreateToken(e.Project, "read"))

	for _, path := range []string{"/v1/floating_ips", fp, fp + "/actions"} {
		res := ro.Get(path)
		require.Equal(t, http.StatusOK, res.StatusCode, "%s: %s", path, e.App.Body(res))
		res.Body.Close()
	}
	ctx := context.Background()
	before, err := e.App.Ent.Action.Query().Count(ctx)
	require.NoError(t, err)
	for _, r := range []struct {
		method, path string
		body         any
	}{
		{http.MethodPost, "/v1/floating_ips", map[string]any{"type": "ipv4", "location": "eu-1"}},
		{http.MethodPut, fp, map[string]any{"name": "y"}},
		{http.MethodDelete, fp, nil},
		{http.MethodPost, fp + "/actions/assign", map[string]string{"server": "web"}},
		{http.MethodPost, fp + "/actions/unassign", nil},
	} {
		res := ro.Send(r.method, r.path, r.body)
		require.Equal(t, http.StatusForbidden, res.StatusCode, "%s %s", r.method, r.path)
		require.Equal(t, "forbidden", errCode(t, e.App, res))
	}
	after, err := e.App.Ent.Action.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, before, after)
	require.Equal(t, 1, e.App.Ent.IPAddress.Query().Where(ipaddress.KindEQ(ipaddress.KindFloating)).CountX(ctx))
	require.Equal(t, out.FloatingIP.Name, decodeJSON[apiFIP](t, e.App, ro.Get(fp), http.StatusOK).Name)
}

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
	"github.com/nebla-cloud/nebla/internal/ent/server"
	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/stretchr/testify/require"
)

type apiPIP struct {
	ID          int64             `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Labels      map[string]string `json:"labels"`
	Type        string            `json:"type"`
	IP          string            `json:"ip"`
	Status      string            `json:"status"`
	AutoDelete  bool              `json:"auto_delete"`
	Server      *struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	} `json:"server"`
	Location struct {
		Name string `json:"name"`
	} `json:"location"`
}

type apiSrvPrimary struct {
	PublicNet struct {
		IPv4 *struct {
			ID int64  `json:"id"`
			IP string `json:"ip"`
		} `json:"ipv4"`
		IPv6 *struct {
			ID int64  `json:"id"`
			IP string `json:"ip"`
		} `json:"ipv6"`
	} `json:"public_net"`
}

func ppath(id int64, rest string) string { return "/v1/primary_ips/" + itoa(id) + rest }

func TestPrimaryIPLifecycleHTTP(t *testing.T) {
	ctx := context.Background()
	e := newFIPEnv(t)
	off := e.RunningServer("off1")
	require.NoError(t, e.App.Ent.Server.UpdateOneID(off.ServerID).SetStatus(server.StatusOff).Exec(ctx))
	// off1 holds its own IPv4; free the family by creating a v6-only target instead.
	p := decodeJSON[struct {
		PrimaryIP apiPIP `json:"primary_ip"`
	}](t, e.App, e.C.Send(http.MethodPost, "/v1/primary_ips", map[string]any{"type": "ipv6", "location": "eu-1", "name": "keep6",
		"description": "d", "labels": map[string]string{"env": "prod"}, "auto_delete": false}), http.StatusCreated).PrimaryIP
	require.Equal(t, "keep6", p.Name)
	require.Equal(t, "ipv6", p.Type)
	require.Equal(t, "active", p.Status)
	require.False(t, p.AutoDelete)
	require.Nil(t, p.Server)
	require.Equal(t, "eu-1", p.Location.Name)
	require.Equal(t, map[string]string{"env": "prod"}, p.Labels)

	list := decodeJSON[struct {
		PrimaryIPs []apiPIP `json:"primary_ips"`
		Meta       struct {
			Pagination struct {
				TotalEntries int `json:"total_entries"`
			} `json:"pagination"`
		} `json:"meta"`
	}](t, e.App, e.C.Get("/v1/primary_ips?type=ipv6&name=keep6&label_selector=env%3Dprod&location=eu-1"), http.StatusOK)
	require.Len(t, list.PrimaryIPs, 1)
	require.Equal(t, 1, list.Meta.Pagination.TotalEntries)
	byServer := decodeJSON[struct {
		PrimaryIPs []apiPIP `json:"primary_ips"`
	}](t, e.App, e.C.Get("/v1/primary_ips?server=off1"), http.StatusOK)
	require.Len(t, byServer.PrimaryIPs, 1, "the server's own IPv4")
	require.Equal(t, "ipv4", byServer.PrimaryIPs[0].Type)

	require.Equal(t, p, decodeJSON[apiPIP](t, e.App, e.C.Get(ppath(p.ID, "")), http.StatusOK))
	put := decodeJSON[apiPIP](t, e.App, e.C.Send(http.MethodPut, ppath(p.ID, ""), map[string]any{"name": "ip6", "auto_delete": true}), http.StatusOK)
	require.Equal(t, "ip6", put.Name)
	require.Equal(t, "d", put.Description)
	require.True(t, put.AutoDelete)

	as := decodeJSON[struct {
		Action   apiAct   `json:"action"`
		Warnings []string `json:"warnings"`
	}](t, e.App, e.C.Send(http.MethodPost, ppath(p.ID, "/actions/assign"), map[string]string{"server": "off1"}), http.StatusCreated)
	require.Equal(t, "assign_primary_ip", as.Action.Command)
	require.NotNil(t, as.Warnings, "warnings is always an array")
	require.NoError(t, actions.Succeed(ctx, e.App.Ent, as.Action.ID))
	got := decodeJSON[apiPIP](t, e.App, e.C.Get(ppath(p.ID, "")), http.StatusOK)
	require.NotNil(t, got.Server)
	require.Equal(t, off.ServerID, got.Server.ID)
	require.Equal(t, "off1", got.Server.Name)

	res := e.C.Send(http.MethodDelete, ppath(p.ID, ""), nil)
	require.Equal(t, http.StatusConflict, res.StatusCode)
	require.Equal(t, "ip_in_use", errCode(t, e.App, res))

	acts := decodeJSON[struct {
		Actions []apiAct `json:"actions"`
	}](t, e.App, e.C.Get(ppath(p.ID, "/actions")), http.StatusOK)
	require.Len(t, acts.Actions, 1)

	un := decodeJSON[struct {
		Action   apiAct   `json:"action"`
		Warnings []string `json:"warnings"`
	}](t, e.App, e.C.Send(http.MethodPost, ppath(p.ID, "/actions/unassign"), nil), http.StatusCreated)
	require.Equal(t, "unassign_primary_ip", un.Action.Command)
	require.NoError(t, actions.Succeed(ctx, e.App.Ent, un.Action.ID))
	after := decodeJSON[apiPIP](t, e.App, e.C.Get(ppath(p.ID, "")), http.StatusOK)
	require.Nil(t, after.Server)
	require.Equal(t, "releasing", after.Status, "the worker has not released it yet")
	res = e.C.Send(http.MethodDelete, ppath(p.ID, ""), nil)
	require.Equal(t, http.StatusConflict, res.StatusCode)
	require.Equal(t, "locked", errCode(t, e.App, res))
	// The release worker does not run here, so settle the release by hand.
	e.App.Ent.IPAddress.UpdateOneID(p.ID).ClearReleasingFromID().ExecX(ctx)

	res = e.C.Send(http.MethodDelete, ppath(p.ID, ""), nil)
	require.Equal(t, http.StatusNoContent, res.StatusCode)
	res.Body.Close()
	res = e.C.Get(ppath(p.ID, ""))
	require.Equal(t, http.StatusNotFound, res.StatusCode)
	res.Body.Close()
}

func TestServerPublicNetCarriesPrimaryIPID(t *testing.T) {
	e := newFIPEnv(t)
	web := e.RunningServer("web")
	srv := decodeJSON[apiSrvPrimary](t, e.App, e.C.Get("/v1/servers/"+itoa(web.ServerID)), http.StatusOK)
	require.NotNil(t, srv.PublicNet.IPv4)
	ips := decodeJSON[struct {
		PrimaryIPs []apiPIP `json:"primary_ips"`
	}](t, e.App, e.C.Get("/v1/primary_ips?server=web"), http.StatusOK).PrimaryIPs
	require.Len(t, ips, 1)
	require.Equal(t, ips[0].ID, srv.PublicNet.IPv4.ID)
	require.Equal(t, ips[0].IP, srv.PublicNet.IPv4.IP)
}

func TestCreateServerWithExistingIPHTTP(t *testing.T) {
	e := newFIPEnv(t)
	mk := func(name string) apiPIP {
		return decodeJSON[struct {
			PrimaryIP apiPIP `json:"primary_ip"`
		}](t, e.App, e.C.Send(http.MethodPost, "/v1/primary_ips", map[string]any{"type": "ipv4", "location": "eu-1", "name": name}), http.StatusCreated).PrimaryIP
	}
	byName := mk("web1-ipv4")
	byID := mk("other")
	for _, c := range []struct {
		server string
		ipv4   any
		want   int64
	}{{"a", "web1-ipv4", byName.ID}, {"b", itoa(byID.ID), byID.ID}} {
		out := decodeJSON[struct {
			Server apiSrvPrimary `json:"server"`
		}](t, e.App, e.C.Send(http.MethodPost, "/v1/servers", map[string]any{"name": c.server, "server_type": "small", "image": "ubuntu",
			"location": "eu-1", "public_net": map[string]any{"ipv4": c.ipv4}}), http.StatusCreated)
		require.Equal(t, c.want, out.Server.PublicNet.IPv4.ID)
		got := decodeJSON[apiPIP](t, e.App, e.C.Get(ppath(c.want, "")), http.StatusOK)
		require.NotNil(t, got.Server, c.server)
		require.Equal(t, c.server, got.Server.Name)
	}

	v6 := decodeJSON[struct {
		PrimaryIP apiPIP `json:"primary_ip"`
	}](t, e.App, e.C.Send(http.MethodPost, "/v1/primary_ips", map[string]any{"type": "ipv6", "location": "eu-1", "name": "keep6"}), http.StatusCreated).PrimaryIP
	for n, c := range []struct {
		name       string
		publicNet  any
		want4      bool
		want6      bool
		want6ID    int64
		wantStatus int
	}{
		{"empty", map[string]any{}, true, false, 0, http.StatusCreated},
		{"ipv4 true", map[string]any{"ipv4": true}, true, false, 0, http.StatusCreated},
		{"ipv4 false", map[string]any{"ipv4": false}, false, false, 0, http.StatusCreated},
		{"ipv6 true", map[string]any{"ipv6": true}, true, true, 0, http.StatusCreated},
		{"ipv6 false", map[string]any{"ipv6": false}, true, false, 0, http.StatusCreated},
		{"ipv6 existing", map[string]any{"ipv6": "keep6"}, true, true, v6.ID, http.StatusCreated},
		{"ipv4 number", map[string]any{"ipv4": 5}, false, false, 0, http.StatusUnprocessableEntity},
		{"ipv4 empty string", map[string]any{"ipv4": ""}, false, false, 0, http.StatusUnprocessableEntity},
		{"ipv4 unknown", map[string]any{"ipv4": "nope"}, false, false, 0, http.StatusNotFound},
	} {
		res := e.C.Send(http.MethodPost, "/v1/servers", map[string]any{"name": "t" + itoa(int64(n)), "server_type": "small", "image": "ubuntu",
			"location": "eu-1", "public_net": c.publicNet})
		if c.wantStatus != http.StatusCreated {
			require.Equal(t, c.wantStatus, res.StatusCode, "%s: %s", c.name, e.App.Body(res))
			continue
		}
		out := decodeJSON[struct {
			Server apiSrvPrimary `json:"server"`
		}](t, e.App, res, http.StatusCreated)
		require.Equal(t, c.want4, out.Server.PublicNet.IPv4 != nil, c.name)
		require.Equal(t, c.want6, out.Server.PublicNet.IPv6 != nil, c.name)
		if c.want6ID != 0 {
			require.Equal(t, c.want6ID, out.Server.PublicNet.IPv6.ID, c.name)
		}
	}
	res := e.C.Send(http.MethodPost, "/v1/servers", map[string]any{"name": "bad", "server_type": "small", "image": "ubuntu",
		"location": "eu-1", "public_net": map[string]any{"ipv4": ""}})
	require.Equal(t, "invalid_input", errCode(t, e.App, res))
}

func TestPrimaryIPScopes(t *testing.T) {
	ctx := context.Background()
	e := newFIPEnv(t)
	p := decodeJSON[struct {
		PrimaryIP apiPIP `json:"primary_ip"`
	}](t, e.App, e.C.Send(http.MethodPost, "/v1/primary_ips", map[string]any{"type": "ipv4", "location": "eu-1"}), http.StatusCreated).PrimaryIP

	e.App.CreateUser("ro@example.com", false)
	token := e.Admin.Invite(e.Project, "ro@example.com", "read_only")
	ro := e.App.Login("ro@example.com")
	res := ro.Send(http.MethodPost, "/v1/invites/"+token+"/accept", nil)
	require.Equal(t, http.StatusOK, res.StatusCode, e.App.Body(res))
	e.App.CreateUser("m@example.com", false)
	token = e.Admin.Invite(e.Project, "m@example.com", "member")
	m := e.App.Login("m@example.com")
	res = m.Send(http.MethodPost, "/v1/invites/"+token+"/accept", nil)
	require.Equal(t, http.StatusOK, res.StatusCode, e.App.Body(res))
	readToken := e.App.TokenClient(e.Admin.CreateToken(e.Project, "read"))

	before, err := e.App.Ent.Action.Query().Count(ctx)
	require.NoError(t, err)
	for name, c := range map[string]*testutil.Client{"read_only member": ro.WithProject(e.Project), "read token": readToken} {
		for _, path := range []string{"/v1/primary_ips", ppath(p.ID, ""), ppath(p.ID, "/actions")} {
			res := c.Get(path)
			require.Equal(t, http.StatusOK, res.StatusCode, "%s GET %s: %s", name, path, e.App.Body(res))
			res.Body.Close()
		}
		for _, r := range []struct {
			method, path string
			body         any
		}{
			{http.MethodPost, "/v1/primary_ips", map[string]any{"type": "ipv4", "location": "eu-1"}},
			{http.MethodPut, ppath(p.ID, ""), map[string]any{"name": "y"}},
			{http.MethodDelete, ppath(p.ID, ""), nil},
			{http.MethodPost, ppath(p.ID, "/actions/assign"), map[string]string{"server": "web"}},
			{http.MethodPost, ppath(p.ID, "/actions/unassign"), nil},
		} {
			res := c.Send(r.method, r.path, r.body)
			require.Equal(t, http.StatusForbidden, res.StatusCode, "%s %s %s", name, r.method, r.path)
			require.Equal(t, "forbidden", errCode(t, e.App, res))
		}
	}
	after, err := e.App.Ent.Action.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, before, after)

	mc := m.WithProject(e.Project)
	put := decodeJSON[apiPIP](t, e.App, mc.Send(http.MethodPut, ppath(p.ID, ""), map[string]any{"name": "by-member"}), http.StatusOK)
	require.Equal(t, "by-member", put.Name)
}

func TestPrimaryIPsIsolation(t *testing.T) {
	ctx := context.Background()
	e := newFIPEnv(t)
	e.RunningServer("web")
	theirs := decodeJSON[struct {
		PrimaryIP apiPIP `json:"primary_ip"`
	}](t, e.App, e.C.Send(http.MethodPost, "/v1/primary_ips", map[string]any{"type": "ipv4", "location": "eu-1"}), http.StatusCreated).PrimaryIP
	projA := e.Admin.CreateProject("a")
	tokA := e.App.TokenClient(e.Admin.CreateToken(projA, "read_write"))
	before, err := e.App.Ent.Action.Query().Count(ctx)
	require.NoError(t, err)
	for _, r := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, ppath(theirs.ID, ""), nil},
		{http.MethodPut, ppath(theirs.ID, ""), map[string]any{"name": "x"}},
		{http.MethodDelete, ppath(theirs.ID, ""), nil},
		{http.MethodGet, ppath(theirs.ID, "/actions"), nil},
		{http.MethodPost, ppath(theirs.ID, "/actions/assign"), map[string]string{"server": "web"}},
		{http.MethodPost, ppath(theirs.ID, "/actions/unassign"), nil},
	} {
		res := tokA.Send(r.method, r.path, r.body)
		body := e.App.Body(res)
		require.Equal(t, http.StatusNotFound, res.StatusCode, "%s %s: %s", r.method, r.path, body)
		require.Equal(t, "not_found", errCode2(t, body))
	}
	listed := decodeJSON[struct {
		PrimaryIPs []apiPIP `json:"primary_ips"`
	}](t, e.App, tokA.Get("/v1/primary_ips"), http.StatusOK)
	require.Empty(t, listed.PrimaryIPs)
	after, err := e.App.Ent.Action.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, before, after)
	require.Equal(t, theirs.Name, e.App.Ent.IPAddress.GetX(ctx, theirs.ID).Name)
}

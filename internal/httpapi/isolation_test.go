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
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/nebla-cloud/nebla/internal/ent/volume"
	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/stretchr/testify/require"
)

// TestCrossProjectIsolation: a project-A token never sees or acts on project B's server, firewall, actions, metrics or console.
func TestCrossProjectIsolation(t *testing.T) {
	ctx := context.Background()
	e := testutil.NewServerEnv(testutil.NewApp(t, testutil.QueueOnlyJobs(t)))
	b := e.RunningServer("web")
	res := e.C.Send(http.MethodPost, "/v1/firewalls", map[string]any{"name": "fw", "apply_to": []any{serverRes(b.ServerID)}})
	require.Equal(t, http.StatusCreated, res.StatusCode)
	fwp := "/v1/firewalls/" + strconv.FormatInt(decode[fwCreated](t, e.App, res).Firewall.ID, 10)
	projA := e.Admin.CreateProject("a")
	tokA := e.App.TokenClient(e.Admin.CreateToken(projA, "read_write"))
	before, err := e.App.Ent.Action.Query().Count(ctx)
	require.NoError(t, err)

	srv := "/v1/servers/" + strconv.FormatInt(b.ServerID, 10)
	for _, r := range []struct {
		method, path string
		body         any
	}{
		{method: http.MethodGet, path: srv},
		{method: http.MethodPost, path: srv + "/actions/poweroff"},
		{method: http.MethodGet, path: "/v1/actions/" + strconv.FormatInt(b.ActionID, 10)},
		{method: http.MethodGet, path: srv + "/actions"},
		{method: http.MethodGet, path: srv + "/metrics?type=cpu&timeframe=hour"},
		{method: http.MethodPost, path: srv + "/actions/request_console"},
		{method: http.MethodGet, path: fwp},
		{method: http.MethodPut, path: fwp, body: map[string]any{"name": "x"}},
		{method: http.MethodDelete, path: fwp},
		{method: http.MethodGet, path: fwp + "/actions"},
		{method: http.MethodPost, path: fwp + "/actions/set_rules", body: map[string]any{"rules": []any{}}},
		{method: http.MethodPost, path: fwp + "/actions/apply_to_resources", body: map[string]any{"apply_to": []any{serverRes(b.ServerID)}}},
		{method: http.MethodPost, path: fwp + "/actions/remove_from_resources", body: map[string]any{"remove_from": []any{serverRes(b.ServerID)}}},
	} {
		res := tokA.Send(r.method, r.path, r.body)
		body := e.App.Body(res)
		require.Equal(t, http.StatusNotFound, res.StatusCode, "%s %s: %s", r.method, r.path, body)
		require.JSONEq(t, `{"error":{"code":"not_found","message":"not found"}}`, body)
	}
	after, err := e.App.Ent.Action.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, before, after, "no action started in project B")
	tokens, err := e.App.Ent.ConsoleToken.Query().Count(ctx)
	require.NoError(t, err)
	require.Zero(t, tokens, "no console token minted for project B")

	// The owner's own token still works.
	tokB := e.App.TokenClient(e.Admin.CreateToken(e.Project, "read_write"))
	res = tokB.Get(srv)
	require.Equal(t, http.StatusOK, res.StatusCode, e.App.Body(res))
}

// TestVolumesIsolation: a project-A token never sees or acts on project B's volumes or attaches to B's servers.
func TestVolumesIsolation(t *testing.T) {
	ctx := context.Background()
	e := newVolEnv(t)
	web := e.RunningServer("web")
	vb, a := e.createVolume(e.C, map[string]any{"name": "data", "size": 10, "location": "eu-1"})
	e.settle(a, vb.ID, volume.StatusAvailable, nil, "")
	actB := a.ID
	projA := e.Admin.CreateProject("a")
	tokA := e.App.TokenClient(e.Admin.CreateToken(projA, "read_write"))
	va, aa := e.createVolume(tokA, map[string]any{"name": "mine", "size": 10, "location": "eu-1"})
	e.settle(aa, va.ID, volume.StatusAvailable, nil, "")
	before, err := e.App.Ent.Action.Query().Count(ctx)
	require.NoError(t, err)

	vp := "/v1/volumes/" + strconv.FormatInt(vb.ID, 10)
	for _, r := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, vp, nil},
		{http.MethodPut, vp, map[string]any{"name": "x"}},
		{http.MethodDelete, vp, nil},
		{http.MethodGet, vp + "/actions", nil},
		{http.MethodPost, vp + "/actions/attach", map[string]string{"server": "web"}},
		{http.MethodPost, vp + "/actions/detach", nil},
		{http.MethodPost, vp + "/actions/resize", map[string]int{"size": 20}},
		{http.MethodGet, "/v1/actions/" + strconv.FormatInt(actB, 10), nil},
	} {
		res := tokA.Send(r.method, r.path, r.body)
		body := e.App.Body(res)
		require.Equal(t, http.StatusNotFound, res.StatusCode, "%s %s: %s", r.method, r.path, body)
		require.Equal(t, "not_found", errCode2(t, body))
	}
	// A's own volume cannot reach B's server, by name or id.
	for _, ref := range []string{"web", strconv.FormatInt(web.ServerID, 10)} {
		res := tokA.Send(http.MethodPost, "/v1/volumes/"+strconv.FormatInt(va.ID, 10)+"/actions/attach", map[string]string{"server": ref})
		require.Equal(t, http.StatusNotFound, res.StatusCode, e.App.Body(res))
		res.Body.Close()
		res = tokA.Send(http.MethodPost, "/v1/volumes", map[string]any{"name": "x" + ref, "size": 10, "server": ref})
		require.Equal(t, http.StatusNotFound, res.StatusCode, e.App.Body(res))
		res.Body.Close()
	}
	require.Equal(t, []string{"mine"}, e.list(tokA, ""))
	require.Empty(t, e.list(tokA, "server=web"))
	after, err := e.App.Ent.Action.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, before, after, "no action started")
	got, err := e.App.Ent.Volume.Get(ctx, vb.ID)
	require.NoError(t, err)
	require.Equal(t, "data", got.Name)
}

// TestFloatingIPsIsolation: a project-A token never sees or acts on project B's floating IPs or assigns to B's servers.
func TestFloatingIPsIsolation(t *testing.T) {
	ctx := context.Background()
	e := newFIPEnv(t)
	web := e.RunningServer("web")
	created := decodeJSON[struct {
		FloatingIP apiFIP `json:"floating_ip"`
	}](t, e.App, e.C.Send(http.MethodPost, "/v1/floating_ips", map[string]any{"type": "ipv4", "location": "eu-1"}), http.StatusCreated)
	projA := e.Admin.CreateProject("a")
	tokA := e.App.TokenClient(e.Admin.CreateToken(projA, "read_write"))
	mine := decodeJSON[struct {
		FloatingIP apiFIP `json:"floating_ip"`
	}](t, e.App, tokA.Send(http.MethodPost, "/v1/floating_ips", map[string]any{"type": "ipv4", "location": "eu-1"}), http.StatusCreated).FloatingIP
	before, err := e.App.Ent.Action.Query().Count(ctx)
	require.NoError(t, err)

	fp := "/v1/floating_ips/" + strconv.FormatInt(created.FloatingIP.ID, 10)
	for _, r := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, fp, nil},
		{http.MethodPut, fp, map[string]any{"name": "x"}},
		{http.MethodDelete, fp, nil},
		{http.MethodGet, fp + "/actions", nil},
		{http.MethodPost, fp + "/actions/assign", map[string]string{"server": "web"}},
		{http.MethodPost, fp + "/actions/unassign", nil},
	} {
		res := tokA.Send(r.method, r.path, r.body)
		body := e.App.Body(res)
		require.Equal(t, http.StatusNotFound, res.StatusCode, "%s %s: %s", r.method, r.path, body)
		require.Equal(t, "not_found", errCode2(t, body))
	}
	// A's own floating IP cannot reach B's server, by name or id.
	for _, ref := range []string{"web", strconv.FormatInt(web.ServerID, 10)} {
		res := tokA.Send(http.MethodPost, "/v1/floating_ips/"+strconv.FormatInt(mine.ID, 10)+"/actions/assign", map[string]string{"server": ref})
		require.Equal(t, http.StatusNotFound, res.StatusCode, e.App.Body(res))
		res.Body.Close()
		res = tokA.Send(http.MethodPost, "/v1/floating_ips", map[string]any{"type": "ipv4", "server": ref})
		require.Equal(t, http.StatusNotFound, res.StatusCode, e.App.Body(res))
		res.Body.Close()
	}
	listed := decodeJSON[struct {
		FloatingIPs []apiFIP `json:"floating_ips"`
	}](t, e.App, tokA.Get("/v1/floating_ips"), http.StatusOK)
	require.Len(t, listed.FloatingIPs, 1)
	require.Equal(t, mine.ID, listed.FloatingIPs[0].ID)
	after, err := e.App.Ent.Action.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, before, after, "no action started")
	row, err := e.App.Ent.IPAddress.Get(ctx, created.FloatingIP.ID)
	require.NoError(t, err)
	require.Equal(t, created.FloatingIP.Name, row.Name)
	require.Nil(t, row.ServerID)
}

// TestNetworksIsolation: a project-A token never sees or acts on project B's networks or attaches to B's servers.
func TestNetworksIsolation(t *testing.T) {
	ctx := context.Background()
	e := newNetEnv(t)
	web := e.RunningServer("web")
	nb := createNet(t, e, e.C, map[string]any{"name": "backend", "location": "eu-1"})
	projA := e.Admin.CreateProject("a")
	tokA := e.App.TokenClient(e.Admin.CreateToken(projA, "read_write"))
	mine := createNet(t, e, tokA, map[string]any{"name": "mine", "location": "eu-1"})
	before, err := e.App.Ent.Action.Query().Count(ctx)
	require.NoError(t, err)

	np := "/v1/networks/" + strconv.FormatInt(nb.ID, 10)
	sp := "/v1/servers/" + strconv.FormatInt(web.ServerID, 10)
	for _, r := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, np, nil},
		{http.MethodPut, np, map[string]any{"name": "x"}},
		{http.MethodDelete, np, nil},
		{http.MethodGet, np + "/actions", nil},
		{http.MethodPost, sp + "/actions/attach_to_network", map[string]string{"network": "mine"}},
		{http.MethodPost, sp + "/actions/detach_from_network", map[string]string{"network": "mine"}},
	} {
		res := tokA.Send(r.method, r.path, r.body)
		body := e.App.Body(res)
		require.Equal(t, http.StatusNotFound, res.StatusCode, "%s %s: %s", r.method, r.path, body)
		require.Equal(t, "not_found", errCode2(t, body))
	}
	// A's own servers can't join B's network, by name or id.
	for _, ref := range []string{"backend", strconv.FormatInt(nb.ID, 10)} {
		res := tokA.Send(http.MethodPost, "/v1/servers", map[string]any{"name": "x", "server_type": "small", "image": "ubuntu", "location": "eu-1",
			"networks": []map[string]string{{"network": ref}}})
		require.Equal(t, http.StatusNotFound, res.StatusCode, e.App.Body(res))
		res.Body.Close()
	}
	listed := decodeJSON[struct {
		Networks []apiNet `json:"networks"`
	}](t, e.App, tokA.Get("/v1/networks"), http.StatusOK)
	require.Len(t, listed.Networks, 1)
	require.Equal(t, mine.ID, listed.Networks[0].ID)
	after, err := e.App.Ent.Action.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, before, after, "no action started")
	row, err := e.App.Ent.Network.Get(ctx, nb.ID)
	require.NoError(t, err)
	require.Equal(t, "backend", row.Name)
}

func errCode2(t *testing.T, body string) string {
	t.Helper()
	var e struct{ Error struct{ Code string } }
	require.NoError(t, json.Unmarshal([]byte(body), &e))
	return e.Error.Code
}

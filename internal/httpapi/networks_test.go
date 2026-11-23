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
	"strings"
	"testing"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/ent/server"
	"github.com/nebla-cloud/nebla/internal/ent/servernetwork"
	"github.com/nebla-cloud/nebla/internal/pve"
	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/stretchr/testify/require"
)

type apiNet struct {
	ID       int64             `json:"id"`
	Name     string            `json:"name"`
	Labels   map[string]string `json:"labels"`
	IPRange  string            `json:"ip_range"`
	Location struct {
		Name string `json:"name"`
	} `json:"location"`
	Servers []struct {
		Server int64  `json:"server"`
		Name   string `json:"name"`
		IP     string `json:"ip"`
	} `json:"servers"`
}

type apiGuestCfg struct {
	Apply   *string `json:"apply"`
	Persist *string `json:"persist"`
	Remove  *string `json:"remove"`
	Note    *string `json:"note"`
}

type apiPrivNet struct {
	Network     int64        `json:"network"`
	Name        string       `json:"name"`
	IP          string       `json:"ip"`
	GuestConfig *apiGuestCfg `json:"guest_config"`
}

type apiSrvNets struct {
	ID         int64        `json:"id"`
	PrivateNet []apiPrivNet `json:"private_net"`
}

type netActionResp struct {
	Action      apiAct       `json:"action"`
	GuestConfig *apiGuestCfg `json:"guest_config"`
}

// newNetEnv is a project with eu-1 set up for private networks: zone nebla with three VNets, firewall on.
func newNetEnv(t *testing.T) *testutil.ServerEnv {
	t.Helper()
	e := testutil.NewServerEnv(testutil.NewApp(t, testutil.QueueOnlyJobs(t)))
	e.App.PVE.AddSDN(pve.SDNZone{Zone: "nebla", Type: "vxlan"},
		pve.SDNVNet{VNet: "nb0001", Zone: "nebla"}, pve.SDNVNet{VNet: "nb0002", Zone: "nebla"}, pve.SDNVNet{VNet: "nb0003", Zone: "nebla"})
	require.NoError(t, e.App.Ent.Location.UpdateOneID(e.Location).SetFirewallEnabled(true).Exec(context.Background()))
	setZone(t, e, "nebla")
	return e
}

func adminLocationBody(e *testutil.ServerEnv, extra map[string]any) map[string]any {
	c := e.App.PVE.Conn()
	body := map[string]any{"name": "eu-1", "disk_storage": "ceph", "iso_storage": "local", "pve": map[string]any{
		"url": c.BaseURL, "token_id": c.TokenID, "token_secret": c.Secret, "ca_fingerprint": c.CAFingerprint}}
	for k, v := range extra {
		body[k] = v
	}
	return body
}

type apiAdminLoc struct {
	SdnZone         *string `json:"sdn_zone"`
	VnetPrefix      string  `json:"vnet_prefix"`
	PrivateNetworks struct {
		State     string  `json:"state"`
		Zone      *string `json:"zone"`
		Available int     `json:"available"`
		InUse     int     `json:"in_use"`
		Cooling   int     `json:"cooling"`
		Unusable  []struct {
			VNet   string `json:"vnet"`
			Reason string `json:"reason"`
		} `json:"unusable"`
		Degraded []struct {
			VNet    string `json:"vnet"`
			Network string `json:"network"`
			Reason  string `json:"reason"`
		} `json:"degraded"`
	} `json:"private_networks"`
}

func setZone(t *testing.T, e *testutil.ServerEnv, zone string) apiAdminLoc {
	t.Helper()
	return decodeJSON[apiAdminLoc](t, e.App, e.Admin.Send(http.MethodPut, "/v1/admin/locations/"+itoa(e.Location),
		adminLocationBody(e, map[string]any{"sdn_zone": zone})), http.StatusOK)
}

func createNet(t *testing.T, e *testutil.ServerEnv, c *testutil.Client, body map[string]any) apiNet {
	t.Helper()
	return decodeJSON[struct {
		Network apiNet `json:"network"`
	}](t, e.App, c.Send(http.MethodPost, "/v1/networks", body), http.StatusCreated).Network
}

func settleNIC(t *testing.T, e *testutil.ServerEnv, act apiAct, serverID int64, st servernetwork.State) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, actions.Succeed(ctx, e.App.Ent, act.ID))
	if st == "" {
		e.App.Ent.ServerNetwork.Delete().Where(servernetwork.ServerID(serverID)).ExecX(ctx)
		return
	}
	e.App.Ent.ServerNetwork.Update().Where(servernetwork.ServerID(serverID)).SetState(st).ExecX(ctx)
}

func TestNetworksAPILifecycle(t *testing.T) {
	e := newNetEnv(t)
	np := func(id int64) string { return "/v1/networks/" + itoa(id) }

	n := createNet(t, e, e.C, map[string]any{"name": "backend", "location": "eu-1", "labels": map[string]string{"env": "prod"}})
	require.Equal(t, "backend", n.Name)
	require.Equal(t, "eu-1", n.Location.Name)
	require.Equal(t, map[string]string{"env": "prod"}, n.Labels)
	require.Equal(t, "10.1.0.0/16", n.IPRange, "10.0.0.0/16 overlaps the location's public pool")
	require.Empty(t, n.Servers)

	list := decodeJSON[struct {
		Networks []apiNet `json:"networks"`
		Meta     struct {
			Pagination struct {
				TotalEntries int `json:"total_entries"`
			} `json:"pagination"`
		} `json:"meta"`
	}](t, e.App, e.C.Get("/v1/networks?name=backend&location=eu-1&label_selector=env%3Dprod"), http.StatusOK)
	require.Len(t, list.Networks, 1)
	require.Equal(t, 1, list.Meta.Pagination.TotalEntries)

	put := decodeJSON[apiNet](t, e.App, e.C.Send(http.MethodPut, np(n.ID), map[string]any{"labels": map[string]string{"env": "dev"}}), http.StatusOK)
	require.Equal(t, "backend", put.Name)
	require.Equal(t, map[string]string{"env": "dev"}, put.Labels)

	// A server created into the network gets the first usable address.
	created := e.CreateServer("web", map[string]any{"networks": []map[string]string{{"network": "backend"}}})
	web := decodeJSON[apiSrvNets](t, e.App, e.C.Get("/v1/servers/"+itoa(created.ServerID)), http.StatusOK)
	require.Equal(t, []apiPrivNet{{Network: n.ID, Name: "backend", IP: "10.1.0.2"}}, web.PrivateNet, "no commands for a server that isn't running")

	// A running server is attached through the action; its commands come back with the action.
	db := e.RunningServer("db")
	got := decodeJSON[apiSrvNets](t, e.App, e.C.Get("/v1/servers/"+itoa(db.ServerID)), http.StatusOK)
	require.NotNil(t, got.PrivateNet)
	require.Empty(t, got.PrivateNet)
	att := decodeJSON[netActionResp](t, e.App, e.C.Send(http.MethodPost, "/v1/servers/"+itoa(db.ServerID)+"/actions/attach_to_network",
		map[string]string{"network": "backend"}), http.StatusCreated)
	require.Equal(t, "attach_to_network", att.Action.Command)
	require.NotNil(t, att.GuestConfig)
	require.Contains(t, *att.GuestConfig.Apply, "ip addr add 10.1.0.3/16")
	require.NotNil(t, att.GuestConfig.Persist)
	require.Nil(t, att.GuestConfig.Note)
	settleNIC(t, e, att.Action, db.ServerID, servernetwork.StateAttached)

	got = decodeJSON[apiSrvNets](t, e.App, e.C.Get("/v1/servers/"+itoa(db.ServerID)), http.StatusOK)
	require.Len(t, got.PrivateNet, 1)
	require.Equal(t, "10.1.0.3", got.PrivateNet[0].IP)
	require.NotNil(t, got.PrivateNet[0].GuestConfig, "a running server can show its commands again")
	require.Contains(t, *got.PrivateNet[0].GuestConfig.Apply, "10.1.0.3/16")

	nn := decodeJSON[apiNet](t, e.App, e.C.Get(np(n.ID)), http.StatusOK)
	require.Len(t, nn.Servers, 2)
	require.Equal(t, "web", nn.Servers[0].Name)
	require.Equal(t, "10.1.0.2", nn.Servers[0].IP)
	require.Equal(t, db.ServerID, nn.Servers[1].Server)

	acts := decodeJSON[struct {
		Actions []apiAct `json:"actions"`
	}](t, e.App, e.C.Get(np(n.ID)+"/actions"), http.StatusOK)
	require.NotEmpty(t, acts.Actions)

	res := e.C.Send(http.MethodDelete, np(n.ID), nil)
	require.Equal(t, http.StatusConflict, res.StatusCode)
	require.Contains(t, e.App.Body(res), "Detach all servers from this network first")

	det := decodeJSON[netActionResp](t, e.App, e.C.Send(http.MethodPost, "/v1/servers/"+itoa(db.ServerID)+"/actions/detach_from_network",
		map[string]string{"network": "backend"}), http.StatusCreated)
	require.Equal(t, "detach_from_network", det.Action.Command)
	require.NotNil(t, det.GuestConfig)
	require.Contains(t, *det.GuestConfig.Remove, "netplan")
	require.Nil(t, det.GuestConfig.Apply)
	settleNIC(t, e, det.Action, db.ServerID, "")

	res = e.C.Send(http.MethodPost, "/v1/servers/"+itoa(db.ServerID)+"/actions/detach_from_network", map[string]string{"network": "backend"})
	require.Equal(t, http.StatusUnprocessableEntity, res.StatusCode)
	require.Contains(t, e.App.Body(res), "The server isn't on this network.")

	scratch := createNet(t, e, e.C, map[string]any{"name": "scratch", "location": "eu-1", "ip_range": "192.168.5.7/24"})
	require.Equal(t, "192.168.5.0/24", scratch.IPRange)
	res = e.C.Send(http.MethodDelete, np(scratch.ID), nil)
	require.Equal(t, http.StatusNoContent, res.StatusCode)
	res.Body.Close()
	res = e.C.Get(np(scratch.ID))
	require.Equal(t, http.StatusNotFound, res.StatusCode)
	res.Body.Close()
}

func TestNetworkErrors(t *testing.T) {
	e := newNetEnv(t)
	web := e.RunningServer("web")
	createNet(t, e, e.C, map[string]any{"name": "a", "location": "eu-1"})
	res := e.C.Send(http.MethodPost, "/v1/networks", map[string]any{"name": "b", "location": "eu-1", "ip_range": "8.8.8.0/24"})
	require.Equal(t, http.StatusUnprocessableEntity, res.StatusCode)
	res.Body.Close()
	res = e.C.Send(http.MethodPost, "/v1/servers/"+itoa(web.ServerID)+"/actions/attach_to_network", map[string]string{"network": "a", "ip": "10.1.0.1"})
	require.Equal(t, http.StatusUnprocessableEntity, res.StatusCode)
	require.Contains(t, e.App.Body(res), "That address isn't usable on this network.")
}

func TestStoppedServerAttachNote(t *testing.T) {
	e := newNetEnv(t)
	created := e.RunningServer("web")
	require.NoError(t, e.App.Ent.Server.UpdateOneID(created.ServerID).SetStatus(server.StatusOff).Exec(context.Background()))
	createNet(t, e, e.C, map[string]any{"name": "a", "location": "eu-1"})
	att := decodeJSON[netActionResp](t, e.App, e.C.Send(http.MethodPost, "/v1/servers/"+itoa(created.ServerID)+"/actions/attach_to_network",
		map[string]string{"network": "a"}), http.StatusCreated)
	require.NotNil(t, att.GuestConfig)
	require.Equal(t, "Run these after starting the server.", *att.GuestConfig.Note)
}

func TestReadOnlyTokenCannotWriteNetworks(t *testing.T) {
	e := newNetEnv(t)
	e.RunningServer("web")
	n := createNet(t, e, e.C, map[string]any{"name": "a", "location": "eu-1"})
	np := "/v1/networks/" + itoa(n.ID)
	ro := e.App.TokenClient(e.Admin.CreateToken(e.Project, "read"))

	for _, path := range []string{"/v1/networks", np, np + "/actions"} {
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
		{http.MethodPost, "/v1/networks", map[string]any{"name": "b", "location": "eu-1"}},
		{http.MethodPut, np, map[string]any{"name": "y"}},
		{http.MethodDelete, np, nil},
		{http.MethodPost, "/v1/servers/1/actions/attach_to_network", map[string]string{"network": "a"}},
		{http.MethodPost, "/v1/servers/1/actions/detach_from_network", map[string]string{"network": "a"}},
	} {
		res := ro.Send(r.method, r.path, r.body)
		require.Equal(t, http.StatusForbidden, res.StatusCode, "%s %s", r.method, r.path)
		require.Equal(t, "forbidden", errCode(t, e.App, res))
	}
	after, err := e.App.Ent.Action.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, before, after)
	require.Equal(t, 1, e.App.Ent.Network.Query().CountX(ctx))
	require.Equal(t, "a", decodeJSON[apiNet](t, e.App, ro.Get(np), http.StatusOK).Name)
}

func TestMembersNeverSeeVNet(t *testing.T) {
	e := newNetEnv(t)
	n := createNet(t, e, e.C, map[string]any{"name": "backend", "location": "eu-1"})
	e.CreateServer("web", map[string]any{"networks": []map[string]string{{"network": "backend"}}})
	db := e.RunningServer("db")
	att := e.C.Send(http.MethodPost, "/v1/servers/"+itoa(db.ServerID)+"/actions/attach_to_network", map[string]string{"network": "backend"})
	require.Equal(t, http.StatusCreated, att.StatusCode)
	paths := []string{"/v1/networks", "/v1/networks/" + itoa(n.ID), "/v1/servers", "/v1/servers/" + itoa(db.ServerID),
		"/v1/locations", "/v1/locations/" + itoa(e.Location), "/v1/networks/" + itoa(n.ID) + "/actions"}
	bodies := []string{e.App.Body(att)}
	for _, p := range paths {
		bodies = append(bodies, e.App.Body(e.C.Get(p)))
	}
	for i, b := range bodies {
		for _, word := range []string{"nb0001", "nb0002", "nb0003", "vnet", "zone", "bridge"} {
			require.NotContains(t, strings.ToLower(b), word, "response %d", i)
		}
	}
}

func TestLocationPrivateNetworksAvailable(t *testing.T) {
	e := testutil.NewServerEnv(testutil.NewApp(t, testutil.QueueOnlyJobs(t)))
	type loc struct {
		Available bool `json:"private_networks_available"`
	}
	require.False(t, decodeJSON[loc](t, e.App, e.C.Get("/v1/locations/"+itoa(e.Location)), http.StatusOK).Available)
	e = newNetEnv(t)
	require.True(t, decodeJSON[loc](t, e.App, e.C.Get("/v1/locations/"+itoa(e.Location)), http.StatusOK).Available)
	all := decodeJSON[struct {
		Locations []loc `json:"locations"`
	}](t, e.App, e.C.Get("/v1/locations"), http.StatusOK)
	require.True(t, all.Locations[0].Available)
}

func TestAdminLocationPrivateNetworks(t *testing.T) {
	e := testutil.NewServerEnv(testutil.NewApp(t, testutil.QueueOnlyJobs(t)))
	e.App.PVE.AddSDN(pve.SDNZone{Zone: "nebla", Type: "vxlan"}, pve.SDNVNet{VNet: "nb0001", Zone: "nebla"}, pve.SDNVNet{VNet: "nb0002", Zone: "nebla"})
	e.App.PVE.SetSDNSubnets("nb0002", "10.9.0.0/24")
	ctx := context.Background()
	path := "/v1/admin/locations/" + itoa(e.Location)

	off := decodeJSON[apiAdminLoc](t, e.App, e.Admin.Get(path), http.StatusOK)
	require.Nil(t, off.SdnZone)
	require.Equal(t, "nb", off.VnetPrefix)
	require.Equal(t, "not_set_up", off.PrivateNetworks.State)
	require.Nil(t, off.PrivateNetworks.Zone)
	require.NotNil(t, off.PrivateNetworks.Unusable)

	// A plain edit leaves the SDN settings alone.
	same := decodeJSON[apiAdminLoc](t, e.App, e.Admin.Send(http.MethodPut, path, adminLocationBody(e, nil)), http.StatusOK)
	require.Nil(t, same.SdnZone)

	on := setZone(t, e, "nebla")
	require.Equal(t, "nebla", *on.SdnZone)
	require.Equal(t, "firewall_off", on.PrivateNetworks.State)
	require.Equal(t, "nebla", *on.PrivateNetworks.Zone)
	require.Equal(t, 1, on.PrivateNetworks.Available)
	require.Len(t, on.PrivateNetworks.Unusable, 1)
	require.Equal(t, "nb0002", on.PrivateNetworks.Unusable[0].VNet)
	require.NotEmpty(t, on.PrivateNetworks.Unusable[0].Reason)

	require.NoError(t, e.App.Ent.Location.UpdateOneID(e.Location).SetFirewallEnabled(true).Exec(ctx))
	keep := decodeJSON[apiAdminLoc](t, e.App, e.Admin.Send(http.MethodPut, path, adminLocationBody(e, map[string]any{"vnet_prefix": "nb"})), http.StatusOK)
	require.Equal(t, "ready", keep.PrivateNetworks.State)
	listed := decodeJSON[struct {
		Locations []apiAdminLoc `json:"locations"`
	}](t, e.App, e.Admin.Get("/v1/admin/locations"), http.StatusOK)
	require.Equal(t, "ready", listed.Locations[0].PrivateNetworks.State)

	// With a network in use the zone and prefix are fixed.
	createNet(t, e, e.C, map[string]any{"name": "a", "location": "eu-1"})
	res := e.Admin.Send(http.MethodPut, path, adminLocationBody(e, map[string]any{"vnet_prefix": "xx"}))
	require.Equal(t, http.StatusConflict, res.StatusCode)
	require.Contains(t, e.App.Body(res), "This location's private networks are in use; the zone and prefix can't change.")

	res = e.Admin.Send(http.MethodPut, path, adminLocationBody(e, map[string]any{"vnet_prefix": "BAD!"}))
	require.Equal(t, http.StatusUnprocessableEntity, res.StatusCode)
	res.Body.Close()
}

func TestNetworkQuotaAPI(t *testing.T) {
	e := newNetEnv(t)
	pp := "/v1/admin/projects/" + itoa(e.Project)
	body := map[string]int{"max_servers": 5, "max_vcpu": 5, "max_memory_mb": 5, "max_disk_gb": 5, "max_ips": 5, "max_networks": 1}
	q := decodeJSON[struct {
		MaxNetworks int `json:"max_networks"`
	}](t, e.App, e.Admin.Send(http.MethodPut, pp+"/quota", body), http.StatusOK)
	require.Equal(t, 1, q.MaxNetworks)
	createNet(t, e, e.C, map[string]any{"name": "a", "location": "eu-1"})
	res := e.C.Send(http.MethodPost, "/v1/networks", map[string]any{"name": "b", "location": "eu-1"})
	require.Equal(t, http.StatusUnprocessableEntity, res.StatusCode, e.App.Body(res))

	delete(body, "max_networks")
	q = decodeJSON[struct {
		MaxNetworks int `json:"max_networks"`
	}](t, e.App, e.Admin.Send(http.MethodPut, pp+"/quota", body), http.StatusOK)
	require.Equal(t, 1, q.MaxNetworks, "an omitted max_networks keeps its value")
	p := decodeJSON[struct {
		Usage struct {
			Networks int `json:"networks"`
		} `json:"usage"`
	}](t, e.App, e.Admin.Get(pp), http.StatusOK)
	require.Equal(t, 1, p.Usage.Networks)
}

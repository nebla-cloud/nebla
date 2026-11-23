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
	"net/url"
	"testing"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/server"
	"github.com/nebla-cloud/nebla/internal/ent/snapshot"
	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/stretchr/testify/require"
)

const apiSnapVMID = 9100

type apiSnap struct {
	ID                   int64             `json:"id"`
	Name                 string            `json:"name"`
	Description          string            `json:"description"`
	Labels               map[string]string `json:"labels"`
	Size                 int               `json:"size"`
	Server               *int64            `json:"server"`
	ServerName           string            `json:"server_name"`
	OSFlavor             string            `json:"os_flavor"`
	OSVersion            string            `json:"os_version"`
	Status               string            `json:"status"`
	RestoreKeepsIdentity *bool             `json:"restore_keeps_identity"`
	Location             struct {
		Name string `json:"name"`
	} `json:"location"`
}

func newSnapEnv(t *testing.T) *testutil.ServerEnv {
	t.Helper()
	return testutil.NewServerEnv(testutil.NewApp(t, testutil.QueueOnlyJobs(t)))
}

// addAPISnapshot inserts an available snapshot on shared storage whose template exists on pve1.
func addAPISnapshot(t *testing.T, e *testutil.ServerEnv, name string, sizeGB int, mod ...func(*ent.SnapshotCreate)) *ent.Snapshot {
	t.Helper()
	e.App.PVE.AddTemplate("pve1", apiSnapVMID, "ceph")
	c := e.App.Ent.Snapshot.Create().SetProjectID(e.Project).SetLocationID(e.Location).SetName(name).SetLabels(map[string]string{}).
		SetSizeGB(sizeGB).SetVmid(apiSnapVMID).SetNode("").SetStatus(snapshot.StatusAvailable).SetImageID(e.Image).
		SetOsFlavor("ubuntu").SetOsVersion("24.04")
	for _, m := range mod {
		m(c)
	}
	return c.SaveX(context.Background())
}

func TestSnapshotsCRUD(t *testing.T) {
	e := newSnapEnv(t)
	web := e.RunningServer("web1")
	raw := decodeJSON[map[string]any](t, e.App, e.C.Send(http.MethodPost, "/v1/snapshots", map[string]any{
		"server": "web1", "name": "before", "description": "d", "labels": map[string]string{"env": "prod"}}), http.StatusCreated)
	snapRaw := raw["snapshot"].(map[string]any)
	for _, k := range []string{"vmid", "node", "storage", "template"} {
		require.NotContains(t, snapRaw, k)
	}
	s := decodeJSON[apiSnap](t, e.App, e.C.Get("/v1/snapshots/"+itoa(int64(snapRaw["id"].(float64)))), http.StatusOK)
	require.Equal(t, "before", s.Name)
	require.Equal(t, "d", s.Description)
	require.Equal(t, map[string]string{"env": "prod"}, s.Labels)
	require.Equal(t, "creating", s.Status)
	require.Equal(t, web.ServerID, *s.Server)
	require.Equal(t, "web1", s.ServerName)
	require.Equal(t, "eu-1", s.Location.Name)
	require.NotNil(t, s.RestoreKeepsIdentity)
	require.Equal(t, "create_snapshot", raw["action"].(map[string]any)["command"])

	e.App.Ent.Snapshot.UpdateOneID(s.ID).SetStatus(snapshot.StatusAvailable).SetVmid(apiSnapVMID).ExecX(context.Background())
	list := func(q string) []string {
		out := decodeJSON[struct {
			Snapshots []apiSnap `json:"snapshots"`
			Meta      struct {
				Pagination struct {
					TotalEntries int `json:"total_entries"`
				} `json:"pagination"`
			} `json:"meta"`
		}](t, e.App, e.C.Get("/v1/snapshots?"+q), http.StatusOK)
		names := []string{}
		for _, x := range out.Snapshots {
			names = append(names, x.Name)
		}
		require.Equal(t, len(names), out.Meta.Pagination.TotalEntries)
		return names
	}
	require.Equal(t, []string{"before"}, list(""))
	require.Equal(t, []string{"before"}, list("server=web1&location=eu-1&status=available&name=before"))
	require.Equal(t, []string{"before"}, list("label_selector="+url.QueryEscape("env=prod")))
	require.Empty(t, list("server=nope"))
	require.Empty(t, list("name=other"))
	res := e.C.Get("/v1/snapshots?status=bogus")
	require.Equal(t, http.StatusUnprocessableEntity, res.StatusCode, e.App.Body(res))

	acts := decodeJSON[struct {
		Actions []apiAct `json:"actions"`
	}](t, e.App, e.C.Get("/v1/snapshots/"+itoa(s.ID)+"/actions"), http.StatusOK)
	require.Len(t, acts.Actions, 1)
	require.Equal(t, "create_snapshot", acts.Actions[0].Command)
	require.NoError(t, actions.Succeed(context.Background(), e.App.Ent, acts.Actions[0].ID))

	up := decodeJSON[apiSnap](t, e.App, e.C.Send(http.MethodPut, "/v1/snapshots/"+itoa(s.ID),
		map[string]any{"name": "renamed", "description": "new", "labels": map[string]string{}}), http.StatusOK)
	require.Equal(t, "renamed", up.Name)
	require.Equal(t, "new", up.Description)
	require.Empty(t, up.Labels)

	del := decodeJSON[struct {
		Action apiAct `json:"action"`
	}](t, e.App, e.C.Send(http.MethodDelete, "/v1/snapshots/"+itoa(s.ID), nil), http.StatusOK)
	require.Equal(t, "delete_snapshot", del.Action.Command)
	res = e.C.Get("/v1/snapshots/99999")
	require.Equal(t, http.StatusNotFound, res.StatusCode)
}

func TestSnapshotsReadOnlyToken(t *testing.T) {
	e := newSnapEnv(t)
	e.RunningServer("web1")
	tok := e.App.TokenClient(e.C.CreateToken(e.Project, "read"))
	res := tok.Get("/v1/snapshots")
	require.Equal(t, http.StatusOK, res.StatusCode, e.App.Body(res))
	res = tok.Send(http.MethodPost, "/v1/snapshots", map[string]any{"server": "web1"})
	require.Equal(t, http.StatusForbidden, res.StatusCode, e.App.Body(res))
}

func TestCreateServerWithSnapshot(t *testing.T) {
	e := newSnapEnv(t)
	snap := addAPISnapshot(t, e, "gold", 10)
	body := map[string]any{"name": "from-snap", "server_type": "small", "snapshot": "gold", "location": "eu-1"}
	out := decodeJSON[struct {
		Server struct {
			Snapshot *struct {
				ID   int64
				Name string
			} `json:"snapshot"`
			OSFlavor  string `json:"os_flavor"`
			OSVersion string `json:"os_version"`
		} `json:"server"`
	}](t, e.App, e.C.Send(http.MethodPost, "/v1/servers", body), http.StatusCreated)
	require.NotNil(t, out.Server.Snapshot)
	require.Equal(t, snap.ID, out.Server.Snapshot.ID)
	require.Equal(t, "gold", out.Server.Snapshot.Name)
	require.Equal(t, "ubuntu", out.Server.OSFlavor)
	require.Equal(t, "24.04", out.Server.OSVersion)

	body["name"] = "both"
	body["image"] = "ubuntu"
	res := e.C.Send(http.MethodPost, "/v1/servers", body)
	require.Equal(t, http.StatusUnprocessableEntity, res.StatusCode)
	require.Equal(t, "invalid_input", errCode(t, e.App, res))
}

func TestRebuildWithSnapshot(t *testing.T) {
	e := newSnapEnv(t)
	web := e.RunningServer("web1")
	addAPISnapshot(t, e, "gold", 10)
	res := e.C.Send(http.MethodPost, "/v1/servers/"+itoa(web.ServerID)+"/actions/rebuild", map[string]any{"snapshot": "gold"})
	require.Equal(t, http.StatusCreated, res.StatusCode, e.App.Body(res))
	res.Body.Close()
	require.Equal(t, server.StatusRebuilding, e.App.Ent.Server.GetX(context.Background(), web.ServerID).Status)
}

func TestSnapshotErrorCodes(t *testing.T) {
	e := newSnapEnv(t)
	web := e.RunningServer("web1")
	rebuild := func(snap string) *http.Response {
		return e.C.Send(http.MethodPost, "/v1/servers/"+itoa(web.ServerID)+"/actions/rebuild", map[string]any{"snapshot": snap})
	}
	requireCode := func(res *http.Response, status int, code string) {
		t.Helper()
		require.Equal(t, status, res.StatusCode)
		require.Equal(t, code, errCode(t, e.App, res))
	}

	addAPISnapshot(t, e, "big", 30)
	requireCode(rebuild("big"), http.StatusUnprocessableEntity, "snapshot_too_large")
	addAPISnapshot(t, e, "pinned", 10, func(c *ent.SnapshotCreate) { c.SetNode("pve2").SetVmid(apiSnapVMID + 1) })
	requireCode(rebuild("pinned"), http.StatusUnprocessableEntity, "snapshot_server_mismatch")

	gold := addAPISnapshot(t, e, "gold", 10, func(c *ent.SnapshotCreate) { c.SetVmid(apiSnapVMID + 2) })
	res := e.C.Send(http.MethodPost, "/v1/servers", map[string]any{"name": "from-snap", "server_type": "small", "snapshot": "gold", "location": "eu-1"})
	require.Equal(t, http.StatusCreated, res.StatusCode, e.App.Body(res))
	res.Body.Close()
	requireCode(e.C.Send(http.MethodDelete, "/v1/snapshots/"+itoa(gold.ID), nil), http.StatusConflict, "snapshot_in_use")

	for _, n := range []string{"pve1", "pve2"} {
		e.App.PVE.AddStorage(n, "ceph", []string{"iso"}, true)
	}
	requireCode(e.C.Send(http.MethodPost, "/v1/snapshots", map[string]any{"server": "web1", "name": "x"}), http.StatusUnprocessableEntity, "snapshots_unavailable")
}

func TestServerJSONImageNull(t *testing.T) {
	e := newSnapEnv(t)
	web := e.RunningServer("web1")
	e.App.Ent.Server.UpdateOneID(web.ServerID).ClearImageID().SetOsFlavor("debian").SetOsVersion("13").ExecX(context.Background())
	got := decodeJSON[map[string]any](t, e.App, e.C.Get("/v1/servers/"+itoa(web.ServerID)), http.StatusOK)
	require.Contains(t, got, "image")
	require.Nil(t, got["image"])
	require.Equal(t, "debian", got["os_flavor"])
	require.Equal(t, "13", got["os_version"])
	require.Contains(t, got, "snapshot")
	require.Nil(t, got["snapshot"])
}

func TestLocationSnapshotsAvailable(t *testing.T) {
	e := newSnapEnv(t)
	avail := func() bool {
		t.Helper()
		out := decodeJSON[struct {
			Locations []struct {
				SnapshotsAvailable bool `json:"snapshots_available"`
			} `json:"locations"`
		}](t, e.App, e.C.Get("/v1/locations"), http.StatusOK)
		one := decodeJSON[struct {
			SnapshotsAvailable bool `json:"snapshots_available"`
		}](t, e.App, e.C.Get("/v1/locations/"+itoa(e.Location)), http.StatusOK)
		require.Equal(t, out.Locations[0].SnapshotsAvailable, one.SnapshotsAvailable)
		return one.SnapshotsAvailable
	}
	require.True(t, avail())
	e.App.PVE.SetDown(true)
	require.False(t, avail(), "an unreachable Proxmox hides snapshots without failing the read")
	e.App.PVE.SetDown(false)
	require.True(t, avail())
}

func TestAdminLocationSnapshotStorage(t *testing.T) {
	app := testutil.NewApp(t)
	app.SeedPVE()
	admin := app.LoginAdmin()
	conn := app.PVE.Conn()
	body := func(snap *string) map[string]any {
		b := map[string]any{"name": "eu-1", "disk_storage": "ceph", "iso_storage": "local", "pve": map[string]any{
			"url": conn.BaseURL, "token_id": conn.TokenID, "token_secret": conn.Secret, "ca_fingerprint": conn.CAFingerprint}}
		if snap != nil {
			b["snapshot_storage"] = *snap
		}
		return b
	}
	type loc struct {
		ID              int64  `json:"id"`
		SnapshotStorage string `json:"snapshot_storage"`
	}
	res := admin.Send(http.MethodPost, "/v1/admin/locations", body(new("local")))
	require.Equal(t, http.StatusUnprocessableEntity, res.StatusCode)
	require.Equal(t, "invalid_input", errCode(t, app, res))
	l := decodeJSON[loc](t, app, admin.Send(http.MethodPost, "/v1/admin/locations", body(new("local-lvm"))), http.StatusCreated)
	require.Equal(t, "local-lvm", l.SnapshotStorage)
	// Omitted on update keeps it.
	l = decodeJSON[loc](t, app, admin.Send(http.MethodPut, "/v1/admin/locations/"+itoa(l.ID), body(nil)), http.StatusOK)
	require.Equal(t, "local-lvm", l.SnapshotStorage)
	l = decodeJSON[loc](t, app, admin.Send(http.MethodPut, "/v1/admin/locations/"+itoa(l.ID), body(new(""))), http.StatusOK)
	require.Empty(t, l.SnapshotStorage)
}

func TestAdminQuotaSnapshots(t *testing.T) {
	e := newSnapEnv(t)
	q := map[string]any{"max_servers": 5, "max_vcpu": 10, "max_memory_mb": 8192, "max_disk_gb": 100, "max_ips": 5, "max_snapshots": 3, "max_snapshot_gb": 50}
	got := decodeJSON[map[string]any](t, e.App, e.Admin.Send(http.MethodPut, "/v1/admin/projects/"+itoa(e.Project)+"/quota", q), http.StatusOK)
	require.EqualValues(t, 3, got["max_snapshots"])
	require.EqualValues(t, 50, got["max_snapshot_gb"])
	p := decodeJSON[struct {
		Quota map[string]any `json:"quota"`
		Usage map[string]any `json:"usage"`
	}](t, e.App, e.C.Get("/v1/projects/"+itoa(e.Project)), http.StatusOK)
	require.EqualValues(t, 3, p.Quota["max_snapshots"])
	require.EqualValues(t, 50, p.Quota["max_snapshot_gb"])
	require.EqualValues(t, 0, p.Usage["snapshots"])
	require.EqualValues(t, 0, p.Usage["snapshot_gb"])
}

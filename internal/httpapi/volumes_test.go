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
	"net/url"
	"slices"
	"testing"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/ent/volume"
	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/stretchr/testify/require"
)

type apiVol struct {
	ID          int64             `json:"id"`
	Name        string            `json:"name"`
	Labels      map[string]string `json:"labels"`
	Size        int               `json:"size"`
	Server      *int64            `json:"server"`
	Status      string            `json:"status"`
	LinuxDevice string            `json:"linux_device"`
	Location    struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	} `json:"location"`
}

type apiAct struct {
	ID        int64  `json:"id"`
	Command   string `json:"command"`
	Status    string `json:"status"`
	Resources []struct {
		ID   int64  `json:"id"`
		Type string `json:"type"`
	} `json:"resources"`
}

type volEnv struct {
	*testutil.ServerEnv
	t *testing.T
}

// newVolEnv is a server env whose location offers volumes on shared ceph; jobs are only queued.
func newVolEnv(t *testing.T) *volEnv {
	t.Helper()
	e := testutil.NewServerEnv(testutil.NewApp(t, testutil.QueueOnlyJobs(t)))
	require.NoError(t, e.App.Ent.Location.UpdateOneID(e.Location).SetVolumeStorageOk(true).Exec(context.Background()))
	return &volEnv{ServerEnv: e, t: t}
}

func (e *volEnv) createVolume(c *testutil.Client, body map[string]any) (apiVol, apiAct) {
	e.t.Helper()
	out := decodeJSON[struct {
		Volume apiVol `json:"volume"`
		Action apiAct `json:"action"`
	}](e.t, e.App, c.Send(http.MethodPost, "/v1/volumes", body), http.StatusCreated)
	return out.Volume, out.Action
}

// settle finishes action a by hand and puts the volume in status st on serverID.
func (e *volEnv) settle(a apiAct, volumeID int64, st volume.Status, serverID *int64, device string) {
	e.t.Helper()
	ctx := context.Background()
	require.NoError(e.t, actions.Succeed(ctx, e.App.Ent, a.ID))
	u := e.App.Ent.Volume.UpdateOneID(volumeID).SetStatus(st).SetDevice(device)
	if serverID != nil {
		u.SetServerID(*serverID)
	} else {
		u.ClearServerID()
	}
	require.NoError(e.t, u.Exec(ctx))
}

func (e *volEnv) action(c *testutil.Client, method, path string, body any, want int) apiAct {
	e.t.Helper()
	return decodeJSON[struct {
		Action apiAct `json:"action"`
	}](e.t, e.App, c.Send(method, path, body), want).Action
}

func (e *volEnv) list(c *testutil.Client, query string) []string {
	e.t.Helper()
	out := decodeJSON[struct {
		Volumes []apiVol `json:"volumes"`
		Meta    struct {
			Pagination struct {
				TotalEntries int `json:"total_entries"`
			} `json:"pagination"`
		} `json:"meta"`
	}](e.t, e.App, c.Get("/v1/volumes?"+query), http.StatusOK)
	names := make([]string, len(out.Volumes))
	for i, v := range out.Volumes {
		names[i] = v.Name
	}
	require.Equal(e.t, len(names), out.Meta.Pagination.TotalEntries)
	return names
}

func TestVolumesAPI(t *testing.T) {
	e := newVolEnv(t)
	web := e.RunningServer("web")
	vp := func(id int64) string { return "/v1/volumes/" + itoa(id) }

	data, a := e.createVolume(e.C, map[string]any{"name": "data", "size": 10, "location": "eu-1", "labels": map[string]string{"env": "prod"}})
	require.Equal(t, "creating", data.Status)
	require.Equal(t, 10, data.Size)
	require.Equal(t, "eu-1", data.Location.Name)
	require.Nil(t, data.Server)
	require.Equal(t, "create_volume", a.Command)
	require.Equal(t, "running", a.Status)
	e.settle(a, data.ID, volume.StatusAvailable, nil, "")

	logs, a := e.createVolume(e.C, map[string]any{"name": "logs", "size": 20, "server": "web"})
	require.Equal(t, "eu-1", logs.Location.Name)
	e.settle(a, logs.ID, volume.StatusAttached, &web.ServerID, "virtio1")

	require.Equal(t, []string{"data", "logs"}, e.list(e.C, ""))
	require.Equal(t, []string{"logs"}, e.list(e.C, "server=web"))
	require.Equal(t, []string{"logs"}, e.list(e.C, "server="+itoa(web.ServerID)))
	require.Empty(t, e.list(e.C, "server=nope"))
	require.Equal(t, []string{"data"}, e.list(e.C, "label_selector="+url.QueryEscape("env=prod")))
	require.Equal(t, []string{"logs"}, e.list(e.C, "label_selector="+url.QueryEscape("!env")))
	require.Equal(t, []string{"data"}, e.list(e.C, "status=available"))
	require.Equal(t, []string{"logs"}, e.list(e.C, "name=logs"))
	require.Equal(t, []string{"data", "logs"}, e.list(e.C, "location=eu-1"))
	res := e.C.Get("/v1/volumes?status=bogus")
	require.Equal(t, http.StatusUnprocessableEntity, res.StatusCode, e.App.Body(res))

	// Describe shows the stable device path.
	got := decodeJSON[apiVol](t, e.App, e.C.Get(vp(logs.ID)), http.StatusOK)
	require.Equal(t, "/dev/disk/by-id/virtio-nb-vol-"+itoa(logs.ID), got.LinuxDevice)
	require.Equal(t, web.ServerID, *got.Server)
	require.Equal(t, "attached", got.Status)

	// The server lists its volumes in get and list.
	srv := decodeJSON[struct {
		Volumes []int64 `json:"volumes"`
	}](t, e.App, e.C.Get("/v1/servers/"+itoa(web.ServerID)), http.StatusOK)
	require.Equal(t, []int64{logs.ID}, srv.Volumes)

	a = e.action(e.C, http.MethodPost, vp(data.ID)+"/actions/attach", map[string]string{"server": "web"}, http.StatusCreated)
	require.Equal(t, "attach_volume", a.Command)
	types := []string{}
	for _, r := range a.Resources {
		types = append(types, r.Type)
	}
	require.Equal(t, []string{"server", "volume"}, types)
	e.settle(a, data.ID, volume.StatusAttached, &web.ServerID, "virtio2")

	servers := decodeJSON[struct {
		Servers []struct {
			ID      int64   `json:"id"`
			Volumes []int64 `json:"volumes"`
		} `json:"servers"`
	}](t, e.App, e.C.Get("/v1/servers"), http.StatusOK)
	require.Len(t, servers.Servers, 1)
	require.Equal(t, []int64{data.ID, logs.ID}, servers.Servers[0].Volumes)

	res = e.C.Send(http.MethodDelete, vp(data.ID), nil)
	require.Equal(t, http.StatusConflict, res.StatusCode, e.App.Body(res))

	a = e.action(e.C, http.MethodPost, vp(data.ID)+"/actions/detach", nil, http.StatusCreated)
	require.Equal(t, "detach_volume", a.Command)
	e.settle(a, data.ID, volume.StatusAvailable, nil, "")

	a = e.action(e.C, http.MethodPost, vp(data.ID)+"/actions/resize", map[string]int{"size": 30}, http.StatusCreated)
	require.Equal(t, "resize_volume", a.Command)
	require.NoError(t, actions.Succeed(context.Background(), e.App.Ent, a.ID))
	require.Equal(t, 30, decodeJSON[apiVol](t, e.App, e.C.Get(vp(data.ID)), http.StatusOK).Size)

	res = e.C.Send(http.MethodPost, vp(data.ID)+"/actions/resize", map[string]int{"size": 10})
	require.Equal(t, http.StatusUnprocessableEntity, res.StatusCode, e.App.Body(res))

	renamed := decodeJSON[apiVol](t, e.App, e.C.Send(http.MethodPut, vp(data.ID), map[string]any{"name": "data2", "labels": map[string]string{"a": "b"}}), http.StatusOK)
	require.Equal(t, "data2", renamed.Name)
	require.Equal(t, map[string]string{"a": "b"}, renamed.Labels)

	acts := decodeJSON[struct {
		Actions []apiAct `json:"actions"`
	}](t, e.App, e.C.Get(vp(data.ID)+"/actions?sort=id:asc"), http.StatusOK)
	cmds := []string{}
	for _, x := range acts.Actions {
		cmds = append(cmds, x.Command)
	}
	require.Equal(t, []string{"create_volume", "attach_volume", "detach_volume", "resize_volume"}, cmds)

	a = e.action(e.C, http.MethodDelete, vp(data.ID), nil, http.StatusOK)
	require.Equal(t, "delete_volume", a.Command)

	// Usage counts volumes and their GB.
	proj := decodeJSON[struct {
		Usage map[string]int `json:"usage"`
	}](t, e.App, e.Admin.Get("/v1/admin/projects/"+itoa(e.Project)), http.StatusOK)
	require.Equal(t, 2, proj.Usage["volumes"])
	require.Equal(t, 50, proj.Usage["volume_gb"])
}

func TestVolumeJSONHasNoProxmoxFields(t *testing.T) {
	e := newVolEnv(t)
	web := e.RunningServer("web")
	v, a := e.createVolume(e.C, map[string]any{"name": "data", "size": 10, "server": "web"})
	e.settle(a, v.ID, volume.StatusAttached, &web.ServerID, "virtio1")
	require.NoError(t, e.App.Ent.Volume.UpdateOneID(v.ID).SetNode("pve1").Exec(context.Background()))

	check := func(raw map[string]json.RawMessage) {
		t.Helper()
		keys := []string{}
		for k := range raw {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		require.Equal(t, []string{"created", "id", "labels", "linux_device", "location", "name", "server", "size", "status"}, keys)
		for _, bad := range []string{"node", "storage", "device", "vmid"} {
			require.NotContains(t, raw, bad)
		}
		require.NotContains(t, string(raw["location"]), "pve1")
	}
	check(decodeJSON[map[string]json.RawMessage](t, e.App, e.C.Get("/v1/volumes/"+itoa(v.ID)), http.StatusOK))
	list := decodeJSON[struct {
		Volumes []map[string]json.RawMessage `json:"volumes"`
	}](t, e.App, e.C.Get("/v1/volumes"), http.StatusOK)
	require.Len(t, list.Volumes, 1)
	check(list.Volumes[0])
}

func TestReadOnlyTokenCannotWriteVolumes(t *testing.T) {
	e := newVolEnv(t)
	e.RunningServer("web")
	v, a := e.createVolume(e.C, map[string]any{"name": "data", "size": 10, "location": "eu-1"})
	e.settle(a, v.ID, volume.StatusAvailable, nil, "")
	ro := e.App.TokenClient(e.Admin.CreateToken(e.Project, "read"))
	vp := "/v1/volumes/" + itoa(v.ID)

	for _, path := range []string{"/v1/volumes", vp, vp + "/actions"} {
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
		{http.MethodPost, "/v1/volumes", map[string]any{"name": "x", "size": 10, "location": "eu-1"}},
		{http.MethodPut, vp, map[string]any{"name": "y"}},
		{http.MethodDelete, vp, nil},
		{http.MethodPost, vp + "/actions/attach", map[string]string{"server": "web"}},
		{http.MethodPost, vp + "/actions/detach", nil},
		{http.MethodPost, vp + "/actions/resize", map[string]int{"size": 20}},
	} {
		res := ro.Send(r.method, r.path, r.body)
		require.Equal(t, http.StatusForbidden, res.StatusCode, "%s %s", r.method, r.path)
		require.Equal(t, "forbidden", errCode(t, e.App, res))
	}
	after, err := e.App.Ent.Action.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, before, after)
	n, err := e.App.Ent.Volume.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.Equal(t, "data", decodeJSON[apiVol](t, e.App, ro.Get(vp), http.StatusOK).Name)
}

func TestLocationVolumesAvailable(t *testing.T) {
	e := testutil.NewServerEnv(testutil.NewApp(t, testutil.QueueOnlyJobs(t)))
	avail := func() bool {
		t.Helper()
		out := decodeJSON[struct {
			Locations []struct {
				VolumesAvailable bool `json:"volumes_available"`
			} `json:"locations"`
		}](t, e.App, e.C.Get("/v1/locations"), http.StatusOK)
		require.Len(t, out.Locations, 1)
		one := decodeJSON[struct {
			VolumesAvailable bool `json:"volumes_available"`
		}](t, e.App, e.C.Get("/v1/locations/"+itoa(e.Location)), http.StatusOK)
		require.Equal(t, out.Locations[0].VolumesAvailable, one.VolumesAvailable)
		return one.VolumesAvailable
	}
	require.NoError(t, e.App.Ent.Location.UpdateOneID(e.Location).SetVolumeStorageOk(false).Exec(context.Background()))
	require.False(t, avail(), "volume_storage_ok false hides volumes")
	require.NoError(t, e.App.Ent.Location.UpdateOneID(e.Location).SetVolumeStorageOk(true).Exec(context.Background()))
	require.True(t, avail())

	// Member reads never call Proxmox.
	before := len(e.App.PVE.Calls())
	e.App.PVE.SetDown(true)
	require.True(t, avail())
	require.Len(t, e.App.PVE.Calls(), before)
	e.App.PVE.SetDown(false)

	require.NoError(t, e.App.Ent.Node.Update().SetEnabled(false).Exec(context.Background()))
	require.False(t, avail(), "no enabled online node")
	require.NoError(t, e.App.Ent.Node.Update().SetEnabled(true).Exec(context.Background()))
	res := e.C.Send(http.MethodPost, "/v1/volumes", map[string]any{"name": "x", "size": 10, "location": "eu-1"})
	require.Equal(t, http.StatusCreated, res.StatusCode, e.App.Body(res))
	res.Body.Close()
}

func TestAdminLocationVolumeStorage(t *testing.T) {
	app := testutil.NewApp(t)
	app.SeedPVE()
	admin := app.LoginAdmin()
	conn := app.PVE.Conn()
	body := func(vol *string) map[string]any {
		b := map[string]any{"name": "eu-1", "disk_storage": "ceph", "iso_storage": "local", "pve": map[string]any{
			"url": conn.BaseURL, "token_id": conn.TokenID, "token_secret": conn.Secret, "ca_fingerprint": conn.CAFingerprint}}
		if vol != nil {
			b["volume_storage"] = *vol
		}
		return b
	}
	type loc struct {
		ID              int64  `json:"id"`
		VolumeStorage   string `json:"volume_storage"`
		VolumeStorageOK bool   `json:"volume_storage_ok"`
	}
	for _, bad := range []string{"nope", "local"} {
		res := admin.Send(http.MethodPost, "/v1/admin/locations", body(new(bad)))
		require.Equal(t, http.StatusUnprocessableEntity, res.StatusCode, bad)
		require.Equal(t, "invalid_input", errCode(t, app, res))
	}
	l := decodeJSON[loc](t, app, admin.Send(http.MethodPost, "/v1/admin/locations", body(new("local-lvm"))), http.StatusCreated)
	require.Equal(t, "local-lvm", l.VolumeStorage)
	require.True(t, l.VolumeStorageOK, "checked on create")
	path := "/v1/admin/locations/" + itoa(l.ID)

	res := admin.Send(http.MethodPut, path, body(new("nope")))
	require.Equal(t, http.StatusUnprocessableEntity, res.StatusCode, app.Body(res))
	res.Body.Close()
	// Omitted keeps the stored value.
	require.Equal(t, "local-lvm", decodeJSON[loc](t, app, admin.Send(http.MethodPut, path, body(nil)), http.StatusOK).VolumeStorage)
	require.Equal(t, "ceph", decodeJSON[loc](t, app, admin.Send(http.MethodPut, path, body(new("ceph"))), http.StatusOK).VolumeStorage)
	require.Empty(t, decodeJSON[loc](t, app, admin.Send(http.MethodPut, path, body(new(""))), http.StatusOK).VolumeStorage)

	// Every save re-checks Datastore.Allocate on the resolved storages.
	app.PVE.DenyAllocateOn("local-lvm")
	require.False(t, decodeJSON[loc](t, app, admin.Send(http.MethodPut, path, body(new("local-lvm"))), http.StatusOK).VolumeStorageOK)
	got := decodeJSON[map[string]any](t, app, admin.Get(path), http.StatusOK)
	require.Contains(t, got, "volume_storage")
	require.Equal(t, false, got["volume_storage_ok"])
	require.True(t, decodeJSON[loc](t, app, admin.Send(http.MethodPut, path, body(new(""))), http.StatusOK).VolumeStorageOK)
	app.PVE.SetPermissions("/storage/local-lvm", nil)
	require.True(t, decodeJSON[loc](t, app, admin.Send(http.MethodPut, path, body(new("local-lvm"))), http.StatusOK).VolumeStorageOK)
}

func TestAdminQuotaVolumes(t *testing.T) {
	app := testutil.NewApp(t)
	admin := app.LoginAdmin()
	id := admin.CreateProject("p")
	path := "/v1/admin/projects/" + itoa(id) + "/quota"
	type quota map[string]int
	q := decodeJSON[quota](t, app, admin.Send(http.MethodPut, path, quota{"max_servers": 1, "max_vcpu": 2, "max_memory_mb": 3,
		"max_disk_gb": 4, "max_ips": 5, "max_volumes": 6, "max_volume_gb": 70}), http.StatusOK)
	require.Equal(t, 6, q["max_volumes"])
	require.Equal(t, 70, q["max_volume_gb"])
	// A body without the volume fields keeps them.
	q = decodeJSON[quota](t, app, admin.Send(http.MethodPut, path, quota{"max_servers": 9, "max_vcpu": 2, "max_memory_mb": 3,
		"max_disk_gb": 4, "max_ips": 5}), http.StatusOK)
	require.Equal(t, quota{"max_servers": 9, "max_vcpu": 2, "max_memory_mb": 3, "max_disk_gb": 4, "max_ips": 5, "max_volumes": 6, "max_volume_gb": 70, "max_backups": 0, "max_networks": 0}, q)
	q = decodeJSON[quota](t, app, admin.Send(http.MethodPut, path, quota{"max_servers": 9, "max_vcpu": 2, "max_memory_mb": 3,
		"max_disk_gb": 4, "max_ips": 5, "max_volume_gb": 0}), http.StatusOK)
	require.Equal(t, 6, q["max_volumes"])
	require.Equal(t, 0, q["max_volume_gb"])
	res := admin.Send(http.MethodPut, path, quota{"max_servers": 0, "max_vcpu": 0, "max_memory_mb": 0, "max_disk_gb": 0, "max_ips": 0, "max_volumes": -1})
	require.Equal(t, http.StatusUnprocessableEntity, res.StatusCode, app.Body(res))
	res.Body.Close()

	p := decodeJSON[struct {
		Quota quota `json:"quota"`
		Usage quota `json:"usage"`
	}](t, app, admin.Get("/v1/admin/projects/"+itoa(id)), http.StatusOK)
	require.Equal(t, 6, p.Quota["max_volumes"])
	require.Contains(t, p.Usage, "volumes")
	require.Contains(t, p.Usage, "volume_gb")
}

func TestVolumesUnavailableWithoutAllocate(t *testing.T) {
	e := testutil.NewServerEnv(testutil.NewApp(t, testutil.QueueOnlyJobs(t)))
	type avail struct {
		VolumesAvailable bool `json:"volumes_available"`
	}
	type adminLoc struct {
		VolumeStorageOK bool `json:"volume_storage_ok"`
	}
	lp, ap := "/v1/locations/"+itoa(e.Location), "/v1/admin/locations/"+itoa(e.Location)
	require.True(t, decodeJSON[avail](t, e.App, e.C.Get(lp), http.StatusOK).VolumesAvailable)

	// The location's disk storage is ceph; a sync sees the missing grant.
	e.App.PVE.DenyAllocateOn("ceph")
	res := e.Admin.Send(http.MethodPost, ap+"/sync_nodes", nil)
	require.Equal(t, http.StatusOK, res.StatusCode, e.App.Body(res))
	res.Body.Close()
	require.False(t, decodeJSON[adminLoc](t, e.App, e.Admin.Get(ap), http.StatusOK).VolumeStorageOK)
	require.False(t, decodeJSON[avail](t, e.App, e.C.Get(lp), http.StatusOK).VolumesAvailable)
	res = e.C.Send(http.MethodPost, "/v1/volumes", map[string]any{"name": "x", "size": 10, "location": "eu-1"})
	require.NotEqual(t, http.StatusCreated, res.StatusCode)
	res.Body.Close()

	e.App.PVE.SetPermissions("/storage/ceph", nil)
	res = e.Admin.Send(http.MethodPost, ap+"/sync_nodes", nil)
	require.Equal(t, http.StatusOK, res.StatusCode, e.App.Body(res))
	res.Body.Close()
	require.True(t, decodeJSON[avail](t, e.App, e.C.Get(lp), http.StatusOK).VolumesAvailable)
}

func TestDeleteProjectWithVolumes(t *testing.T) {
	e := newVolEnv(t)
	ctx := context.Background()
	v, err := e.App.Ent.Volume.Create().SetProjectID(e.Project).SetLocationID(e.Location).SetName("data").
		SetLabels(map[string]string{}).SetSizeGB(10).SetStorage("ceph").SetStatus(volume.StatusAvailable).Save(ctx)
	require.NoError(t, err)

	type errBody struct {
		Error struct{ Code, Message string }
	}
	got := decodeJSON[errBody](t, e.App, e.C.Send(http.MethodDelete, "/v1/projects/"+itoa(e.Project), nil), http.StatusConflict)
	require.Equal(t, "project_not_empty", got.Error.Code)
	require.Equal(t, "Delete the project's servers, volumes, floating IPs and networks first.", got.Error.Message)
	got = decodeJSON[errBody](t, e.App, e.Admin.Send(http.MethodDelete, "/v1/admin/projects/"+itoa(e.Project), nil), http.StatusConflict)
	require.Equal(t, "project_not_empty", got.Error.Code)

	require.NoError(t, e.App.Ent.Volume.DeleteOneID(v.ID).Exec(ctx))
	res := e.Admin.Send(http.MethodDelete, "/v1/admin/projects/"+itoa(e.Project), nil)
	require.Equal(t, http.StatusNoContent, res.StatusCode, e.App.Body(res))
}

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
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/ent/backup"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/stretchr/testify/require"
)

type bkEnv struct {
	*testutil.ServerEnv
	t   *testing.T
	vol int
}

// newBkEnv is a server env whose location has usable backup storage and a project that may hold backups.
func newBkEnv(t *testing.T) *bkEnv {
	t.Helper()
	e := testutil.NewServerEnv(testutil.NewApp(t, testutil.QueueOnlyJobs(t)))
	ctx := context.Background()
	require.NoError(t, e.App.Ent.Location.UpdateOneID(e.Location).SetBackupStorage("pbs").SetBackupStorageOk(true).Exec(ctx))
	require.NoError(t, e.App.Ent.Project.UpdateOneID(e.Project).SetMaxBackups(10).Exec(ctx))
	return &bkEnv{ServerEnv: e, t: t}
}

// enabledServer is a running server with backups on, keeping 3.
func (e *bkEnv) enabledServer(name string) int64 {
	e.t.Helper()
	s := e.RunningServer(name)
	res := e.C.Send(http.MethodPut, "/v1/servers/"+itoa(s.ServerID), map[string]any{"backups": map[string]any{"enabled": true, "keep": 3}})
	require.Equal(e.t, http.StatusOK, res.StatusCode, e.App.Body(res))
	res.Body.Close()
	return s.ServerID
}

func (e *bkEnv) put(serverID int64, st backup.Status) int64 {
	e.t.Helper()
	srv := e.App.Ent.Server.GetX(context.Background(), serverID)
	c := e.App.Ent.Backup.Create().SetProjectID(e.Project).SetServerID(serverID).SetLocationID(e.Location).SetStorage("pbs").
		SetNode(srv.Node).SetKind(backup.KindScheduled).SetStatus(st).SetSizeBytes(2048).SetInstanceGeneration(servers.InstanceGeneration(srv))
	if st != backup.StatusQueued && st != backup.StatusCreating {
		e.vol++
		c.SetVolid(fmt.Sprintf("pbs:backup/vm/101/%d", e.vol))
	}
	b, err := c.Save(context.Background())
	require.NoError(e.t, err)
	return b.ID
}

// noInternals fails if any JSON key in body names storage, node, volid or the VMID.
func noInternals(t *testing.T, body string) {
	t.Helper()
	for _, k := range []string{"volid", "storage", "node", "vmid"} {
		require.NotContains(t, strings.ToLower(body), `"`+k+`"`)
	}
}

func decodeBody[T any](t *testing.T, body string) T {
	t.Helper()
	var v T
	require.NoError(t, json.Unmarshal([]byte(body), &v), body)
	return v
}

func TestBackupsRoutes(t *testing.T) {
	e := newBkEnv(t)
	id := e.enabledServer("web")
	base := "/v1/servers/" + itoa(id) + "/backups"
	type bk struct {
		ID                    int64   `json:"id"`
		Server                int64   `json:"server"`
		Kind                  string  `json:"kind"`
		Status                string  `json:"status"`
		Size                  int64   `json:"size"`
		Created               string  `json:"created"`
		Completed             *string `json:"completed"`
		RestoreResetsIdentity bool    `json:"restore_resets_identity"`
	}

	list := decodeJSON[struct {
		Backups []bk `json:"backups"`
	}](t, e.App, e.C.Get(base), http.StatusOK)
	require.Empty(t, list.Backups)
	require.NotNil(t, list.Backups)

	// Back up now: 202 with a queued manual backup, 200 with the same one while it is pending.
	res := e.C.Send(http.MethodPost, base, nil)
	body := e.App.Body(res)
	require.Equal(t, http.StatusAccepted, res.StatusCode, body)
	noInternals(t, body)
	first := decodeBody[bk](t, body)
	require.Equal(t, "queued", first.Status)
	require.Equal(t, "manual", first.Kind)
	require.Equal(t, id, first.Server)
	require.Nil(t, first.Completed)
	again := decodeJSON[bk](t, e.App, e.C.Send(http.MethodPost, base, nil), http.StatusOK)
	require.Equal(t, first.ID, again.ID)

	// An available backup: get, list filters, delete, restore.
	av := e.put(id, backup.StatusAvailable)
	res = e.C.Get(base + "/" + itoa(av))
	body = e.App.Body(res)
	require.Equal(t, http.StatusOK, res.StatusCode, body)
	noInternals(t, body)
	got := decodeBody[bk](t, body)
	require.Equal(t, "available", got.Status)
	require.Equal(t, "scheduled", got.Kind)
	require.EqualValues(t, 2048, got.Size)

	res = e.C.Get(base + "?status=available&kind=scheduled")
	body = e.App.Body(res)
	require.Equal(t, http.StatusOK, res.StatusCode, body)
	noInternals(t, body)
	require.Len(t, decodeBody[struct {
		Backups []bk `json:"backups"`
	}](t, body).Backups, 1)
	res = e.C.Get(base + "?status=queued")
	require.Len(t, decodeBody[struct {
		Backups []bk `json:"backups"`
	}](t, e.App.Body(res)).Backups, 1)
	res = e.C.Get(base + "?status=bogus")
	require.Equal(t, http.StatusUnprocessableEntity, res.StatusCode)
	res.Body.Close()

	// An idle backup deletes; the restore then runs and the backup is busy.
	idle := e.put(id, backup.StatusAvailable)
	res = e.C.Send(http.MethodDelete, base+"/"+itoa(idle), nil)
	body = e.App.Body(res)
	require.Equal(t, http.StatusOK, res.StatusCode, body)
	noInternals(t, body)
	del := decodeBody[struct {
		Action apiAct `json:"action"`
	}](t, body).Action
	require.Equal(t, "delete_backup", del.Command)
	require.NoError(t, actions.Succeed(context.Background(), e.App.Ent, del.ID))

	res = e.C.Send(http.MethodPost, "/v1/servers/"+itoa(id)+"/actions/restore_backup", map[string]any{"backup": av})
	body = e.App.Body(res)
	require.Equal(t, http.StatusCreated, res.StatusCode, body)
	noInternals(t, body)
	act := decodeBody[struct {
		Action apiAct `json:"action"`
	}](t, body).Action
	require.Equal(t, "restore_backup", act.Command)
	require.Equal(t, "restoring", e.App.Ent.Backup.GetX(context.Background(), av).Status.String())

	res = e.C.Send(http.MethodDelete, base+"/"+itoa(av), nil)
	require.Equal(t, http.StatusConflict, res.StatusCode)
	res.Body.Close()
	res = e.C.Get(base + "/999999")
	require.Equal(t, http.StatusNotFound, res.StatusCode)
	res.Body.Close()
}

func TestBackupsRoutesDisabledAndRoles(t *testing.T) {
	e := newBkEnv(t)
	s := e.RunningServer("off")
	res := e.C.Send(http.MethodPost, "/v1/servers/"+itoa(s.ServerID)+"/backups", nil)
	body := e.App.Body(res)
	require.Equal(t, http.StatusUnprocessableEntity, res.StatusCode, body)
	require.Contains(t, body, "backups_disabled")

	ro := e.App.TokenClient(e.Admin.CreateToken(e.Project, "read"))
	res = ro.Send(http.MethodPost, "/v1/servers/"+itoa(s.ServerID)+"/backups", nil)
	require.Equal(t, http.StatusForbidden, res.StatusCode)
	res.Body.Close()
}

func TestServerBackupsField(t *testing.T) {
	e := newBkEnv(t)
	type srv struct {
		Backups struct {
			Enabled      bool    `json:"enabled"`
			Keep         int     `json:"keep"`
			Count        int     `json:"count"`
			LastBackupAt *string `json:"last_backup_at"`
			NextBackupAt *string `json:"next_backup_at"`
		} `json:"backups"`
	}

	// Create with backups on.
	out := decodeJSON[struct {
		Server srv    `json:"server"`
		Action apiAct `json:"action"`
	}](t, e.App, e.C.Send(http.MethodPost, "/v1/servers", map[string]any{"name": "new", "server_type": "small", "image": "ubuntu",
		"location": "eu-1", "backups": map[string]any{"enabled": true, "keep": 5}}), http.StatusCreated)
	require.True(t, out.Server.Backups.Enabled)
	require.Equal(t, 5, out.Server.Backups.Keep)

	id := e.RunningServer("web").ServerID
	p := "/v1/servers/" + itoa(id)
	got := decodeJSON[srv](t, e.App, e.C.Get(p), http.StatusOK)
	require.False(t, got.Backups.Enabled)
	require.Nil(t, got.Backups.NextBackupAt)
	require.Nil(t, got.Backups.LastBackupAt)

	got = decodeJSON[srv](t, e.App, e.C.Send(http.MethodPut, p, map[string]any{"backups": map[string]any{"enabled": true, "keep": 4}}), http.StatusOK)
	require.True(t, got.Backups.Enabled)
	require.Equal(t, 4, got.Backups.Keep)
	require.NotNil(t, got.Backups.NextBackupAt)

	e.put(id, backup.StatusAvailable)
	e.App.Ent.Backup.Update().SetCompletedAt(e.App.Ent.Backup.Query().OnlyX(context.Background()).CreatedAt).ExecX(context.Background())
	list := decodeJSON[struct {
		Servers []srv `json:"servers"`
	}](t, e.App, e.C.Get("/v1/servers"), http.StatusOK)
	var one srv
	for _, s := range list.Servers {
		if s.Backups.Count == 1 {
			one = s
		}
	}
	require.Equal(t, 1, one.Backups.Count)
	require.NotNil(t, one.Backups.LastBackupAt)

	res := e.C.Send(http.MethodPut, p, map[string]any{"backups": map[string]any{"enabled": true, "keep": 15}})
	require.Equal(t, http.StatusUnprocessableEntity, res.StatusCode)
	res.Body.Close()

	// A location's backups_available follows its storage state.
	type loc struct {
		BackupsAvailable bool `json:"backups_available"`
	}
	require.True(t, decodeJSON[loc](t, e.App, e.C.Get("/v1/locations/"+itoa(e.Location)), http.StatusOK).BackupsAvailable)
	require.NoError(t, e.App.Ent.Location.UpdateOneID(e.Location).SetBackupStorageOk(false).Exec(context.Background()))
	require.False(t, decodeJSON[loc](t, e.App, e.C.Get("/v1/locations/"+itoa(e.Location)), http.StatusOK).BackupsAvailable)
}

func TestAdminLocationBackupFields(t *testing.T) {
	e := newBkEnv(t)
	type win struct {
		Start string `json:"start"`
		End   string `json:"end"`
	}
	type loc struct {
		BackupStorage         string `json:"backup_storage"`
		BackupStorageOK       bool   `json:"backup_storage_ok"`
		BackupWindow          win    `json:"backup_window"`
		BackupsOnOtherStorage int    `json:"backups_on_other_storage"`
	}
	ap := "/v1/admin/locations/" + itoa(e.Location)
	got := decodeJSON[loc](t, e.App, e.Admin.Get(ap), http.StatusOK)
	require.Equal(t, loc{BackupStorage: "pbs", BackupStorageOK: true, BackupWindow: win{"02:00", "06:00"}}, got)

	// Two backups on the current storage, one left behind on an old one.
	id := e.RunningServer("web").ServerID
	e.put(id, backup.StatusAvailable)
	e.put(id, backup.StatusAvailable)
	old := e.put(id, backup.StatusAvailable)
	e.App.Ent.Backup.UpdateOneID(old).SetStorage("old-pbs").ExecX(context.Background())
	require.Equal(t, 1, decodeJSON[loc](t, e.App, e.Admin.Get(ap), http.StatusOK).BackupsOnOtherStorage)

	// A save keeps the window and storage when omitted, and sets a new window.
	conn := e.App.PVE.Conn()
	body := func(w map[string]string) map[string]any {
		b := map[string]any{"name": "eu-1", "disk_storage": "ceph", "iso_storage": "local", "pve": map[string]any{
			"url": conn.BaseURL, "token_id": conn.TokenID, "token_secret": conn.Secret, "ca_fingerprint": conn.CAFingerprint}}
		if w != nil {
			b["backup_window"] = w
		}
		return b
	}
	got = decodeJSON[loc](t, e.App, e.Admin.Send(http.MethodPut, ap, body(map[string]string{"start": "22:30", "end": "01:15"})), http.StatusOK)
	require.Equal(t, win{"22:30", "01:15"}, got.BackupWindow)
	require.Equal(t, "pbs", got.BackupStorage)
	got = decodeJSON[loc](t, e.App, e.Admin.Send(http.MethodPut, ap, body(nil)), http.StatusOK)
	require.Equal(t, win{"22:30", "01:15"}, got.BackupWindow)

	// Members never see admin locations.
	e.App.CreateUser("m@example.com", false)
	m := e.App.Login("m@example.com")
	res := m.Get(ap)
	require.Equal(t, http.StatusForbidden, res.StatusCode)
	res.Body.Close()
	res = m.Send(http.MethodPut, ap, body(nil))
	require.Equal(t, http.StatusForbidden, res.StatusCode)
	res.Body.Close()

	// The member view of the location has no admin fields.
	res = e.C.Get("/v1/locations/" + itoa(e.Location))
	b := e.App.Body(res)
	for _, k := range []string{"backup_storage", "backup_window", "backups_on_other_storage"} {
		require.NotContains(t, b, k)
	}
}

func TestBackupWindowHHMM(t *testing.T) {
	e := newBkEnv(t)
	conn := e.App.PVE.Conn()
	ap := "/v1/admin/locations/" + itoa(e.Location)
	for _, w := range []map[string]string{
		{"start": "25:00", "end": "06:00"}, {"start": "02:00", "end": "6:00"}, {"start": "02:60", "end": "06:00"},
		{"start": "02:00", "end": "02:00"}, {"start": "", "end": "06:00"},
	} {
		res := e.Admin.Send(http.MethodPut, ap, map[string]any{"name": "eu-1", "disk_storage": "ceph", "iso_storage": "local",
			"pve":           map[string]any{"url": conn.BaseURL, "token_id": conn.TokenID, "token_secret": conn.Secret, "ca_fingerprint": conn.CAFingerprint},
			"backup_window": w})
		body := e.App.Body(res)
		require.Equal(t, http.StatusUnprocessableEntity, res.StatusCode, "%v: %s", w, body)
		require.Contains(t, body, "invalid_input")
	}
}

func TestQuotaMaxBackups(t *testing.T) {
	app := testutil.NewApp(t)
	admin := app.LoginAdmin()
	id := admin.CreateProject("p")
	path := "/v1/admin/projects/" + itoa(id) + "/quota"
	type quota map[string]int
	base := quota{"max_servers": 1, "max_vcpu": 2, "max_memory_mb": 3, "max_disk_gb": 4, "max_ips": 5}
	with := func(k string, v int) quota {
		q := quota{}
		for a, b := range base {
			q[a] = b
		}
		q[k] = v
		return q
	}
	q := decodeJSON[quota](t, app, admin.Send(http.MethodPut, path, with("max_backups", 12)), http.StatusOK)
	require.Equal(t, 12, q["max_backups"])
	// Omitted keeps it.
	q = decodeJSON[quota](t, app, admin.Send(http.MethodPut, path, base), http.StatusOK)
	require.Equal(t, 12, q["max_backups"])
	q = decodeJSON[quota](t, app, admin.Send(http.MethodPut, path, with("max_backups", 0)), http.StatusOK)
	require.Equal(t, 0, q["max_backups"])
	res := admin.Send(http.MethodPut, path, with("max_backups", -1))
	require.Equal(t, http.StatusUnprocessableEntity, res.StatusCode, app.Body(res))
	res.Body.Close()

	p := decodeJSON[struct {
		Quota quota `json:"quota"`
		Usage quota `json:"usage"`
	}](t, app, admin.Get("/v1/admin/projects/"+itoa(id)), http.StatusOK)
	require.Contains(t, p.Quota, "max_backups")
	require.Contains(t, p.Usage, "backups")
	require.Contains(t, p.Usage, "backups_reserved")
}

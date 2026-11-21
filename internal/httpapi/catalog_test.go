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
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

type page struct {
	Meta struct {
		Pagination struct {
			Page         int `json:"page"`
			PerPage      int `json:"per_page"`
			TotalEntries int `json:"total_entries"`
		} `json:"pagination"`
	} `json:"meta"`
}

func TestAdminLocationHTTP(t *testing.T) {
	app := testutil.NewApp(t)
	app.SeedPVE()
	admin := app.LoginAdmin()
	id := admin.CreateLocation("eu-1")

	res := admin.Get("/v1/admin/locations/" + itoa(id))
	require.Equal(t, 200, res.StatusCode)
	body := app.Body(res)
	require.NotContains(t, body, "token_secret")
	require.NotContains(t, body, app.PVE.Conn().Secret)
	var loc struct {
		Name          string  `json:"name"`
		ImportStorage *string `json:"import_storage"`
		PVE           struct {
			TokenID   string `json:"token_id"`
			HasSecret bool   `json:"has_secret"`
		} `json:"pve"`
		Nodes []struct {
			Name    string `json:"name"`
			Enabled bool   `json:"enabled"`
		} `json:"nodes"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &loc))
	require.Equal(t, "eu-1", loc.Name)
	require.NotNil(t, loc.ImportStorage)
	require.Empty(t, *loc.ImportStorage)
	require.True(t, loc.PVE.HasSecret)
	require.Equal(t, app.PVE.Conn().TokenID, loc.PVE.TokenID)
	require.Len(t, loc.Nodes, 2)

	conn := app.PVE.Conn()
	res = admin.Send(http.MethodPost, "/v1/admin/locations", map[string]any{
		"name": "bad", "disk_storage": "ceph", "iso_storage": "local",
		"pve": map[string]any{"url": conn.BaseURL, "token_id": conn.TokenID, "token_secret": "nope", "ca_fingerprint": conn.CAFingerprint},
	})
	require.Equal(t, 422, res.StatusCode)
	require.Equal(t, "pve_unreachable", errCode(t, app, res))

	res = admin.Send(http.MethodPost, "/v1/admin/locations/"+itoa(id)+"/sync_nodes", nil)
	require.Equal(t, 200, res.StatusCode, app.Body(res))
	res.Body.Close()

	res = admin.Get("/v1/admin/locations?per_page=1")
	require.Equal(t, 200, res.StatusCode)
	p := decode[page](t, app, res)
	require.Equal(t, 1, p.Meta.Pagination.TotalEntries)
	require.Equal(t, 1, p.Meta.Pagination.PerPage)

	app.CreateUser("u@example.com", false)
	u := app.Login("u@example.com")
	res = u.Get("/v1/admin/locations")
	require.Equal(t, 403, res.StatusCode)
	res.Body.Close()

	imgID := admin.CreateImage("ubuntu-24.04", id, 9000)
	res = admin.Send(http.MethodDelete, "/v1/admin/locations/"+itoa(id), nil)
	require.Equal(t, 409, res.StatusCode)
	require.Equal(t, "location_in_use", errCode(t, app, res))
	res = admin.Send(http.MethodDelete, "/v1/admin/images/"+itoa(imgID), nil)
	require.Equal(t, 204, res.StatusCode)
	res.Body.Close()
	res = admin.Send(http.MethodDelete, "/v1/admin/locations/"+itoa(id), nil)
	require.Equal(t, 204, res.StatusCode)
	res.Body.Close()
}

func TestProjectCatalogHTTP(t *testing.T) {
	app := testutil.NewApp(t)
	app.SeedPVE()
	admin := app.LoginAdmin()
	eu1 := admin.CreateLocation("eu-1")
	eu2 := admin.CreateLocation("eu-2")
	admin.CreateServerType("small", 1, 1024, 10)
	old := admin.CreateServerType("tiny", 1, 512, 5)
	res := admin.Send(http.MethodPut, "/v1/admin/server_types/"+itoa(old), map[string]any{
		"name": "tiny", "cores": 1, "memory_mb": 512, "disk_gb": 5, "deprecated": true,
	})
	require.Equal(t, 200, res.StatusCode, app.Body(res))
	res.Body.Close()
	admin.CreateImage("ubuntu-24.04", eu1, 9000)
	admin.CreateImage("debian-12", eu2, 9000)

	app.CreateUser("u@example.com", false)
	u := app.Login("u@example.com")
	pid := u.CreateProject("Web")
	c := app.TokenClient(u.CreateToken(pid, "read"))

	res = c.Get("/v1/locations")
	require.Equal(t, 200, res.StatusCode)
	body := app.Body(res)
	require.Contains(t, body, `"eu-1"`)
	require.NotContains(t, body, "token_id")
	require.NotContains(t, body, "pve")

	res = c.Get("/v1/server_types")
	require.Equal(t, 200, res.StatusCode)
	types := decode[struct {
		page
		ServerTypes []struct {
			Name       string `json:"name"`
			Deprecated bool   `json:"deprecated"`
		} `json:"server_types"`
	}](t, app, res)
	require.Len(t, types.ServerTypes, 2)
	require.Equal(t, 2, types.Meta.Pagination.TotalEntries)
	for _, st := range types.ServerTypes {
		require.Equal(t, st.Name == "tiny", st.Deprecated)
	}

	for _, ref := range []string{"eu-1", itoa(eu1)} {
		res = c.Get("/v1/images?location=" + ref)
		require.Equal(t, 200, res.StatusCode)
		imgs := decode[struct {
			Images []struct {
				Name string `json:"name"`
			} `json:"images"`
		}](t, app, res)
		require.Len(t, imgs.Images, 1)
		require.Equal(t, "ubuntu-24.04", imgs.Images[0].Name)
	}
	res = c.Get("/v1/images")
	require.Equal(t, 200, res.StatusCode)
	require.Contains(t, app.Body(res), "debian-12")
	res = c.Get("/v1/images?location=nowhere")
	require.Equal(t, 404, res.StatusCode)
	res.Body.Close()

	res = u.Get("/v1/server_types")
	require.Equal(t, 404, res.StatusCode, "sessions need Project-ID")
	res.Body.Close()
	res = u.WithProject(pid).Get("/v1/server_types")
	require.Equal(t, 200, res.StatusCode)
	res.Body.Close()
}

func TestSSHKeysHTTP(t *testing.T) {
	app := testutil.NewApp(t)
	app.CreateUser("u@example.com", false)
	u := app.Login("u@example.com")
	pid := u.CreateProject("Web")
	other := u.CreateProject("Other")
	c := u.WithProject(pid)

	pub, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	pk, err := ssh.NewPublicKey(pub)
	require.NoError(t, err)
	key := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pk)))

	res := c.Send(http.MethodPost, "/v1/ssh_keys", map[string]string{"name": "laptop", "public_key": "garbage"})
	require.Equal(t, 422, res.StatusCode)
	require.Equal(t, "invalid_input", errCode(t, app, res))

	res = c.Send(http.MethodPost, "/v1/ssh_keys", map[string]string{"name": "laptop", "public_key": key})
	require.Equal(t, 201, res.StatusCode)
	k := decode[struct {
		ID          int64  `json:"id"`
		Fingerprint string `json:"fingerprint"`
	}](t, app, res)
	require.Equal(t, ssh.FingerprintLegacyMD5(pk), k.Fingerprint)

	res = c.Send(http.MethodPost, "/v1/ssh_keys", map[string]string{"name": "again", "public_key": key})
	require.Equal(t, 409, res.StatusCode)
	require.Equal(t, "uniqueness_error", errCode(t, app, res))

	res = c.Get("/v1/ssh_keys")
	require.Equal(t, 200, res.StatusCode)
	require.Equal(t, 1, decode[page](t, app, res).Meta.Pagination.TotalEntries)

	res = u.WithProject(other).Get("/v1/ssh_keys/" + itoa(k.ID))
	require.Equal(t, 404, res.StatusCode)
	res.Body.Close()

	ro := app.TokenClient(u.CreateToken(pid, "read"))
	res = ro.Send(http.MethodPut, "/v1/ssh_keys/"+itoa(k.ID), map[string]string{"name": "x"})
	require.Equal(t, 403, res.StatusCode)
	res.Body.Close()

	res = c.Send(http.MethodPut, "/v1/ssh_keys/"+itoa(k.ID), map[string]string{"name": "work"})
	require.Equal(t, 200, res.StatusCode)
	require.Contains(t, app.Body(res), `"name":"work"`)
	res = c.Send(http.MethodDelete, "/v1/ssh_keys/"+itoa(k.ID), nil)
	require.Equal(t, 204, res.StatusCode)
	res.Body.Close()
	res = c.Get("/v1/ssh_keys/" + itoa(k.ID))
	require.Equal(t, 404, res.StatusCode)
	res.Body.Close()
}

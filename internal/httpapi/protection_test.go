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
	"encoding/json"
	"net/http"
	"testing"

	"github.com/nebla-cloud/nebla/internal/ent/volume"
	"github.com/stretchr/testify/require"
)

type apiProtection struct {
	Delete  bool `json:"delete"`
	Rebuild bool `json:"rebuild"`
}

type apiProtected struct {
	ID         int64         `json:"id"`
	Protection apiProtection `json:"protection"`
}

func errMessage(t *testing.T, body string) string {
	t.Helper()
	var e struct{ Error struct{ Message string } }
	require.NoError(t, json.Unmarshal([]byte(body), &e))
	return e.Error.Message
}

func TestPutServer_PartialProtection(t *testing.T) {
	e := newVolEnv(t)
	web := e.RunningServer("web")
	p := "/v1/servers/" + itoa(web.ServerID)

	got := decodeJSON[apiProtected](t, e.App, e.C.Send(http.MethodPut, p, map[string]any{"protection": map[string]any{"delete": true}}), http.StatusOK)
	require.Equal(t, apiProtection{Delete: true}, got.Protection)

	got = decodeJSON[apiProtected](t, e.App, e.C.Send(http.MethodPut, p, map[string]any{"protection": map[string]any{"rebuild": true}}), http.StatusOK)
	require.Equal(t, apiProtection{Delete: true, Rebuild: true}, got.Protection)

	got = decodeJSON[apiProtected](t, e.App, e.C.Get(p), http.StatusOK)
	require.Equal(t, apiProtection{Delete: true, Rebuild: true}, got.Protection)
}

func TestDeleteVolume_Protected409(t *testing.T) {
	e := newVolEnv(t)
	v, a := e.createVolume(e.C, map[string]any{"name": "data", "size": 10, "location": "eu-1", "protection": map[string]any{"delete": true}})
	e.settle(a, v.ID, volume.StatusAvailable, nil, "")
	vp := "/v1/volumes/" + itoa(v.ID)

	res := e.C.Send(http.MethodDelete, vp, nil)
	require.Equal(t, http.StatusConflict, res.StatusCode)
	body := e.App.Body(res)
	require.Equal(t, "protected", errCode2(t, body))
	require.Equal(t, "Turn off delete protection first.", errMessage(t, body))

	got := decodeJSON[apiProtected](t, e.App, e.C.Send(http.MethodPut, vp, map[string]any{"protection": map[string]any{"delete": false}}), http.StatusOK)
	require.False(t, got.Protection.Delete)
	e.action(e.C, http.MethodDelete, vp, nil, http.StatusOK)
}

func TestPutVolume_ReadOnlyForbidden(t *testing.T) {
	e := newVolEnv(t)
	v, a := e.createVolume(e.C, map[string]any{"name": "data", "size": 10, "location": "eu-1"})
	e.settle(a, v.ID, volume.StatusAvailable, nil, "")
	ro := e.App.TokenClient(e.Admin.CreateToken(e.Project, "read"))

	res := ro.Send(http.MethodPut, "/v1/volumes/"+itoa(v.ID), map[string]any{"protection": map[string]any{"delete": true}})
	require.Equal(t, http.StatusForbidden, res.StatusCode)
	e.App.Body(res)
	got := decodeJSON[apiProtected](t, e.App, e.C.Get("/v1/volumes/"+itoa(v.ID)), http.StatusOK)
	require.False(t, got.Protection.Delete)
}

func TestPostPrimaryIps_WithProtection(t *testing.T) {
	e := newFIPEnv(t)
	out := decodeJSON[struct {
		PrimaryIP apiProtected `json:"primary_ip"`
	}](t, e.App, e.C.Send(http.MethodPost, "/v1/primary_ips", map[string]any{"type": "ipv6", "location": "eu-1", "name": "keep6",
		"protection": map[string]any{"delete": true}}), http.StatusCreated)
	require.True(t, out.PrimaryIP.Protection.Delete)

	res := e.C.Send(http.MethodDelete, "/v1/primary_ips/"+itoa(out.PrimaryIP.ID), nil)
	require.Equal(t, http.StatusConflict, res.StatusCode)
	require.Equal(t, "protected", errCode2(t, e.App.Body(res)))
}

func TestRebuildServer_Protected409(t *testing.T) {
	e := newVolEnv(t)
	web := e.RunningServer("web")
	res := e.C.Send(http.MethodPut, "/v1/servers/"+itoa(web.ServerID), map[string]any{"protection": map[string]any{"rebuild": true}})
	require.Equal(t, http.StatusOK, res.StatusCode, e.App.Body(res))
	res = e.C.Send(http.MethodPost, "/v1/servers/"+itoa(web.ServerID)+"/actions/rebuild", map[string]any{"image": "ubuntu"})
	require.Equal(t, http.StatusConflict, res.StatusCode)
	body := e.App.Body(res)
	require.Equal(t, "protected", errCode2(t, body))
	require.Equal(t, "Turn off rebuild protection first.", errMessage(t, body))
}

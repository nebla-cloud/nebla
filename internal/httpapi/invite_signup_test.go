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

	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/stretchr/testify/require"
)

func enableSelfSignup(t *testing.T, app *testutil.App) {
	t.Helper()
	admin := app.LoginAdmin()
	res := admin.Send(http.MethodPut, "/v1/admin/settings", map[string]bool{"self_signup": true})
	require.Equal(t, 200, res.StatusCode, app.Body(res))
	res.Body.Close()
}

func TestCookielessAcceptBlockedWithoutSelfSignup(t *testing.T) {
	app := testutil.NewApp(t)
	app.CreateUser("u@example.test", false)
	u := app.Login("u@example.test")
	token := u.Invite(u.CreateProject("p"), "victim@example.test", "member")

	res := app.NewClient().Send(http.MethodPost, "/v1/invites/"+token+"/accept", map[string]string{"name": "x", "password": "attacker-chosen-pw-123"})
	require.Equal(t, 401, res.StatusCode)
	require.Equal(t, "login_required", errCode(t, app, res))

	res = app.NewClient().Send(http.MethodPost, "/v1/auth/login", map[string]string{"email": "victim@example.test", "password": "attacker-chosen-pw-123"})
	res.Body.Close()
	require.Equal(t, 401, res.StatusCode, "no account may be created for the invited email")
}

func TestCookielessAcceptAllowedWithSelfSignup(t *testing.T) {
	app := testutil.NewApp(t)
	enableSelfSignup(t, app)
	app.CreateUser("u@example.test", false)
	u := app.Login("u@example.test")
	token := u.Invite(u.CreateProject("p"), "new@example.test", "member")

	res := app.NewClient().Send(http.MethodPost, "/v1/invites/"+token+"/accept", map[string]string{"name": "x", "password": testutil.Password})
	require.Equal(t, 200, res.StatusCode, app.Body(res))
	res.Body.Close()
}

func TestLoggedInAcceptStillWorks(t *testing.T) {
	app := testutil.NewApp(t)
	app.CreateUser("u@example.test", false)
	app.CreateUser("b@example.test", false)
	u := app.Login("u@example.test")
	token := u.Invite(u.CreateProject("p"), "b@example.test", "member")

	b := app.Login("b@example.test")
	res := b.Send(http.MethodPost, "/v1/invites/"+token+"/accept", nil)
	require.Equal(t, 200, res.StatusCode, app.Body(res))
	res.Body.Close()
}

func TestAdminInviteCreatesAccountWithoutSelfSignup(t *testing.T) {
	app := testutil.NewApp(t)
	admin := app.LoginAdmin()
	token := admin.Invite(admin.CreateProject("p"), "new@example.test", "member")

	res := app.NewClient().Send(http.MethodPost, "/v1/invites/"+token+"/accept", map[string]string{"name": "x", "password": testutil.Password})
	require.Equal(t, 200, res.StatusCode, app.Body(res))
	require.NotEmpty(t, res.Cookies())
	res.Body.Close()

	res = app.NewClient().Send(http.MethodPost, "/v1/auth/login", map[string]string{"email": "new@example.test", "password": testutil.Password})
	res.Body.Close()
	require.Equal(t, 200, res.StatusCode, "the admin's invite created the account")
}

func TestGetInvitePublic(t *testing.T) {
	app := testutil.NewApp(t)
	app.CreateUser("u@example.test", false)
	u := app.Login("u@example.test")
	token := u.Invite(u.CreateProject("Homelab"), "new@example.test", "member")

	res := app.NewClient().Get("/v1/invites/" + token)
	body := app.Body(res)
	require.Equal(t, 200, res.StatusCode, body)
	var pv map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &pv))
	require.Equal(t, "Homelab", pv["project_name"])
	require.Equal(t, "new@example.test", pv["email"])
	require.Equal(t, "member", pv["role"])
	require.Equal(t, "u@example.test", pv["inviter_name"])
	require.Equal(t, false, pv["account_exists"])
	require.Equal(t, false, pv["can_create_account"])
	require.NotEmpty(t, pv["expires"])

	res = app.NewClient().Get("/v1/invites/unknown-token")
	require.Equal(t, 404, res.StatusCode)
	require.Equal(t, "not_found", errCode(t, app, res))
}

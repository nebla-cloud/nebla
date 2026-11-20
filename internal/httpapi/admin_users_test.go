package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/stretchr/testify/require"
)

type adminUserJSON struct {
	ID           int64  `json:"id"`
	Email        string `json:"email"`
	Name         string `json:"name"`
	IsAdmin      bool   `json:"is_admin"`
	PasskeyCount *int   `json:"passkey_count"`
}

func TestNewAdminOpsAuth(t *testing.T) {
	app := testutil.NewApp(t)
	app.CreateUser("admin@example.com", true)
	target := app.CreateUser("t@example.com", false)
	app.CreateUser("u@example.com", false)
	u := app.Login("u@example.com")
	pid := u.CreateProject("Web")
	tok := app.TokenClient(u.CreateToken(pid, "read_write"))
	uid, p := itoa(target.ID), itoa(pid)
	ops := []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/v1/admin/users", nil},
		{http.MethodGet, "/v1/admin/projects", nil},
		{http.MethodPut, "/v1/admin/users/" + uid, map[string]any{"name": "N", "email": "n@example.com", "is_admin": true}},
		{http.MethodDelete, "/v1/admin/users/" + uid + "?take_over=true", nil},
		{http.MethodPost, "/v1/admin/users/" + uid + "/password", map[string]any{"password": "another-long-password", "remove_passkeys": true}},
		{http.MethodDelete, "/v1/admin/users/" + uid + "/totp", nil},
		{http.MethodDelete, "/v1/admin/users/" + uid + "/passkeys", nil},
		{http.MethodGet, "/v1/admin/projects/" + p, nil},
		{http.MethodDelete, "/v1/admin/projects/" + p, nil},
	}
	for _, op := range ops {
		res := u.Send(op.method, op.path, op.body)
		require.Equal(t, http.StatusForbidden, res.StatusCode, "%s %s", op.method, op.path)
		res.Body.Close()
		res = tok.Send(op.method, op.path, op.body)
		require.Equal(t, http.StatusUnauthorized, res.StatusCode, "%s %s", op.method, op.path)
		res.Body.Close()
	}
	res := app.Login("admin@example.com").Get("/v1/admin/projects/" + p)
	require.Equal(t, http.StatusOK, res.StatusCode, "the project survived")
	res.Body.Close()
}

func TestAdminUsersHTTP(t *testing.T) {
	app := testutil.NewApp(t)
	admin := app.LoginAdmin()
	me := app.CreateUser("admin2@example.com", false)
	u := app.CreateUser("u@example.com", false)
	app.CreateUser("taken@example.com", false)
	userC := app.Login("u@example.com")
	path := "/v1/admin/users/" + itoa(u.ID)

	res := admin.Get("/v1/admin/users")
	require.Equal(t, http.StatusOK, res.StatusCode)
	list := decode[struct {
		Users []adminUserJSON `json:"users"`
	}](t, app, res)
	require.Len(t, list.Users, 4)
	require.NotNil(t, list.Users[0].PasskeyCount)

	res = admin.Send(http.MethodPost, "/v1/admin/users", map[string]any{"email": "new@example.com", "password": testutil.Password, "name": "New"})
	require.Equal(t, http.StatusCreated, res.StatusCode)
	created := decode[adminUserJSON](t, app, res)
	require.Equal(t, 0, *created.PasskeyCount)

	put := func(id int64, body map[string]any) *http.Response {
		return admin.Send(http.MethodPut, "/v1/admin/users/"+itoa(id), body)
	}
	res = put(u.ID, map[string]any{"name": "U", "email": "TAKEN@example.com", "is_admin": false})
	require.Equal(t, http.StatusConflict, res.StatusCode)
	require.Equal(t, "email_taken", errCode(t, app, res))
	res = put(99999, map[string]any{"name": "U", "email": "x@example.com", "is_admin": false})
	require.Equal(t, http.StatusNotFound, res.StatusCode)
	require.Equal(t, "not_found", errCode(t, app, res))

	res = admin.Get("/v1/me")
	adminID := decode[adminUserJSON](t, app, res).ID
	res = put(adminID, map[string]any{"name": "A", "email": "admin@example.com", "is_admin": false})
	require.Equal(t, http.StatusUnprocessableEntity, res.StatusCode)
	require.Equal(t, "self_admin_action", errCode(t, app, res))
	res = put(adminID, map[string]any{"name": "A", "email": "me@example.com", "is_admin": true})
	require.Equal(t, http.StatusOK, res.StatusCode, "editing my own email keeps me signed in")
	res.Body.Close()
	res = admin.Get("/v1/me")
	require.Equal(t, http.StatusOK, res.StatusCode)
	res.Body.Close()

	res = put(u.ID, map[string]any{"name": "Renamed", "email": "u2@example.com", "is_admin": false})
	require.Equal(t, http.StatusOK, res.StatusCode)
	got := decode[adminUserJSON](t, app, res)
	require.Equal(t, "Renamed", got.Name)
	require.Equal(t, "u2@example.com", got.Email)
	res = userC.Get("/v1/me")
	require.Equal(t, http.StatusUnauthorized, res.StatusCode, "an email change signs them out")
	res.Body.Close()

	other := app.Login("admin2@example.com")
	res = other.Send(http.MethodPut, "/v1/admin/users/"+itoa(adminID), map[string]any{"name": "A", "email": "me@example.com", "is_admin": false})
	require.Equal(t, http.StatusForbidden, res.StatusCode)
	res.Body.Close()
	res = put(me.ID, map[string]any{"name": "A2", "email": "admin2@example.com", "is_admin": true})
	require.Equal(t, http.StatusOK, res.StatusCode)
	res.Body.Close()
	other = app.Login("admin2@example.com")
	res = other.Send(http.MethodPut, "/v1/admin/users/"+itoa(adminID), map[string]any{"name": "A", "email": "me@example.com", "is_admin": false})
	require.Equal(t, http.StatusOK, res.StatusCode, "two admins, demoting one is fine")
	res.Body.Close()
	res = admin.Send(http.MethodPut, "/v1/admin/users/"+itoa(me.ID), map[string]any{"name": "A2", "email": "admin2@example.com", "is_admin": false})
	require.Equal(t, http.StatusForbidden, res.StatusCode, "no longer an admin")
	res.Body.Close()
	res = other.Send(http.MethodDelete, "/v1/admin/users/"+itoa(me.ID), nil)
	require.Equal(t, http.StatusUnprocessableEntity, res.StatusCode)
	require.Equal(t, "self_admin_action", errCode(t, app, res))
	res = other.Send(http.MethodPost, "/v1/admin/users/"+itoa(me.ID)+"/password", map[string]any{"password": "another-long-password", "remove_passkeys": false})
	require.Equal(t, http.StatusUnprocessableEntity, res.StatusCode)
	require.Equal(t, "self_admin_action", errCode(t, app, res))

	res = other.Send(http.MethodPost, path+"/password", map[string]any{"password": "short", "remove_passkeys": true})
	require.Equal(t, http.StatusUnprocessableEntity, res.StatusCode)
	require.Equal(t, "invalid_input", errCode(t, app, res))
	res = other.Send(http.MethodPost, path+"/password", map[string]any{"password": "another-long-password", "remove_passkeys": true})
	require.Equal(t, http.StatusNoContent, res.StatusCode)
	res.Body.Close()
	res = other.Send(http.MethodDelete, path+"/totp", nil)
	require.Equal(t, http.StatusNoContent, res.StatusCode)
	res.Body.Close()
	res = other.Send(http.MethodDelete, path+"/passkeys", nil)
	require.Equal(t, http.StatusNoContent, res.StatusCode)
	res.Body.Close()
	res = other.Send(http.MethodDelete, "/v1/admin/users/99999/totp", nil)
	require.Equal(t, http.StatusNotFound, res.StatusCode)
	res.Body.Close()
}

func TestAdminDeleteUserHTTP(t *testing.T) {
	app := testutil.NewApp(t)
	admin := app.LoginAdmin()
	u := app.CreateUser("u@example.com", false)
	uc := app.Login("u@example.com")
	pid := uc.CreateProject("Homelab")
	path := "/v1/admin/users/" + itoa(u.ID)

	res := admin.Send(http.MethodDelete, path, nil)
	require.Equal(t, http.StatusConflict, res.StatusCode)
	b := app.Body(res)
	require.Contains(t, b, `"sole_owner"`)
	require.Contains(t, b, "They're the only owner of Homelab.")

	res = admin.Send(http.MethodDelete, path+"?take_over=false", nil)
	require.Equal(t, http.StatusConflict, res.StatusCode)
	res.Body.Close()
	res = admin.Send(http.MethodDelete, path+"?take_over=true", nil)
	require.Equal(t, http.StatusNoContent, res.StatusCode)
	res.Body.Close()
	res = admin.Get("/v1/projects/" + itoa(pid))
	require.Equal(t, http.StatusOK, res.StatusCode)
	require.Contains(t, app.Body(res), `"role":"owner"`)
	res = admin.Send(http.MethodDelete, path, nil)
	require.Equal(t, http.StatusNotFound, res.StatusCode)
	require.Equal(t, "not_found", errCode(t, app, res))
}

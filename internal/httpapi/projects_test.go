package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/Hexeption/nebla/internal/ent"
	"github.com/Hexeption/nebla/internal/httpapi"
	"github.com/Hexeption/nebla/internal/testutil"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
)

// stubApp mounts a fake project-scoped /v1/ssh_keys route that echoes the scope.
func stubApp(t *testing.T) *testutil.App {
	return testutil.NewApp(t, func(d *httpapi.Deps) {
		d.ScopedRoutes = func(r chi.Router) {
			echo := func(w http.ResponseWriter, r *http.Request) {
				sc, _ := httpapi.ScopeFrom(r.Context())
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"project_id": sc.ProjectID, "role": sc.Role, "can_write": sc.CanWrite})
			}
			r.Get("/ssh_keys", echo)
			r.Post("/ssh_keys", echo)
		}
	})
}

func decode[T any](t *testing.T, app *testutil.App, res *http.Response) T {
	t.Helper()
	var v T
	require.NoError(t, json.Unmarshal([]byte(app.Body(res)), &v))
	return v
}

func TestCreatorIsOwnerHTTP(t *testing.T) {
	app := testutil.NewApp(t)
	app.CreateUser("a@example.com", false)
	c := app.Login("a@example.com")
	id := c.CreateProject("Web")

	res := c.Get("/v1/projects")
	require.Equal(t, 200, res.StatusCode)
	body := decode[struct {
		Projects []struct {
			ID   int64  `json:"id"`
			Role string `json:"role"`
		}
	}](t, app, res)
	require.Len(t, body.Projects, 1)
	require.Equal(t, id, body.Projects[0].ID)
	require.Equal(t, "owner", body.Projects[0].Role)

	app.CreateUser("b@example.com", false)
	res = app.Login("b@example.com").Get("/v1/projects/" + itoa(id))
	require.Equal(t, 404, res.StatusCode)
}

func TestInviteAcceptNewUser(t *testing.T) {
	app := testutil.NewApp(t)
	app.CreateUser("a@example.com", false)
	owner := app.Login("a@example.com")
	id := owner.CreateProject("Web")

	res := owner.Send(http.MethodPost, "/v1/projects/"+itoa(id)+"/invites", map[string]string{"email": "New@Example.com", "role": "member"})
	require.Equal(t, 201, res.StatusCode)
	url := decode[struct {
		InviteURL string `json:"invite_url"`
	}](t, app, res).InviteURL
	require.Contains(t, url, "/invite/")
	token := url[strings.LastIndex(url, "/")+1:]

	guest := app.NewClient()
	res = guest.Send(http.MethodPost, "/v1/invites/"+token+"/accept", map[string]string{"name": "New", "password": testutil.Password})
	require.Equal(t, 200, res.StatusCode, app.Body(res))
	res.Body.Close()

	res = guest.Get("/v1/me")
	require.Equal(t, 200, res.StatusCode)
	require.Contains(t, app.Body(res), `"email":"new@example.com"`)
	res = guest.Get("/v1/projects/" + itoa(id))
	require.Equal(t, 200, res.StatusCode)
	require.Contains(t, app.Body(res), `"role":"member"`)

	res = app.NewClient().Send(http.MethodPost, "/v1/invites/"+token+"/accept", map[string]string{"name": "X", "password": testutil.Password})
	require.Equal(t, 404, res.StatusCode, "invites are single use")
}

func TestInviteExistingEmailNeedsLogin(t *testing.T) {
	app := testutil.NewApp(t)
	app.CreateUser("a@example.com", false)
	app.CreateUser("b@example.com", false)
	owner := app.Login("a@example.com")
	id := owner.CreateProject("Web")
	token := owner.Invite(id, "b@example.com", "read_only")

	res := app.NewClient().Send(http.MethodPost, "/v1/invites/"+token+"/accept", map[string]string{"name": "B", "password": testutil.Password})
	require.Equal(t, 409, res.StatusCode)
	require.Equal(t, "email_taken", errCode(t, app, res))

	b := app.Login("b@example.com")
	res = b.Send(http.MethodPost, "/v1/invites/"+token+"/accept", nil)
	require.Equal(t, 200, res.StatusCode, app.Body(res))
	res.Body.Close()
	res = b.WithProject(id).Send(http.MethodPut, "/v1/projects/"+itoa(id), map[string]string{"name": "x"})
	require.Equal(t, 403, res.StatusCode)
}

func TestReadTokenCannotWrite(t *testing.T) {
	app := stubApp(t)
	app.CreateUser("a@example.com", false)
	c := app.Login("a@example.com")
	id := c.CreateProject("Web")

	ro := app.TokenClient(c.CreateToken(id, "read"))
	res := ro.Get("/v1/ssh_keys")
	require.Equal(t, 200, res.StatusCode)
	require.JSONEq(t, `{"project_id":`+itoa(id)+`,"role":"api_token","can_write":false}`, app.Body(res))
	res = ro.Send(http.MethodPost, "/v1/ssh_keys", map[string]string{})
	require.Equal(t, 403, res.StatusCode)
	require.Equal(t, "forbidden", errCode(t, app, res))

	rw := app.TokenClient(c.CreateToken(id, "read_write"))
	res = rw.Send(http.MethodPost, "/v1/ssh_keys", map[string]string{})
	require.Equal(t, 200, res.StatusCode)
	res.Body.Close()

	res = rw.Get("/v1/me")
	require.Equal(t, 401, res.StatusCode, "tokens cannot use user-only endpoints")
	require.Equal(t, "unauthorized", errCode(t, app, res))
	res = rw.Get("/v1/projects")
	require.Equal(t, 401, res.StatusCode)
	res.Body.Close()
}

func TestSessionNeedsProjectHeader(t *testing.T) {
	app := stubApp(t)
	app.CreateUser("a@example.com", false)
	c := app.Login("a@example.com")
	id := c.CreateProject("Web")

	res := c.Get("/v1/ssh_keys")
	require.Equal(t, 404, res.StatusCode)
	require.Equal(t, "not_found", errCode(t, app, res))

	res = c.WithProject(id).Get("/v1/ssh_keys")
	require.Equal(t, 200, res.StatusCode)
	require.JSONEq(t, `{"project_id":`+itoa(id)+`,"role":"owner","can_write":true}`, app.Body(res))

	app.CreateUser("b@example.com", false)
	res = app.Login("b@example.com").WithProject(id).Get("/v1/ssh_keys")
	require.Equal(t, 404, res.StatusCode, "non-members get 404")
	res.Body.Close()

	res = app.Get("/v1/ssh_keys")
	require.Equal(t, 401, res.StatusCode)
	res.Body.Close()
}

func TestRevokedTokenRejected(t *testing.T) {
	app := stubApp(t)
	app.CreateUser("a@example.com", false)
	c := app.Login("a@example.com")
	id := c.CreateProject("Web")
	tok := c.CreateToken(id, "read_write")
	tc := app.TokenClient(tok)
	res := tc.Get("/v1/ssh_keys")
	require.Equal(t, 200, res.StatusCode)
	res.Body.Close()

	res = c.Get("/v1/projects/" + itoa(id) + "/tokens")
	require.Equal(t, 200, res.StatusCode)
	list := decode[struct {
		Tokens []struct {
			ID int64 `json:"id"`
		}
	}](t, app, res)
	require.Len(t, list.Tokens, 1)
	res = c.Send(http.MethodDelete, "/v1/projects/"+itoa(id)+"/tokens/"+itoa(list.Tokens[0].ID), nil)
	require.Equal(t, 204, res.StatusCode)
	res.Body.Close()

	res = tc.Get("/v1/ssh_keys")
	require.Equal(t, 401, res.StatusCode)
	require.Equal(t, "unauthorized", errCode(t, app, res))
	res = app.TokenClient("nb_" + strings.Repeat("a", 40)).Get("/v1/ssh_keys")
	require.Equal(t, 401, res.StatusCode)
	res.Body.Close()
}

func TestDeleteProjectNotEmpty(t *testing.T) {
	app := testutil.NewApp(t, func(d *httpapi.Deps) {
		d.ProjectHooks.HasServers = func(_ context.Context, _ *ent.Client, _ int64) (bool, error) { return true, nil }
	})
	app.CreateUser("a@example.com", false)
	c := app.Login("a@example.com")
	id := c.CreateProject("Web")
	res := c.Send(http.MethodDelete, "/v1/projects/"+itoa(id), nil)
	require.Equal(t, 409, res.StatusCode)
	require.Equal(t, "project_not_empty", errCode(t, app, res))
}

func TestAdminUsersAndQuota(t *testing.T) {
	app := testutil.NewApp(t)
	admin := app.LoginAdmin()
	res := admin.Send(http.MethodPost, "/v1/admin/users", map[string]any{"email": "u@example.com", "password": testutil.Password, "name": "U"})
	require.Equal(t, 201, res.StatusCode, app.Body(res))
	res.Body.Close()
	res = admin.Get("/v1/admin/users")
	require.Equal(t, 200, res.StatusCode)
	require.Contains(t, app.Body(res), "u@example.com")

	u := app.Login("u@example.com")
	id := u.CreateProject("Web")
	res = u.Get("/v1/admin/users")
	require.Equal(t, 403, res.StatusCode)
	res.Body.Close()
	q := map[string]int{"max_servers": 3, "max_vcpu": 8, "max_memory_mb": 0, "max_disk_gb": 0, "max_ips": 0}
	res = u.Send(http.MethodPut, "/v1/admin/projects/"+itoa(id)+"/quota", q)
	require.Equal(t, 403, res.StatusCode)
	res.Body.Close()
	res = admin.Send(http.MethodPut, "/v1/admin/projects/"+itoa(id)+"/quota", q)
	require.Equal(t, 200, res.StatusCode)
	res.Body.Close()
	res = u.Get("/v1/projects/" + itoa(id))
	require.Contains(t, app.Body(res), `"max_servers":3`)
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

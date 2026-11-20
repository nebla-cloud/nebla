package httpapi_test

import (
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

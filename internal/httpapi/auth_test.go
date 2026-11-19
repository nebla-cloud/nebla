package httpapi_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Hexeption/nebla/internal/testutil"
	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/require"
)

func errCode(t *testing.T, app *testutil.App, res *http.Response) string {
	t.Helper()
	var e struct{ Error struct{ Code string } }
	require.NoError(t, json.Unmarshal([]byte(app.Body(res)), &e))
	return e.Error.Code
}

func TestSetupOnlyOnce(t *testing.T) {
	app := testutil.NewApp(t)
	b := map[string]string{"email": "a@example.com", "password": testutil.Password, "name": "A"}
	res := app.PostJSON("/v1/setup", b)
	require.Equal(t, 201, res.StatusCode)
	res = app.PostJSON("/v1/setup", b)
	require.Equal(t, 409, res.StatusCode)
	require.Equal(t, "already_setup", errCode(t, app, res))
}

func TestLoginWrongPassword401(t *testing.T) {
	app := testutil.NewApp(t)
	app.CreateUser("a@example.com", true)
	res := app.PostJSON("/v1/auth/login", map[string]string{"email": "a@example.com", "password": "wrong-password"})
	require.Equal(t, 401, res.StatusCode)
	require.Equal(t, "invalid_credentials", errCode(t, app, res))
}

func TestLoginSetsCookieAndMe(t *testing.T) {
	app := testutil.NewApp(t)
	app.CreateUser("a@example.com", true)
	res := app.PostJSON("/v1/auth/login", map[string]string{"email": "a@example.com", "password": testutil.Password})
	require.Equal(t, 200, res.StatusCode)
	var c *http.Cookie
	for _, k := range res.Cookies() {
		if k.Name == "nb_session" {
			c = k
		}
	}
	require.NotNil(t, c)
	require.True(t, c.HttpOnly)
	require.Equal(t, http.SameSiteLaxMode, c.SameSite)
	require.False(t, c.Secure)

	cl := app.Login("a@example.com")
	res = cl.Get("/v1/me")
	require.Equal(t, 200, res.StatusCode)
	require.JSONEq(t, `{"id":1,"email":"a@example.com","name":"a@example.com","is_admin":true,"totp_enabled":false}`, app.Body(res))

	require.Equal(t, 204, cl.Send("POST", "/v1/auth/logout", nil).StatusCode)
	require.Equal(t, 401, cl.Get("/v1/me").StatusCode)
}

func TestUnauthenticatedProtectedRoute401(t *testing.T) {
	app := testutil.NewApp(t)
	res := app.Get("/v1/me")
	require.Equal(t, 401, res.StatusCode)
	require.Equal(t, "unauthorized", errCode(t, app, res))
}

func TestLoginRequiresTOTPWhenEnabled(t *testing.T) {
	app := testutil.NewApp(t)
	app.CreateUser("a@example.com", true)
	cl := app.Login("a@example.com")
	res := cl.Send("POST", "/v1/auth/totp/enroll", nil)
	require.Equal(t, 200, res.StatusCode)
	var en struct {
		Secret     string `json:"secret"`
		OtpauthURL string `json:"otpauth_url"`
	}
	require.NoError(t, json.Unmarshal([]byte(app.Body(res)), &en))
	require.NotEmpty(t, en.OtpauthURL)
	code, err := totp.GenerateCode(en.Secret, time.Now())
	require.NoError(t, err)
	require.Equal(t, 204, cl.Send("POST", "/v1/auth/totp/verify", map[string]string{"code": code}).StatusCode)

	login := map[string]string{"email": "a@example.com", "password": testutil.Password}
	res = app.PostJSON("/v1/auth/login", login)
	require.Equal(t, 401, res.StatusCode)
	require.Equal(t, "totp_required", errCode(t, app, res))
	login["totp_code"] = code
	require.Equal(t, 200, app.PostJSON("/v1/auth/login", login).StatusCode)

	require.Equal(t, 204, cl.Send("POST", "/v1/auth/totp/disable", map[string]string{"code": code}).StatusCode)
	delete(login, "totp_code")
	require.Equal(t, 200, app.PostJSON("/v1/auth/login", login).StatusCode)
}

func TestNonAdminForbiddenOnAdminRoute(t *testing.T) {
	app := testutil.NewApp(t)
	app.CreateUser("u@example.com", false)
	cl := app.Login("u@example.com")
	res := cl.Get("/v1/admin/settings")
	require.Equal(t, 403, res.StatusCode)
	require.Equal(t, "forbidden", errCode(t, app, res))
}

func TestSignupDisabledByDefault(t *testing.T) {
	app := testutil.NewApp(t)
	res := app.PostJSON("/v1/signup", map[string]string{"email": "n@example.com", "password": testutil.Password, "name": "N"})
	require.Equal(t, 404, res.StatusCode)
}

func TestSignupWhenEnabled(t *testing.T) {
	app := testutil.NewApp(t)
	app.CreateUser("a@example.com", true)
	admin := app.Login("a@example.com")
	res := admin.Send("PUT", "/v1/admin/settings", map[string]bool{"self_signup": true})
	require.Equal(t, 200, res.StatusCode)
	require.JSONEq(t, `{"self_signup":true}`, app.Body(admin.Get("/v1/admin/settings")))

	res = app.PostJSON("/v1/signup", map[string]string{"email": "n@example.com", "password": testutil.Password, "name": "N"})
	require.Equal(t, 201, res.StatusCode)
	require.NotEmpty(t, res.Cookies())
	res = app.PostJSON("/v1/signup", map[string]string{"email": "n@example.com", "password": testutil.Password, "name": "N"})
	require.Equal(t, 409, res.StatusCode)
	require.Equal(t, "email_taken", errCode(t, app, res))
}

func TestLoginRateLimited(t *testing.T) {
	app := testutil.NewApp(t)
	b := map[string]string{"email": "a@example.com", "password": "wrong-password"}
	for range 10 {
		require.Equal(t, 401, app.PostJSON("/v1/auth/login", b).StatusCode)
	}
	res := app.PostJSON("/v1/auth/login", b)
	require.Equal(t, 429, res.StatusCode)
	require.Equal(t, "rate_limited", errCode(t, app, res))
}

func TestLoginRateLimitIgnoresEmailWhitespace(t *testing.T) {
	app := testutil.NewApp(t)
	b := map[string]string{"email": "a@example.com", "password": "wrong-password"}
	for range 10 {
		require.Equal(t, 401, app.PostJSON("/v1/auth/login", b).StatusCode)
	}
	b["email"] = "  A@example.com "
	res := app.PostJSON("/v1/auth/login", b)
	require.Equal(t, 429, res.StatusCode)
}

func TestOversizeBodyRejected(t *testing.T) {
	app := testutil.NewApp(t)
	res := app.PostJSON("/v1/auth/login", map[string]string{"email": "a@example.com", "password": strings.Repeat("x", 2<<20)})
	require.Equal(t, 413, res.StatusCode)
	require.Equal(t, "request_too_large", errCode(t, app, res))
}

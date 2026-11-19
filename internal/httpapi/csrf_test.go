package httpapi_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/Hexeption/nebla/internal/httpapi"
	"github.com/Hexeption/nebla/internal/testutil"
	"github.com/stretchr/testify/require"
)

func csrfPost(t *testing.T, app *testutil.App, path, body, ct string, hdr map[string]string, cookies []*http.Cookie) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, app.URL+path, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", ct)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	return app.Do(req)
}

func sessionCookies(res *http.Response) []*http.Cookie {
	var out []*http.Cookie
	for _, c := range res.Cookies() {
		if c.Name == "nb_session" {
			out = append(out, c)
		}
	}
	return out
}

func loginBody() string {
	return `{"email":"dummy@example.test","password":"` + testutil.Password + `"}`
}

func TestLoginRejectsCrossSite(t *testing.T) {
	app := testutil.NewApp(t)
	app.CreateUser("dummy@example.test", false)

	res := csrfPost(t, app, "/v1/auth/login", loginBody(), "text/plain",
		map[string]string{"Origin": "https://evil.example", "Sec-Fetch-Site": "cross-site"}, nil)
	body := app.Body(res)
	require.Equal(t, http.StatusForbidden, res.StatusCode, body)
	require.Contains(t, body, `"forbidden"`)
	require.Empty(t, sessionCookies(res))
}

func TestMutationRejectsForeignOrigin(t *testing.T) {
	app := testutil.NewApp(t)
	app.CreateUser("dummy@example.test", false)
	res := csrfPost(t, app, "/v1/auth/login", loginBody(), "application/json", nil, nil)
	require.Equal(t, http.StatusOK, res.StatusCode, app.Body(res))
	cookies := sessionCookies(res)
	require.NotEmpty(t, cookies)

	res = csrfPost(t, app, "/v1/projects", `{"name":"csrf-proj"}`, "application/json",
		map[string]string{"Origin": "https://evil.example"}, cookies)
	body := app.Body(res)
	require.Equal(t, http.StatusForbidden, res.StatusCode, body)
	require.Contains(t, body, `"forbidden"`)
}

func TestSameOriginAndNonBrowserAllowed(t *testing.T) {
	for name, hdr := range map[string]map[string]string{
		"same-origin": {"Sec-Fetch-Site": "same-origin"},
		"no-headers":  nil,
	} {
		t.Run(name, func(t *testing.T) {
			app := testutil.NewApp(t)
			app.CreateUser("dummy@example.test", false)
			res := csrfPost(t, app, "/v1/auth/login", loginBody(), "application/json", hdr, nil)
			require.Equal(t, http.StatusOK, res.StatusCode, app.Body(res))
			require.NotEmpty(t, sessionCookies(res))
		})
	}
}

func TestBaseURLWithPathIsTrustedOrigin(t *testing.T) {
	app := testutil.NewApp(t, func(d *httpapi.Deps) { d.Config.BaseURL = "https://panel.example/nebla/" })
	app.CreateUser("dummy@example.test", false)
	res := csrfPost(t, app, "/v1/auth/login", loginBody(), "application/json",
		map[string]string{"Origin": "https://panel.example", "Sec-Fetch-Site": "cross-site"}, nil)
	require.Equal(t, http.StatusOK, res.StatusCode, app.Body(res))
}

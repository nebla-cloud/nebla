package httpapi_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"testing"

	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/stretchr/testify/require"
)

type challenge struct {
	ChallengeID string          `json:"challenge_id"`
	Options     json.RawMessage `json:"options"`
}

func decodeJSON[T any](t *testing.T, app *testutil.App, res *http.Response, want int) T {
	t.Helper()
	body := app.Body(res)
	require.Equal(t, want, res.StatusCode, body)
	var v T
	require.NoError(t, json.Unmarshal([]byte(body), &v))
	return v
}

// addPasskey registers a software passkey for cl's user and returns it.
func addPasskey(t *testing.T, app *testutil.App, cl *testutil.Client) *testutil.Authenticator {
	t.Helper()
	ch := decodeJSON[challenge](t, app, cl.Send("POST", "/v1/me/passkeys/begin", map[string]string{"password": testutil.Password}), 200)
	au := testutil.NewAuthenticator(app.URL, [16]byte{})
	res := cl.Send("POST", "/v1/me/passkeys/finish", map[string]any{"challenge_id": ch.ChallengeID, "credential": au.Register(ch.Options)})
	decodeJSON[map[string]any](t, app, res, 201)
	return au
}

func TestPasskeyOpsDisabledOnIP(t *testing.T) {
	app := testutil.NewApp(t)
	app.CreateUser("a@example.com", false)
	cl := app.Login("a@example.com")
	cred := map[string]any{"challenge_id": "x", "credential": map[string]any{}}
	for _, c := range []struct {
		method, path string
		body         any
	}{
		{"GET", "/v1/me/passkeys", nil},
		{"POST", "/v1/me/passkeys/begin", map[string]string{"password": testutil.Password}},
		{"POST", "/v1/me/passkeys/finish", cred},
		{"PUT", "/v1/me/passkeys/1", map[string]string{"name": "x"}},
		{"DELETE", "/v1/me/passkeys/1", nil},
		{"POST", "/v1/auth/passkey/begin", nil},
		{"POST", "/v1/auth/passkey/finish", cred},
	} {
		res := cl.Send(c.method, c.path, c.body)
		require.Equal(t, 404, res.StatusCode, c.method+" "+c.path)
		require.Equal(t, "not_found", errCode(t, app, res))
	}
	m := decodeJSON[map[string]any](t, app, app.Get("/v1/ui/manifest"), 200)
	v, ok := m["passkey_origin"]
	require.True(t, ok)
	require.Nil(t, v)
}

func TestManifestPasskeyOrigin(t *testing.T) {
	app := testutil.NewApp(t, testutil.Localhost)
	m := decodeJSON[map[string]any](t, app, app.Get("/v1/ui/manifest"), 200)
	require.Equal(t, app.URL, m["passkey_origin"])
}

func TestPasskeyRegisterAndLoginOverHTTP(t *testing.T) {
	app := testutil.NewApp(t, testutil.Localhost)
	app.CreateUser("a@example.com", false)
	cl := app.Login("a@example.com")
	ch := decodeJSON[challenge](t, app, cl.Send("POST", "/v1/me/passkeys/begin", map[string]string{"password": testutil.Password}), 200)
	au := testutil.NewAuthenticator(app.URL, [16]byte{})
	res := cl.Send("POST", "/v1/me/passkeys/finish", map[string]any{"challenge_id": ch.ChallengeID, "credential": au.Register(ch.Options)})
	pk := decodeJSON[struct{ Name string }](t, app, res, 201)
	require.NotEmpty(t, pk.Name)
	list := decodeJSON[struct{ Passkeys []map[string]any }](t, app, cl.Get("/v1/me/passkeys"), 200)
	require.Len(t, list.Passkeys, 1)

	jar, _ := cookiejar.New(nil)
	hc := &http.Client{Jar: jar}
	post := func(path string, body any) *http.Response {
		req := newJSONReq(t, app.URL+path, body)
		r, err := hc.Do(req)
		require.NoError(t, err)
		return r
	}
	lc := decodeJSON[challenge](t, app, post("/v1/auth/passkey/begin", nil), 200)
	res = post("/v1/auth/passkey/finish", map[string]any{"challenge_id": lc.ChallengeID, "credential": au.Assert(lc.Options)})
	require.Equal(t, 200, res.StatusCode)
	cookies := res.Cookies()
	require.Len(t, cookies, 1)
	require.NotEmpty(t, cookies[0].Value)
	require.True(t, cookies[0].HttpOnly)
	require.Equal(t, http.SameSiteLaxMode, cookies[0].SameSite)
	require.Equal(t, "/", cookies[0].Path)
	res.Body.Close()
	req, err := http.NewRequest("GET", app.URL+"/v1/me", nil)
	require.NoError(t, err)
	res, err = hc.Do(req)
	require.NoError(t, err)
	require.Equal(t, 200, res.StatusCode)
	res.Body.Close()
}

func TestPasskeyBeginRateLimited(t *testing.T) {
	app := testutil.NewApp(t, testutil.Localhost)
	app.CreateUser("a@example.com", false)
	cl := app.Login("a@example.com")
	b := map[string]string{"password": "wrong-password"}
	var res *http.Response
	for range 20 {
		if res = cl.Send("POST", "/v1/me/passkeys/begin", b); res.StatusCode != 401 {
			break
		}
		res.Body.Close()
	}
	require.Equal(t, 429, res.StatusCode)
	require.Equal(t, "rate_limited", errCode(t, app, res))
}

func TestDeletePasskeyRevokesOthers(t *testing.T) {
	app := testutil.NewApp(t, testutil.Localhost)
	app.CreateUser("a@example.com", false)
	a, b := app.Login("a@example.com"), app.Login("a@example.com")
	addPasskey(t, app, a)
	list := decodeJSON[struct{ Passkeys []struct{ ID int64 } }](t, app, a.Get("/v1/me/passkeys"), 200)
	require.Len(t, list.Passkeys, 1)
	res := a.Send("DELETE", "/v1/me/passkeys/"+itoa(list.Passkeys[0].ID), nil)
	require.Equal(t, 204, res.StatusCode)
	res.Body.Close()
	res = b.Get("/v1/me")
	require.Equal(t, 401, res.StatusCode)
	res.Body.Close()
	res = a.Get("/v1/me")
	require.Equal(t, 200, res.StatusCode)
	res.Body.Close()
}

func TestPasskeyOpsNeedSession(t *testing.T) {
	app := testutil.NewApp(t, testutil.Localhost)
	app.CreateUser("a@example.com", false)
	cl := app.Login("a@example.com")
	tok := cl.CreateToken(cl.CreateProject("p"), "read")
	res := app.TokenClient(tok).Get("/v1/me/passkeys")
	require.Equal(t, 401, res.StatusCode)
	res.Body.Close()
}

func newJSONReq(t *testing.T, url string, body any) *http.Request {
	t.Helper()
	var r *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(t, err)
		r = bytes.NewReader(b)
	} else {
		r = bytes.NewReader(nil)
	}
	req, err := http.NewRequest("POST", url, r)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	return req
}

package httpapi_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/netip"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nebla-cloud/nebla/internal/httpapi"
	"github.com/nebla-cloud/nebla/internal/ratelimit"
	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

func TestRateLimitHeaders(t *testing.T) {
	app := testutil.NewApp(t)
	app.CreateUser("a@example.com", true)
	cl := app.Login("a@example.com")
	for path, want := range map[string]int{"/v1/me": 200, "/v1/nope": 404, "/v1/healthz": 200} {
		res := cl.Get(path)
		require.Equal(t, want, res.StatusCode, path)
		require.Equal(t, "3600", res.Header.Get("RateLimit-Limit"), path)
		require.NotEmpty(t, res.Header.Get("RateLimit-Remaining"), path)
		require.NotEmpty(t, res.Header.Get("RateLimit-Reset"), path)
		require.Empty(t, res.Header.Get("Retry-After"), path)
	}
	res := app.Get("/v1/healthz")
	require.Equal(t, "3600", res.Header.Get("RateLimit-Limit"))
	res = app.Get("/v1/me")
	require.Equal(t, 401, res.StatusCode)
	require.Equal(t, "3600", res.Header.Get("RateLimit-Limit"))
}

func TestRateLimitExceeded(t *testing.T) {
	var offset atomic.Int64
	start := time.Now()
	lim := ratelimit.New(rate.Every(time.Hour/3600), 2)
	lim.SetClock(func() time.Time { return start.Add(time.Duration(offset.Load())) })
	refill := func() { offset.Add(int64(2 * time.Hour)) }
	app := testutil.NewApp(t, func(d *httpapi.Deps) { d.APILimiter = lim })
	app.CreateUser("a@example.com", true)
	admin := app.Login("a@example.com")
	refill()
	proj := admin.CreateProject("p")
	refill()
	ta := admin.CreateToken(proj, "read_write")
	refill()
	tb := admin.CreateToken(proj, "read_write")
	refill()
	tokA, tokB := app.TokenClient(ta), app.TokenClient(tb)

	path := "/v1/servers"
	for i := range 2 {
		res := tokA.Get(path)
		require.Equal(t, 200, res.StatusCode, i)
	}
	res := tokA.Get(path)
	require.Equal(t, http.StatusTooManyRequests, res.StatusCode)
	require.Equal(t, "rate_limit_exceeded", errCode(t, app, res))
	require.Equal(t, "3600", res.Header.Get("RateLimit-Limit"))
	require.Equal(t, "0", res.Header.Get("RateLimit-Remaining"))
	require.Equal(t, "1", res.Header.Get("Retry-After"))
	require.Equal(t, "2", res.Header.Get("RateLimit-Reset"))
	require.Equal(t, 200, tokB.Get(path).StatusCode)
}

func trustLoopback(d *httpapi.Deps) {
	d.Config.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("::1/128")}
}

// fromIP sends a request forwarded for ip and returns its status.
func fromIP(t *testing.T, app *testutil.App, method, path, ip string, body any) int {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(t, err)
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, app.URL+path, r)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", ip)
	res := app.Do(req)
	res.Body.Close()
	return res.StatusCode
}

func TestRateLimitTrustedProxyUsesForwardedIP(t *testing.T) {
	lim := ratelimit.New(rate.Every(time.Hour), 2)
	app := testutil.NewApp(t, trustLoopback, func(d *httpapi.Deps) { d.APILimiter = lim })
	for range 2 {
		require.Equal(t, 200, fromIP(t, app, "GET", "/v1/healthz", "198.51.100.1", nil))
	}
	require.Equal(t, 429, fromIP(t, app, "GET", "/v1/healthz", "198.51.100.1", nil))
	require.Equal(t, 200, fromIP(t, app, "GET", "/v1/healthz", "198.51.100.2", nil))
}

func TestRateLimitUntrustedIgnoresForwardedFor(t *testing.T) {
	lim := ratelimit.New(rate.Every(time.Hour), 2)
	app := testutil.NewApp(t, func(d *httpapi.Deps) { d.APILimiter = lim })
	require.Equal(t, 200, fromIP(t, app, "GET", "/v1/healthz", "198.51.100.1", nil))
	require.Equal(t, 200, fromIP(t, app, "GET", "/v1/healthz", "198.51.100.2", nil))
	require.Equal(t, 429, fromIP(t, app, "GET", "/v1/healthz", "198.51.100.3", nil))
}

func TestLoginLimitTrustedProxyUsesForwardedIP(t *testing.T) {
	app := testutil.NewApp(t, trustLoopback)
	b := map[string]string{"email": "a@example.com", "password": "wrong-password"}
	for range 10 {
		require.Equal(t, 401, fromIP(t, app, "POST", "/v1/auth/login", "198.51.100.1", b))
	}
	require.Equal(t, 429, fromIP(t, app, "POST", "/v1/auth/login", "198.51.100.1", b))
	require.Equal(t, 401, fromIP(t, app, "POST", "/v1/auth/login", "198.51.100.2", b))
}

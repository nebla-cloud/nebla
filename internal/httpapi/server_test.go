package httpapi_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Hexeption/nebla/internal/httpapi"
	"github.com/Hexeption/nebla/internal/testutil"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func body(res *http.Response) string {
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return string(b)
}

func TestHealthz(t *testing.T) {
	app := testutil.NewApp(t)
	res := app.Get("/v1/healthz")
	require.Equal(t, 200, res.StatusCode)
	require.JSONEq(t, `{"status":"ok"}`, app.Body(res))
}

func TestUnknownV1RouteIsJSON404(t *testing.T) {
	res := testutil.NewApp(t).Get("/v1/nope")
	require.Equal(t, 404, res.StatusCode)
	require.JSONEq(t, `{"error":{"code":"not_found","message":"not found"}}`, body(res))
}

func TestSPAFallback(t *testing.T) {
	res := testutil.NewApp(t).Get("/servers/12")
	require.Equal(t, 200, res.StatusCode)
	require.True(t, strings.HasPrefix(res.Header.Get("Content-Type"), "text/html"))
	require.Equal(t, "no-cache", res.Header.Get("Cache-Control"))
	require.Contains(t, body(res), "<title>Nebla</title>")
}

func TestSPAAssetsAreImmutable(t *testing.T) {
	dist := fstest.MapFS{
		"index.html":        {Data: []byte("<!doctype html><title>Nebla</title>")},
		"assets/app-abc.js": {Data: []byte("console.log(1)")},
	}
	h := httpapi.SPAHandler(dist, []byte("placeholder"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/app-abc.js", nil))
	require.Equal(t, 200, rec.Code)
	require.Equal(t, "public, max-age=31536000, immutable", rec.Header().Get("Cache-Control"))

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/servers/1", nil))
	require.Equal(t, 200, rec.Code)
	require.Equal(t, "no-cache", rec.Header().Get("Cache-Control"))
	require.Contains(t, rec.Body.String(), "<title>Nebla</title>")
}

func TestSPAPlaceholderWhenUnbuilt(t *testing.T) {
	ph := []byte("<!doctype html><title>Nebla</title>unbuilt")
	rec := httptest.NewRecorder()
	httpapi.SPAHandler(fstest.MapFS{}, ph).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/servers/1", nil))
	require.Equal(t, 200, rec.Code)
	require.True(t, strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html"))
	require.Equal(t, "no-cache", rec.Header().Get("Cache-Control"))
	require.Equal(t, string(ph), rec.Body.String())
}

func TestHealthzReportsDatabaseDown(t *testing.T) {
	app := testutil.NewApp(t, func(d *httpapi.Deps) {
		p, err := pgxpool.New(context.Background(), "postgres://nebla@127.0.0.1:1/nebla?connect_timeout=1")
		require.NoError(t, err)
		t.Cleanup(p.Close)
		d.Pool = p
	})
	res := app.Get("/v1/healthz")
	require.Equal(t, http.StatusServiceUnavailable, res.StatusCode)
	require.JSONEq(t, `{"status":"unavailable"}`, app.Body(res))
}

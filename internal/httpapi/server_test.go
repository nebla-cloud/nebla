package httpapi_test

import (
	"context"
	"io"
	"net/http"
	"testing"

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
	require.Contains(t, body(res), "<div id=\"root\">")
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

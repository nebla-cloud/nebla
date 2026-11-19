package testutil

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Hexeption/nebla/internal/httpapi"
)

// App is a running API server backed by a throwaway Postgres.
type App struct {
	URL string
	t   *testing.T
}

func NewApp(t *testing.T) *App {
	t.Helper()
	pool := NewDB(t)
	srv := httptest.NewServer(httpapi.New(httpapi.Deps{Pool: pool}))
	t.Cleanup(srv.Close)
	return &App{URL: srv.URL, t: t}
}

func (a *App) Do(req *http.Request) *http.Response {
	a.t.Helper()
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	return res
}

func (a *App) Get(path string) *http.Response {
	a.t.Helper()
	req, err := http.NewRequest(http.MethodGet, a.URL+path, nil)
	if err != nil {
		a.t.Fatal(err)
	}
	return a.Do(req)
}

func (a *App) Body(res *http.Response) string {
	a.t.Helper()
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		a.t.Fatal(err)
	}
	return string(b)
}

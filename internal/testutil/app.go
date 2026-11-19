package testutil

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"

	"github.com/Hexeption/nebla/internal/auth"
	"github.com/Hexeption/nebla/internal/ext"
	"github.com/Hexeption/nebla/internal/httpapi"
	"github.com/jackc/pgx/v5/pgxpool"
)

// App is a running API server backed by a throwaway Postgres.
type App struct {
	URL  string
	Pool *pgxpool.Pool
	Auth *auth.Service
	t    *testing.T
}

// Password is the password used by fixture users.
const Password = "correct-horse-battery"

func NewApp(t *testing.T) *App {
	t.Helper()
	pool := NewDB(t)
	srv := httptest.NewServer(httpapi.New(httpapi.Deps{Pool: pool}))
	t.Cleanup(srv.Close)
	return &App{URL: srv.URL, Pool: pool, Auth: auth.New(pool, ext.Defaults(nil).Registration), t: t}
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

// PostJSON sends an unauthenticated JSON POST.
func (a *App) PostJSON(path string, body any) *http.Response {
	a.t.Helper()
	return a.Do(newReq(a.t, http.MethodPost, a.URL+path, body))
}

// CreateUser inserts a user with the fixture Password; the name equals the email.
func (a *App) CreateUser(email string, admin bool) auth.User {
	a.t.Helper()
	u, err := a.Auth.CreateUser(context.Background(), email, Password, email, admin)
	if err != nil {
		a.t.Fatal(err)
	}
	return u
}

// Client is a cookie-aware HTTP client bound to an App.
type Client struct {
	app *App
	hc  *http.Client
}

func (a *App) NewClient() *Client {
	jar, _ := cookiejar.New(nil)
	return &Client{app: a, hc: &http.Client{Jar: jar}}
}

// Login returns a client logged in as an existing fixture user.
func (a *App) Login(email string) *Client {
	a.t.Helper()
	c := a.NewClient()
	res := c.Send(http.MethodPost, "/v1/auth/login", map[string]string{"email": email, "password": Password})
	if res.StatusCode != http.StatusOK {
		a.t.Fatalf("login %s: status %d: %s", email, res.StatusCode, a.Body(res))
	}
	res.Body.Close()
	return c
}

// LoginAdmin creates an admin and returns a logged-in client for it.
func (a *App) LoginAdmin() *Client {
	a.t.Helper()
	a.CreateUser("admin@example.com", true)
	return a.Login("admin@example.com")
}

// Send performs a request with an optional JSON body.
func (c *Client) Send(method, path string, body any) *http.Response {
	c.app.t.Helper()
	res, err := c.hc.Do(newReq(c.app.t, method, c.app.URL+path, body))
	if err != nil {
		c.app.t.Fatal(err)
	}
	return res
}

func (c *Client) Get(path string) *http.Response { return c.Send(http.MethodGet, path, nil) }

func newReq(t *testing.T, method, url string, body any) *http.Request {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, url, r)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req
}

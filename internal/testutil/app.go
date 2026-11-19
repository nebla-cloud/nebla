package testutil

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/Hexeption/nebla/internal/auth"
	"github.com/Hexeption/nebla/internal/catalog"
	"github.com/Hexeption/nebla/internal/config"
	"github.com/Hexeption/nebla/internal/ent"
	"github.com/Hexeption/nebla/internal/ext"
	"github.com/Hexeption/nebla/internal/httpapi"
	"github.com/Hexeption/nebla/internal/pve/fake"
	"github.com/jackc/pgx/v5/pgxpool"
)

// App is a running API server backed by a throwaway Postgres.
type App struct {
	URL  string
	Pool *pgxpool.Pool
	SQL  *sql.DB
	Ent  *ent.Client
	Auth *auth.Service
	PVE  *fake.Server
	// Catalog is the catalog service the API uses.
	Catalog *catalog.Service
	t       *testing.T
}

// Password is the password used by fixture users.
const Password = "correct-horse-battery"

// NewApp starts the API; opts may adjust Deps (hooks, extra scoped routes) before it is built.
func NewApp(t *testing.T, opts ...func(*httpapi.Deps)) *App {
	t.Helper()
	pool := NewDB(t)
	client, sqlDB := entFor(t, pool)
	deps := httpapi.Deps{Pool: pool, SQL: sqlDB, Ent: client, Config: config.Config{SecretKey: []byte(SecretKey)}}
	for _, o := range opts {
		o(&deps)
	}
	if deps.Catalog == nil {
		deps.Catalog = catalog.New(sqlDB, client, CatalogOptions(deps.CatalogHooks))
	}
	srv := httptest.NewServer(httpapi.New(deps))
	t.Cleanup(srv.Close)
	return &App{URL: srv.URL, Pool: pool, SQL: sqlDB, Ent: client, Auth: auth.New(client, ext.Defaults(nil).Registration),
		PVE: fake.New(t), Catalog: deps.Catalog, t: t}
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
	app    *App
	hc     *http.Client
	header http.Header
}

func (a *App) NewClient() *Client {
	jar, _ := cookiejar.New(nil)
	return &Client{app: a, hc: &http.Client{Jar: jar}, header: http.Header{}}
}

// TokenClient returns a cookieless client that sends Authorization: Bearer token.
func (a *App) TokenClient(token string) *Client {
	c := &Client{app: a, hc: &http.Client{}, header: http.Header{}}
	c.header.Set("Authorization", "Bearer "+token)
	return c
}

// WithProject returns a copy of c, sharing its cookies, that sends the Project-ID header.
func (c *Client) WithProject(id int64) *Client {
	cp := &Client{app: c.app, hc: c.hc, header: c.header.Clone()}
	cp.header.Set(httpapi.ProjectHeader, strconv.FormatInt(id, 10))
	return cp
}

// CreateProject creates a project owned by c's user and returns its id.
func (c *Client) CreateProject(name string) int64 {
	c.app.t.Helper()
	var p struct {
		ID int64 `json:"id"`
	}
	c.decode(c.Send(http.MethodPost, "/v1/projects", map[string]string{"name": name}), http.StatusCreated, &p)
	return p.ID
}

// CreateToken creates an API token (read or read_write) in a project and returns the plaintext.
func (c *Client) CreateToken(projectID int64, permission string) string {
	c.app.t.Helper()
	var tok struct {
		Token string `json:"token"`
	}
	path := "/v1/projects/" + strconv.FormatInt(projectID, 10) + "/tokens"
	c.decode(c.Send(http.MethodPost, path, map[string]string{"name": "test", "permission": permission}), http.StatusCreated, &tok)
	return tok.Token
}

// Invite invites email to a project and returns the raw invite token.
func (c *Client) Invite(projectID int64, email, role string) string {
	c.app.t.Helper()
	var inv struct {
		URL string `json:"invite_url"`
	}
	path := "/v1/projects/" + strconv.FormatInt(projectID, 10) + "/invites"
	c.decode(c.Send(http.MethodPost, path, map[string]string{"email": email, "role": role}), http.StatusCreated, &inv)
	return inv.URL[strings.LastIndex(inv.URL, "/")+1:]
}

func (c *Client) decode(res *http.Response, want int, v any) {
	c.app.t.Helper()
	body := c.app.Body(res)
	if res.StatusCode != want {
		c.app.t.Fatalf("%s %s: status %d: %s", res.Request.Method, res.Request.URL.Path, res.StatusCode, body)
	}
	if err := json.Unmarshal([]byte(body), v); err != nil {
		c.app.t.Fatal(err)
	}
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
	req := newReq(c.app.t, method, c.app.URL+path, body)
	for k, v := range c.header {
		req.Header[k] = v
	}
	res, err := c.hc.Do(req)
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

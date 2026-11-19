package testutil

import (
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/Hexeption/nebla/internal/httpapi"
	"github.com/Hexeption/nebla/internal/jobs"
	"github.com/Hexeption/nebla/internal/pve/fake"
	"github.com/Hexeption/nebla/internal/servers"
	"github.com/riverqueue/river"
)

// ServerWorkers is a WorkerSetup that registers the server job workers.
func ServerWorkers(w *river.Workers, deps *httpapi.Deps, _ *fake.Server) {
	servers.RegisterWorkers(w, servers.WorkerOptions{SQL: deps.SQL, Ent: deps.Ent, Catalog: deps.Catalog, Bus: deps.Bus,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), PollInterval: 20 * time.Millisecond})
}

// QueueOnlyJobs is a NewApp option whose job runtime inserts jobs but never works them.
func QueueOnlyJobs(t testing.TB) func(*httpapi.Deps) {
	return func(d *httpapi.Deps) {
		j, err := jobs.New(d.Pool, d.SQL, nil)
		if err != nil {
			t.Fatal(err)
		}
		d.Jobs = j
	}
}

// CreateIPPool creates an IP pool on bridge vmbr0 via the admin API.
func (c *Client) CreateIPPool(locationID int64, family, cidr, gateway, start, end string) int64 {
	c.app.t.Helper()
	body := map[string]any{"location_id": locationID, "family": family, "bridge": "vmbr0", "cidr": cidr, "gateway": gateway,
		"dns": []string{gateway}, "range_start": start, "range_end": end}
	return c.createID(http.MethodPost, "/v1/admin/ip_pools", body)
}

// SetQuota sets a project's quota via the admin API.
func (c *Client) SetQuota(projectID int64, maxServers, maxVCPU, maxMemoryMB, maxDiskGB, maxIPs int) {
	c.app.t.Helper()
	body := map[string]int{"max_servers": maxServers, "max_vcpu": maxVCPU, "max_memory_mb": maxMemoryMB, "max_disk_gb": maxDiskGB, "max_ips": maxIPs}
	res := c.Send(http.MethodPut, "/v1/admin/projects/"+strconv.FormatInt(projectID, 10)+"/quota", body)
	if res.StatusCode != http.StatusOK {
		c.app.t.Fatalf("set quota: status %d: %s", res.StatusCode, c.app.Body(res))
	}
	res.Body.Close()
}

// ServerEnv is a seeded catalog plus a project ready to create servers in.
type ServerEnv struct {
	App   *App
	Admin *Client
	// C acts on Project as its owner.
	C        *Client
	Project  int64
	Location int64
	// Type is "small": 2 cores, 2048 MB, 20 GB.
	Type  int64
	Image int64
	// Pool4 hands out 10.0.0.10-10.0.0.20; Pool6 2001:db8::10-2001:db8::ff.
	Pool4, Pool6 int64
}

// NewServerEnv seeds app's fake Proxmox and creates location eu-1, type small, image ubuntu, IP pools and project p.
func NewServerEnv(app *App) *ServerEnv {
	app.t.Helper()
	app.SeedPVE()
	admin := app.LoginAdmin()
	e := &ServerEnv{App: app, Admin: admin}
	e.Location = admin.CreateLocation("eu-1")
	e.Type = admin.CreateServerType("small", 2, 2048, 20)
	e.Image = admin.CreateImage("ubuntu", e.Location, TemplateVMID)
	e.Pool4 = admin.CreateIPPool(e.Location, "ipv4", "10.0.0.0/24", "10.0.0.1", "10.0.0.10", "10.0.0.20")
	e.Pool6 = admin.CreateIPPool(e.Location, "ipv6", "2001:db8::/64", "2001:db8::1", "2001:db8::10", "2001:db8::ff")
	e.Project = admin.CreateProject("p")
	e.C = admin.WithProject(e.Project)
	return e
}

// CreatedServer is the ids from a successful POST /v1/servers.
type CreatedServer struct {
	ServerID, ActionID int64
}

// CreateServer creates a server in eu-1 with type small and image ubuntu; extra overrides body fields.
func (e *ServerEnv) CreateServer(name string, extra map[string]any) CreatedServer {
	e.App.t.Helper()
	body := map[string]any{"name": name, "server_type": "small", "image": "ubuntu", "location": "eu-1"}
	for k, v := range extra {
		body[k] = v
	}
	var out struct {
		Server struct {
			ID int64 `json:"id"`
		} `json:"server"`
		Action struct {
			ID int64 `json:"id"`
		} `json:"action"`
	}
	e.C.decode(e.C.Send(http.MethodPost, "/v1/servers", body), http.StatusCreated, &out)
	return CreatedServer{ServerID: out.Server.ID, ActionID: out.Action.ID}
}

// Copyright (C) 2026 The Nebla contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package testutil

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/db"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/ipaddress"
	"github.com/nebla-cloud/nebla/internal/ent/server"
	"github.com/nebla-cloud/nebla/internal/httpapi"
	"github.com/nebla-cloud/nebla/internal/ipam"
	"github.com/nebla-cloud/nebla/internal/jobs"
	"github.com/nebla-cloud/nebla/internal/pve/fake"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/riverqueue/river"
)

// ServerWorkers is a WorkerSetup that registers the server job workers.
func ServerWorkers(w *river.Workers, deps *httpapi.Deps, _ *fake.Server) {
	servers.RegisterWorkers(w, servers.WorkerOptions{SQL: deps.SQL, Ent: deps.Ent, Catalog: deps.Catalog, Bus: deps.Bus,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), PollInterval: 20 * time.Millisecond,
		Jobs: func() *jobs.Jobs { return deps.Jobs }})
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

// CreateSegmentPool creates an IP pool on bridge, tagged with vlan when it is not nil, via the admin API.
func (c *Client) CreateSegmentPool(locationID int64, family, bridge string, vlan *int, cidr, gateway, start, end string) int64 {
	c.app.t.Helper()
	body := map[string]any{"location_id": locationID, "family": family, "bridge": bridge, "cidr": cidr, "gateway": gateway,
		"dns": []string{gateway}, "range_start": start, "range_end": end}
	if vlan != nil {
		body["vlan_tag"] = *vlan
	}
	return c.createID(http.MethodPost, "/v1/admin/ip_pools", body)
}

// PrimaryIPOn creates an unassigned primary IP in the project on a pool of seg in eu-1.
func (e *ServerEnv) PrimaryIPOn(family string, seg ipam.Segment) (int64, string) {
	e.App.t.Helper()
	ctx := context.Background()
	var row *ent.IPAddress
	err := db.WithTx(ctx, e.App.SQL, func(_ *sql.Tx, etx *ent.Client) error {
		a, allocID, err := ipam.AllocatePrimary(ctx, etx, e.Location, family, &seg)
		if err != nil {
			return err
		}
		row, err = etx.IPAddress.Create().SetKind(ipaddress.KindPrimary).SetProjectID(e.Project).SetLocationID(e.Location).
			SetAllocationID(allocID).SetFamily(family).SetIP(a.IP.String()).SetName("seg-" + strconv.FormatInt(allocID, 10)).
			SetLabels(map[string]string{}).Save(ctx)
		return err
	})
	if err != nil {
		e.App.t.Fatal(err)
	}
	return row.ID, row.IP
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

// RunningServer creates a server and settles it by hand: its action succeeded and it runs as a fake VM on pve1.
func (e *ServerEnv) RunningServer(name string) CreatedServer {
	e.App.t.Helper()
	ctx := context.Background()
	s := e.CreateServer(name, nil)
	if err := actions.Succeed(ctx, e.App.Ent, s.ActionID); err != nil {
		e.App.t.Fatal(err)
	}
	vmid := 100 + int(s.ServerID)
	e.App.PVE.AddVM("pve1", vmid, name)
	e.App.PVE.SetVMConfig(vmid, "tags", servers.TagNebla+";"+servers.ServerTag(s.ServerID))
	e.App.PVE.SetVMStatus(vmid, "running")
	if err := e.App.Ent.Server.UpdateOneID(s.ServerID).SetVmid(vmid).SetNode("pve1").SetStatus(server.StatusRunning).Exec(ctx); err != nil {
		e.App.t.Fatal(err)
	}
	return s
}

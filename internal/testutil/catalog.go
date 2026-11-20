package testutil

import (
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/nebla-cloud/nebla/internal/catalog"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/httpapi"
	"github.com/nebla-cloud/nebla/internal/osimages"
	"github.com/nebla-cloud/nebla/internal/pve/fake"
)

// SecretKey is the PANEL_SECRET_KEY used by test apps.
const SecretKey = "nebla-test-secret-key-0123456789abcdef"

// TemplateVMID is the shared-storage template SeedPVE adds on pve1.
const TemplateVMID = 9000

// SeedPVE gives pv nodes pve1 and pve2, each with local (iso), local-lvm (images) and shared ceph (images), plus template 9000 on ceph.
func SeedPVE(pv *fake.Server) {
	for _, n := range []string{"pve1", "pve2"} {
		pv.AddNode(n, 16, 65536)
		pv.AddStorage(n, "local", []string{"iso", "vztmpl"}, false)
		pv.AddStorage(n, "local-lvm", []string{"images", "rootdir"}, false)
		pv.AddStorage(n, "ceph", []string{"images"}, true)
	}
	pv.AddTemplate("pve1", TemplateVMID, "ceph")
}

// SeedPVE seeds the app's fake Proxmox; see SeedPVE.
func (a *App) SeedPVE() { SeedPVE(a.PVE) }

// CatalogOptions are the catalog options test apps use: the test key and a fast task poll.
func CatalogOptions(h catalog.Hooks) catalog.Options {
	return catalog.Options{SecretKey: []byte(SecretKey), PollInterval: 5 * time.Millisecond, Hooks: h}
}

// NewCatalog starts a migrated Postgres and returns a catalog service over it.
func NewCatalog(t *testing.T, h catalog.Hooks) (*catalog.Service, *ent.Client) {
	t.Helper()
	client, sqlDB := entFor(t, NewDB(t))
	return catalog.New(sqlDB, client, CatalogOptions(h)), client
}

// LocationInput points a location at pv using its seeded storages (ceph for disks, local for ISOs).
func LocationInput(pv *fake.Server, name string) catalog.LocationInput {
	c := pv.Conn()
	return catalog.LocationInput{Name: name, DiskStorage: "ceph", ISOStorage: "local", PVE: catalog.PVEInput{
		URL: c.BaseURL, TokenID: c.TokenID, TokenSecret: new(c.Secret), CAFingerprint: c.CAFingerprint}}
}

// CreateLocation creates a location on the app's fake Proxmox via the admin API; call SeedPVE first.
func (c *Client) CreateLocation(name string) int64 {
	c.app.t.Helper()
	conn := c.app.PVE.Conn()
	body := map[string]any{"name": name, "disk_storage": "ceph", "iso_storage": "local", "pve": map[string]any{
		"url": conn.BaseURL, "token_id": conn.TokenID, "token_secret": conn.Secret, "ca_fingerprint": conn.CAFingerprint}}
	return c.createID(http.MethodPost, "/v1/admin/locations", body)
}

// CreateServerType creates a server type offered in every location via the admin API.
func (c *Client) CreateServerType(name string, cores, memoryMB, diskGB int) int64 {
	c.app.t.Helper()
	body := map[string]any{"name": name, "cores": cores, "memory_mb": memoryMB, "disk_gb": diskGB}
	return c.createID(http.MethodPost, "/v1/admin/server_types", body)
}

// CreateImage creates an image with one node-less template in a location via the admin API.
func (c *Client) CreateImage(name string, locationID int64, vmid int) int64 {
	c.app.t.Helper()
	body := map[string]any{"name": name, "os_flavor": "ubuntu", "os_version": "24.04",
		"templates": []map[string]any{{"location_id": locationID, "vmid": vmid}}}
	return c.createID(http.MethodPost, "/v1/admin/images", body)
}

func (c *Client) createID(method, path string, body any) int64 {
	c.app.t.Helper()
	var v struct {
		ID int64 `json:"id"`
	}
	c.decode(c.Send(method, path, body), http.StatusCreated, &v)
	return v.ID
}

// WithOSImages gives the app an OS image service over entries and a fake upstream client.
func WithOSImages(entries []osimages.Entry, hc *http.Client) func(*httpapi.Deps) {
	return func(d *httpapi.Deps) {
		_, d.CatalogHooks = httpapi.ServerHooks(d.ProjectHooks, d.CatalogHooks)
		if d.Catalog == nil {
			d.Catalog = catalog.New(d.SQL, d.Ent, CatalogOptions(d.CatalogHooks))
		}
		d.OSImages = osimages.New(osimages.Options{SQL: d.SQL, Ent: d.Ent, Catalog: d.Catalog, Entries: entries, HTTP: hc,
			Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	}
}

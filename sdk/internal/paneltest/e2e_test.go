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

//go:build e2e

package paneltest

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nebla-cloud/nebla/internal/backups"
	"github.com/nebla-cloud/nebla/internal/catalog"
	"github.com/nebla-cloud/nebla/internal/httpapi"
	"github.com/nebla-cloud/nebla/internal/jobs"
	"github.com/nebla-cloud/nebla/internal/pve"
	"github.com/nebla-cloud/nebla/internal/pve/fake"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/nebla-cloud/nebla/internal/volumes"
	"github.com/nebla-cloud/nebla/sdk/nebla"
	"github.com/riverqueue/river"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

// Only point this at a disposable or nested Proxmox VE: it creates and destroys real VMs.

const e2eActionTimeout = 10 * time.Minute

type e2eTarget struct {
	url, tokenID, secret, fingerprint string
	insecure                          bool
	templateVMID                      int
	node, cidr, gateway, bridge       string
	rangeStart, rangeEnd              string
	diskStorage, isoStorage           string
}

func loadE2ETarget(t *testing.T) e2eTarget {
	t.Helper()
	if os.Getenv("E2E_PVE_URL") == "" {
		t.Skip("E2E_PVE_URL is unset; skipping the Proxmox SDK smoke test")
	}
	need := func(k string) string {
		v := os.Getenv(k)
		if v == "" {
			t.Fatalf("%s is required when E2E_PVE_URL is set", k)
		}
		return v
	}
	tg := e2eTarget{
		url: need("E2E_PVE_URL"), tokenID: need("E2E_PVE_TOKEN_ID"), secret: need("E2E_PVE_SECRET"),
		fingerprint: os.Getenv("E2E_PVE_FINGERPRINT"), insecure: os.Getenv("E2E_PVE_INSECURE") == "1",
		node: need("E2E_NODE"), cidr: need("E2E_POOL_CIDR"), gateway: need("E2E_GATEWAY"), bridge: need("E2E_BRIDGE"),
		diskStorage: e2eEnvOr("E2E_DISK_STORAGE", "local-lvm"), isoStorage: e2eEnvOr("E2E_ISO_STORAGE", "local"),
	}
	if tg.fingerprint == "" && !tg.insecure {
		t.Fatal("E2E_PVE_FINGERPRINT is required (pvenode cert info); set E2E_PVE_INSECURE=1 to skip TLS verification")
	}
	vmid, err := strconv.Atoi(need("E2E_TEMPLATE_VMID"))
	require.NoError(t, err, "E2E_TEMPLATE_VMID")
	tg.templateVMID = vmid
	var ok bool
	tg.rangeStart, tg.rangeEnd, ok = strings.Cut(need("E2E_RANGE"), "-")
	require.True(t, ok, "E2E_RANGE must be <start>-<end>")
	tg.rangeStart, tg.rangeEnd = strings.TrimSpace(tg.rangeStart), strings.TrimSpace(tg.rangeEnd)
	return tg
}

func e2eEnvOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// e2ePost sends an admin or project request through the test client and decodes a JSON response into out.
func e2ePost(t *testing.T, app *testutil.App, c *testutil.Client, path string, body any, out any) {
	t.Helper()
	res := c.Send(http.MethodPost, path, body)
	b := app.Body(res)
	require.Equal(t, http.StatusCreated, res.StatusCode, "POST %s: %s", path, b)
	if out != nil {
		require.NoError(t, json.Unmarshal([]byte(b), out), b)
	}
}

func e2eSSHKey(t *testing.T) string {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	pk, err := ssh.NewPublicKey(pub)
	require.NoError(t, err)
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pk))) + " nebla-e2e"
}

// e2eSetup wires a panel to the target (location e2e, type e2e-small, image e2e-debian, an IP pool, project e2e)
// and returns an SDK client for the project and which public address families to request.
func e2eSetup(t *testing.T, tg e2eTarget) (*nebla.Client, pve.Client, map[string]bool) {
	t.Helper()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	workers := func(w *river.Workers, d *httpapi.Deps, _ *fake.Server) {
		servers.RegisterWorkers(w, servers.WorkerOptions{SQL: d.SQL, Ent: d.Ent, Catalog: d.Catalog, Bus: d.Bus, Logger: log,
			PollInterval: time.Second, Jobs: func() *jobs.Jobs { return d.Jobs }})
		volumes.RegisterWorkers(w, volumes.WorkerOptions{SQL: d.SQL, Ent: d.Ent, Catalog: d.Catalog, Bus: d.Bus, Logger: log})
		backups.RegisterWorkers(w, backups.WorkerOptions{SQL: d.SQL, Ent: d.Ent, Catalog: d.Catalog, Bus: d.Bus, Logger: log,
			Jobs: func() *jobs.Jobs { return d.Jobs }})
	}
	app := testutil.NewAppWithWorkers(t, workers, func(d *httpapi.Deps) {
		d.Logger = log
		d.ProjectHooks, d.CatalogHooks = httpapi.ServerHooks(d.ProjectHooks, d.CatalogHooks)
		d.Catalog = catalog.New(d.SQL, d.Ent, catalog.Options{SecretKey: []byte(testutil.SecretKey), PollInterval: time.Second,
			Hooks: d.CatalogHooks})
	})

	const email = "e2e-admin@example.com"
	res := app.PostJSON("/v1/setup", map[string]string{"email": email, "password": testutil.Password, "name": "e2e"})
	require.Equal(t, http.StatusCreated, res.StatusCode, app.Body(res))
	res.Body.Close()
	admin := app.Login(email)

	var id struct {
		ID int64 `json:"id"`
	}
	e2ePost(t, app, admin, "/v1/admin/locations", map[string]any{
		"name": "e2e", "disk_storage": tg.diskStorage, "iso_storage": tg.isoStorage,
		"pve": map[string]any{"url": tg.url, "token_id": tg.tokenID, "token_secret": tg.secret,
			"ca_fingerprint": tg.fingerprint, "insecure_tls": tg.insecure && tg.fingerprint == ""},
	}, &id)
	loc := id.ID
	e2ePost(t, app, admin, "/v1/admin/server_types",
		map[string]any{"name": "e2e-small", "cores": 1, "memory_mb": 1024, "disk_gb": 10}, nil)
	e2ePost(t, app, admin, "/v1/admin/images", map[string]any{"name": "e2e-debian", "os_flavor": "debian",
		"os_version": "13", "templates": []map[string]any{{"location_id": loc, "node": tg.node, "vmid": tg.templateVMID}}}, nil)
	family, pubNet := "ipv4", map[string]bool{"ipv4": true, "ipv6": false}
	if p, err := netip.ParsePrefix(tg.cidr); err == nil && p.Addr().Is6() {
		family, pubNet = "ipv6", map[string]bool{"ipv4": false, "ipv6": true}
	}
	e2ePost(t, app, admin, "/v1/admin/ip_pools", map[string]any{"location_id": loc, "family": family,
		"bridge": tg.bridge, "cidr": tg.cidr, "gateway": tg.gateway, "dns": []string{tg.gateway},
		"range_start": tg.rangeStart, "range_end": tg.rangeEnd}, nil)

	proj := admin.CreateProject("e2e")
	e2ePost(t, app, admin.WithProject(proj), "/v1/ssh_keys",
		map[string]string{"name": "e2e", "public_key": e2eSSHKey(t)}, nil)

	token := admin.CreateToken(proj, "read_write")
	client := nebla.NewClient(nebla.WithEndpoint(app.URL), nebla.WithToken(token))
	conn := pve.Conn{BaseURL: tg.url, TokenID: tg.tokenID, Secret: tg.secret, CAFingerprint: tg.fingerprint,
		InsecureTLS: tg.insecure && tg.fingerprint == ""}
	return client, pve.New(conn), pubNet
}

func e2eTaggedVMs(t *testing.T, pc pve.Client, serverID int64) []pve.VM {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	vms, err := pc.VMs(ctx)
	require.NoError(t, err)
	tag := servers.ServerTag(serverID)
	return slices.DeleteFunc(vms, func(v pve.VM) bool {
		return !slices.Contains(strings.FieldsFunc(v.Tags, func(r rune) bool { return r == ';' || r == ',' || r == ' ' }), tag)
	})
}

// e2eWait waits for every action to finish, failing the test on an error or the 10-minute limit.
func e2eWait(t *testing.T, c *nebla.Client, actions ...nebla.Action) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), e2eActionTimeout)
	defer cancel()
	require.NoError(t, c.Actions.WaitFor(ctx, actions))
}

// e2eDelete deletes a resource and waits for its action, failing the test on any error.
func e2eDelete(t *testing.T, c *nebla.Client, del func(context.Context) (*nebla.Action, error)) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), e2eActionTimeout)
	defer cancel()
	a, err := del(ctx)
	require.NoError(t, err)
	if a != nil {
		e2eWait(t, c, *a)
	}
}

// e2eCleanup best-effort deletes a resource left behind by a failed run, retrying while another action holds it.
func e2eCleanup(t *testing.T, c *nebla.Client, what string, del func(context.Context) (*nebla.Action, error)) {
	deadline := time.Now().Add(e2eActionTimeout)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), e2eActionTimeout)
		a, err := del(ctx)
		if err == nil && a != nil {
			err = c.Actions.WaitFor(ctx, []nebla.Action{*a})
		}
		cancel()
		if err == nil || errors.Is(err, nebla.ErrNotFound) {
			return
		}
		t.Logf("cleanup delete %s: %v; retrying", what, err)
		time.Sleep(10 * time.Second)
	}
	t.Errorf("cleanup: %s was not deleted within %s", what, e2eActionTimeout)
}

// TestSDKSmoke drives the Go SDK against the panel and the target Proxmox.
func TestSDKSmoke(t *testing.T) {
	tg := loadE2ETarget(t)
	c, pc, pubNet := e2eSetup(t, tg)
	ctx := context.Background()
	stamp := time.Now().Unix()

	var serverID, volumeID, snapshotID int64
	t.Cleanup(func() {
		if snapshotID != 0 {
			e2eCleanup(t, c, "snapshot", func(ctx context.Context) (*nebla.Action, error) { return c.Snapshots.Delete(ctx, snapshotID) })
		}
		if volumeID != 0 {
			e2eCleanup(t, c, "volume", func(ctx context.Context) (*nebla.Action, error) { return c.Volumes.Delete(ctx, volumeID) })
		}
		if serverID != 0 {
			e2eCleanup(t, c, "server", func(ctx context.Context) (*nebla.Action, error) { return c.Servers.Delete(ctx, serverID) })
			if left := e2eTaggedVMs(t, pc, serverID); len(left) > 0 {
				t.Errorf("VMs tagged %s left on the target: %+v", servers.ServerTag(serverID), left)
			}
		}
	})

	opts := nebla.ServerCreateOpts{Name: fmt.Sprintf("nebla-sdk-e2e-%d", stamp), ServerType: "e2e-small", Image: "e2e-debian",
		Location: "e2e", SSHKeys: []string{"e2e"}, PublicIPv4: nebla.NoPublicIP(), PublicIPv6: nebla.NoPublicIP()}
	if pubNet["ipv4"] {
		opts.PublicIPv4 = nebla.NewPublicIP()
	}
	if pubNet["ipv6"] {
		opts.PublicIPv6 = nebla.NewPublicIP()
	}
	created, err := c.Servers.Create(ctx, opts)
	require.NoError(t, err)
	serverID = created.Server.ID
	e2eWait(t, c, append([]nebla.Action{created.Action}, created.NextActions...)...)
	srv, err := c.Servers.Get(ctx, serverID)
	require.NoError(t, err)
	require.Equal(t, nebla.ServerStatusRunning, srv.Status)
	require.Len(t, e2eTaggedVMs(t, pc, serverID), 1)

	a, err := c.Servers.Reboot(ctx, serverID)
	require.NoError(t, err)
	e2eWait(t, c, *a)
	srv, err = c.Servers.Get(ctx, serverID)
	require.NoError(t, err)
	require.Equal(t, nebla.ServerStatusRunning, srv.Status)

	vol, err := c.Volumes.Create(ctx, nebla.VolumeCreateOpts{Name: fmt.Sprintf("nebla-sdk-e2e-vol-%d", stamp), Size: 1, Location: "e2e"})
	require.NoError(t, err)
	volumeID = vol.Volume.ID
	e2eWait(t, c, vol.Action)
	a, err = c.Volumes.Attach(ctx, volumeID, nebla.VolumeAttachOpts{Server: strconv.FormatInt(serverID, 10)})
	require.NoError(t, err)
	e2eWait(t, c, *a)
	a, err = c.Volumes.Resize(ctx, volumeID, 2)
	require.NoError(t, err)
	e2eWait(t, c, *a)
	v, err := c.Volumes.Get(ctx, volumeID)
	require.NoError(t, err)
	require.Equal(t, 2, v.Size)

	snap, err := c.Snapshots.Create(ctx, nebla.SnapshotCreateOpts{Server: strconv.FormatInt(serverID, 10),
		Name: fmt.Sprintf("nebla-sdk-e2e-snap-%d", stamp)})
	require.NoError(t, err)
	snapshotID = snap.Snapshot.ID
	e2eWait(t, c, snap.Action)

	e2eDelete(t, c, func(ctx context.Context) (*nebla.Action, error) { return c.Snapshots.Delete(ctx, snapshotID) })
	e2eDelete(t, c, func(ctx context.Context) (*nebla.Action, error) { return c.Volumes.Delete(ctx, volumeID) })
	e2eDelete(t, c, func(ctx context.Context) (*nebla.Action, error) { return c.Servers.Delete(ctx, serverID) })
	require.Empty(t, e2eTaggedVMs(t, pc, serverID))

	_, err = c.Snapshots.Get(ctx, snapshotID)
	require.ErrorIs(t, err, nebla.ErrNotFound)
	_, err = c.Volumes.Get(ctx, volumeID)
	require.ErrorIs(t, err, nebla.ErrNotFound)
	_, err = c.Servers.Get(ctx, serverID)
	require.ErrorIs(t, err, nebla.ErrNotFound)
}

//go:build e2e

// Only point this at a disposable or nested Proxmox VE: it creates, rebuilds and destroys real VMs.
package e2e_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
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

	"github.com/coder/websocket"
	"github.com/nebla-cloud/nebla/internal/catalog"
	"github.com/nebla-cloud/nebla/internal/httpapi"
	"github.com/nebla-cloud/nebla/internal/osimages"
	"github.com/nebla-cloud/nebla/internal/pve"
	"github.com/nebla-cloud/nebla/internal/pve/fake"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/riverqueue/river"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

const actionTimeout = 10 * time.Minute

type target struct {
	url, tokenID, secret, fingerprint string
	insecure                          bool
	templateVMID                      int
	node, cidr, gateway, bridge       string
	rangeStart, rangeEnd              string
	diskStorage, isoStorage           string
	importStorage                     string
}

func loadTarget(t *testing.T) target {
	t.Helper()
	if os.Getenv("E2E_PVE_URL") == "" {
		t.Skip("E2E_PVE_URL is unset; skipping the Proxmox smoke test")
	}
	need := func(k string) string {
		v := os.Getenv(k)
		if v == "" {
			t.Fatalf("%s is required when E2E_PVE_URL is set", k)
		}
		return v
	}
	tg := target{
		url: need("E2E_PVE_URL"), tokenID: need("E2E_PVE_TOKEN_ID"), secret: need("E2E_PVE_SECRET"),
		fingerprint: os.Getenv("E2E_PVE_FINGERPRINT"), insecure: os.Getenv("E2E_PVE_INSECURE") == "1",
		node: need("E2E_NODE"), cidr: need("E2E_POOL_CIDR"), gateway: need("E2E_GATEWAY"), bridge: need("E2E_BRIDGE"),
		diskStorage: envOr("E2E_DISK_STORAGE", "local-lvm"), isoStorage: envOr("E2E_ISO_STORAGE", "local"),
		importStorage: envOr("E2E_IMPORT_STORAGE", "local"),
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

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func (tg target) conn() pve.Conn {
	return pve.Conn{BaseURL: tg.url, TokenID: tg.tokenID, Secret: tg.secret, CAFingerprint: tg.fingerprint,
		InsecureTLS: tg.insecure && tg.fingerprint == ""}
}

// call sends a JSON request and decodes the response, failing unless the status is want.
func call(t *testing.T, app *testutil.App, c *testutil.Client, method, path string, body any, want int, out any) {
	t.Helper()
	res := c.Send(method, path, body)
	b := app.Body(res)
	require.Equal(t, want, res.StatusCode, "%s %s: %s", method, path, b)
	if out != nil {
		require.NoError(t, json.Unmarshal([]byte(b), out), b)
	}
}

type action struct {
	ID     int64  `json:"id"`
	Status string `json:"status"`
	Error  *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type actionBody struct {
	Action action `json:"action"`
}

func waitAction(t *testing.T, app *testutil.App, c *testutil.Client, id int64) {
	t.Helper()
	deadline := time.Now().Add(actionTimeout)
	for {
		var a action
		call(t, app, c, http.MethodGet, fmt.Sprintf("/v1/actions/%d", id), nil, http.StatusOK, &a)
		switch a.Status {
		case "success":
			return
		case "error":
			t.Fatalf("action %d failed: %+v", id, a.Error)
		case "running":
		default:
			t.Fatalf("action %d has unexpected status %q", id, a.Status)
		}
		if time.Now().After(deadline) {
			t.Fatalf("action %d still running after %s", id, actionTimeout)
		}
		time.Sleep(2 * time.Second)
	}
}

func serverStatus(t *testing.T, app *testutil.App, c *testutil.Client, id int64) string {
	t.Helper()
	var s struct {
		Status string `json:"status"`
	}
	call(t, app, c, http.MethodGet, fmt.Sprintf("/v1/servers/%d", id), nil, http.StatusOK, &s)
	return s.Status
}

func waitStatus(t *testing.T, app *testutil.App, c *testutil.Client, id int64, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Minute)
	for st := serverStatus(t, app, c, id); st != want; st = serverStatus(t, app, c, id) {
		if time.Now().After(deadline) {
			t.Fatalf("server %d is %s, want %s", id, st, want)
		}
		time.Sleep(2 * time.Second)
	}
}

func taggedVMs(t *testing.T, pc pve.Client, serverID int64) []pve.VM {
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

func sshKey(t *testing.T) string {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	pk, err := ssh.NewPublicKey(pub)
	require.NoError(t, err)
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pk))) + " nebla-e2e"
}

// env is a panel wired to the target with location e2e, type e2e-small, image e2e-debian, an IP pool and project e2e.
type env struct {
	tg     target
	app    *testutil.App
	admin  *testutil.Client
	c      *testutil.Client
	proj   int64
	pc     pve.Client
	pubNet map[string]bool
}

func newEnv(t *testing.T) env {
	tg := loadTarget(t)
	// Job and Proxmox logs go to stderr so a failed run shows how far each step got.
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	workers := func(w *river.Workers, d *httpapi.Deps, _ *fake.Server) {
		servers.RegisterWorkers(w, servers.WorkerOptions{SQL: d.SQL, Ent: d.Ent, Catalog: d.Catalog, Bus: d.Bus, Logger: log,
			PollInterval: time.Second})
	}
	app := testutil.NewAppWithWorkers(t, workers, func(d *httpapi.Deps) {
		d.Logger = log
		d.ProjectHooks, d.CatalogHooks = httpapi.ServerHooks(d.ProjectHooks, d.CatalogHooks)
		d.Catalog = catalog.New(d.SQL, d.Ent, catalog.Options{SecretKey: []byte(testutil.SecretKey), PollInterval: time.Second,
			Hooks: d.CatalogHooks})
	})
	pc := pve.New(tg.conn())

	const email = "e2e-admin@example.com"
	res := app.PostJSON("/v1/setup", map[string]string{"email": email, "password": testutil.Password, "name": "e2e"})
	require.Equal(t, http.StatusCreated, res.StatusCode, app.Body(res))
	res.Body.Close()
	admin := app.Login(email)

	var id struct {
		ID int64 `json:"id"`
	}
	call(t, app, admin, http.MethodPost, "/v1/admin/locations", map[string]any{
		"name": "e2e", "disk_storage": tg.diskStorage, "iso_storage": tg.isoStorage,
		"pve": map[string]any{"url": tg.url, "token_id": tg.tokenID, "token_secret": tg.secret,
			"ca_fingerprint": tg.fingerprint, "insecure_tls": tg.insecure && tg.fingerprint == ""},
	}, http.StatusCreated, &id)
	loc := id.ID
	call(t, app, admin, http.MethodPost, "/v1/admin/server_types",
		map[string]any{"name": "e2e-small", "cores": 1, "memory_mb": 1024, "disk_gb": 10}, http.StatusCreated, nil)
	call(t, app, admin, http.MethodPost, "/v1/admin/images", map[string]any{"name": "e2e-debian", "os_flavor": "debian",
		"os_version": "13", "templates": []map[string]any{{"location_id": loc, "node": tg.node, "vmid": tg.templateVMID}}},
		http.StatusCreated, nil)
	family, pubNet := "ipv4", map[string]bool{"ipv4": true, "ipv6": false}
	if p, err := netip.ParsePrefix(tg.cidr); err == nil && p.Addr().Is6() {
		family, pubNet = "ipv6", map[string]bool{"ipv4": false, "ipv6": true}
	}
	call(t, app, admin, http.MethodPost, "/v1/admin/ip_pools", map[string]any{"location_id": loc, "family": family,
		"bridge": tg.bridge, "cidr": tg.cidr, "gateway": tg.gateway, "dns": []string{tg.gateway},
		"range_start": tg.rangeStart, "range_end": tg.rangeEnd}, http.StatusCreated, nil)

	proj := admin.CreateProject("e2e")
	c := admin.WithProject(proj)
	call(t, app, c, http.MethodPost, "/v1/ssh_keys", map[string]string{"name": "e2e", "public_key": sshKey(t)},
		http.StatusCreated, nil)

	return env{tg: tg, app: app, admin: admin, c: c, proj: proj, pc: pc, pubNet: pubNet}
}

func TestSmoke(t *testing.T) {
	e := newEnv(t)
	app, c, pc, pubNet := e.app, e.c, e.pc, e.pubNet
	var created struct {
		Server struct {
			ID int64 `json:"id"`
		} `json:"server"`
		Action action `json:"action"`
	}
	name := fmt.Sprintf("nebla-e2e-%d", time.Now().Unix())
	call(t, app, c, http.MethodPost, "/v1/servers", map[string]any{"name": name, "server_type": "e2e-small",
		"image": "e2e-debian", "location": "e2e", "ssh_keys": []string{"e2e"}, "public_net": pubNet},
		http.StatusCreated, &created)
	sid := created.Server.ID
	deleted := false
	t.Cleanup(func() {
		if !deleted {
			cleanupServer(t, app, c, sid)
		}
		if left := taggedVMs(t, pc, sid); len(left) > 0 {
			t.Errorf("VMs tagged %s left on the target: %+v", servers.ServerTag(sid), left)
		}
	})
	waitAction(t, app, c, created.Action.ID)
	waitStatus(t, app, c, sid, "running")
	vms := taggedVMs(t, pc, sid)
	require.Len(t, vms, 1)
	fw, err := pc.FirewallOptions(context.Background(), vms[0].Node, vms[0].VMID)
	require.NoError(t, err)
	require.Equal(t, "ACCEPT", fw["policy_in"], "%v", fw)
	require.Equal(t, "1", fw["enable"], "%v", fw)

	var grant struct {
		WSSURL   string `json:"wss_url"`
		Password string `json:"password"`
	}
	call(t, app, c, http.MethodPost, fmt.Sprintf("/v1/servers/%d/actions/request_console", sid), nil, http.StatusCreated, &grant)
	require.NotEmpty(t, grant.Password)
	checkRFBBanner(t, grant.WSSURL)
	call(t, app, c, http.MethodGet, fmt.Sprintf("/v1/servers/%d/metrics?type=cpu,disk,network&timeframe=hour", sid), nil,
		http.StatusOK, nil)

	var a actionBody
	call(t, app, c, http.MethodPost, fmt.Sprintf("/v1/servers/%d/actions/poweroff", sid), nil, http.StatusCreated, &a)
	waitAction(t, app, c, a.Action.ID)
	waitStatus(t, app, c, sid, "off")

	call(t, app, c, http.MethodPost, fmt.Sprintf("/v1/servers/%d/actions/rebuild", sid), map[string]string{"image": "e2e-debian"},
		http.StatusCreated, &a)
	waitAction(t, app, c, a.Action.ID)
	require.Len(t, taggedVMs(t, pc, sid), 1)

	call(t, app, c, http.MethodDelete, fmt.Sprintf("/v1/servers/%d", sid), nil, http.StatusOK, &a)
	waitAction(t, app, c, a.Action.ID)
	deleted = true
	require.Empty(t, taggedVMs(t, pc, sid))
	call(t, app, c, http.MethodGet, fmt.Sprintf("/v1/servers/%d", sid), nil, http.StatusNotFound, nil)
}

// checkRFBBanner opens the console websocket and reads the VNC server's protocol banner.
func checkRFBBanner(t *testing.T, wssURL string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, wssURL, &websocket.DialOptions{Subprotocols: []string{"binary"}})
	require.NoError(t, err)
	defer ws.CloseNow()
	_, b, err := ws.Read(ctx)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(string(b), "RFB 003.00"), "banner %q", b)
	_ = ws.Close(websocket.StatusNormalClosure, "")
}

// cleanupServer best-effort deletes a server left behind by a failed run, retrying while another action holds it.
func cleanupServer(t *testing.T, app *testutil.App, c *testutil.Client, sid int64) {
	path := fmt.Sprintf("/v1/servers/%d", sid)
	deadline := time.Now().Add(actionTimeout)
	for time.Now().Before(deadline) {
		res := c.Send(http.MethodDelete, path, nil)
		b := app.Body(res)
		switch res.StatusCode {
		case http.StatusNotFound:
			return
		case http.StatusOK:
			var a actionBody
			if err := json.Unmarshal([]byte(b), &a); err != nil {
				t.Errorf("cleanup delete: %v: %s", err, b)
				return
			}
			waitAction(t, app, c, a.Action.ID)
			return
		}
		t.Logf("cleanup delete server %d: %d %s; retrying", sid, res.StatusCode, b)
		time.Sleep(10 * time.Second)
	}
	t.Errorf("cleanup: server %d was not deleted within %s", sid, actionTimeout)
}

const (
	buildTimeout  = 20 * time.Minute
	guestBootWait = 60 * time.Second
)

type catalogLoc struct {
	State string `json:"state"`
	Nodes []struct {
		Node   string `json:"node"`
		Status string `json:"status"`
		Error  string `json:"error"`
	} `json:"nodes"`
}

func catalogState(t *testing.T, app *testutil.App, c *testutil.Client, key string, loc int64) catalogLoc {
	t.Helper()
	var out struct {
		Catalog []struct {
			Key       string `json:"key"`
			Locations []struct {
				LocationID int64 `json:"location_id"`
				catalogLoc
			} `json:"locations"`
		} `json:"catalog"`
	}
	call(t, app, c, http.MethodGet, "/v1/admin/catalog", nil, http.StatusOK, &out)
	for _, e := range out.Catalog {
		for _, l := range e.Locations {
			if e.Key == key && l.LocationID == loc {
				return l.catalogLoc
			}
		}
	}
	t.Fatalf("catalog entry %s has no state for location %d", key, loc)
	return catalogLoc{}
}

func waitCatalogReady(t *testing.T, app *testutil.App, c *testutil.Client, key string, loc int64) {
	t.Helper()
	deadline := time.Now().Add(buildTimeout)
	for {
		st := catalogState(t, app, c, key, loc)
		switch st.State {
		case "ready":
			return
		case "failed", "partial", "unavailable":
			t.Fatalf("catalog %s state %s: %+v", key, st.State, st.Nodes)
		}
		if time.Now().After(deadline) {
			t.Fatalf("catalog %s still %s after %s: %+v", key, st.State, buildTimeout, st.Nodes)
		}
		time.Sleep(5 * time.Second)
	}
}

// templateVMs lists VMs carrying the catalog template tag, minus the ids in skip.
func templateVMs(t *testing.T, pc pve.Client, skip map[int]bool) []pve.VM {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	vms, err := pc.VMs(ctx)
	require.NoError(t, err)
	return slices.DeleteFunc(vms, func(v pve.VM) bool {
		tags := strings.FieldsFunc(v.Tags, func(r rune) bool { return r == ';' || r == ',' || r == ' ' })
		return skip[v.VMID] || !slices.Contains(tags, osimages.TagTemplate)
	})
}

func waitNoTemplateVMs(t *testing.T, pc pve.Client, skip map[int]bool) {
	t.Helper()
	deadline := time.Now().Add(buildTimeout)
	for {
		left := templateVMs(t, pc, skip)
		if len(left) == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Errorf("VMs tagged %s left on the target: %+v", osimages.TagTemplate, left)
			return
		}
		time.Sleep(5 * time.Second)
	}
}

func TestCatalogTemplate(t *testing.T) {
	tg := loadTarget(t)
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	workers := func(w *river.Workers, d *httpapi.Deps, _ *fake.Server) {
		servers.RegisterWorkers(w, servers.WorkerOptions{SQL: d.SQL, Ent: d.Ent, Catalog: d.Catalog, Bus: d.Bus, Logger: log,
			PollInterval: time.Second})
	}
	app := testutil.NewAppWithWorkers(t, workers, func(d *httpapi.Deps) {
		d.Logger = log
		d.ProjectHooks, d.CatalogHooks = httpapi.ServerHooks(d.ProjectHooks, d.CatalogHooks)
		d.Catalog = catalog.New(d.SQL, d.Ent, catalog.Options{SecretKey: []byte(testutil.SecretKey), PollInterval: time.Second,
			Hooks: d.CatalogHooks})
		d.OSImages = osimages.New(osimages.Options{SQL: d.SQL, Ent: d.Ent, Catalog: d.Catalog, Logger: log})
	})
	pc := pve.New(tg.conn())
	preexisting := map[int]bool{}
	for _, v := range templateVMs(t, pc, nil) {
		preexisting[v.VMID] = true
	}

	const email = "e2e-catalog@example.com"
	res := app.PostJSON("/v1/setup", map[string]string{"email": email, "password": testutil.Password, "name": "e2e"})
	require.Equal(t, http.StatusCreated, res.StatusCode, app.Body(res))
	res.Body.Close()
	admin := app.Login(email)

	var id struct {
		ID int64 `json:"id"`
	}
	call(t, app, admin, http.MethodPost, "/v1/admin/locations", map[string]any{
		"name": "e2e-catalog", "disk_storage": tg.diskStorage, "iso_storage": tg.isoStorage, "import_storage": tg.importStorage,
		"pve": map[string]any{"url": tg.url, "token_id": tg.tokenID, "token_secret": tg.secret,
			"ca_fingerprint": tg.fingerprint, "insecure_tls": tg.insecure && tg.fingerprint == ""},
	}, http.StatusCreated, &id)
	loc := id.ID
	var poolID int64
	t.Cleanup(func() {
		if poolID != 0 {
			admin.Send(http.MethodDelete, fmt.Sprintf("/v1/admin/ip_pools/%d", poolID), nil)
		}
		for deadline := time.Now().Add(2 * time.Minute); time.Now().Before(deadline); time.Sleep(5 * time.Second) {
			res := admin.Send(http.MethodDelete, fmt.Sprintf("/v1/admin/locations/%d", loc), nil)
			b := app.Body(res)
			if res.StatusCode == http.StatusNoContent || res.StatusCode == http.StatusNotFound {
				return
			}
			t.Logf("cleanup delete location: %d %s; retrying", res.StatusCode, b)
		}
		t.Errorf("cleanup: location %d was not deleted", loc)
	})

	// Registered after the location cleanup, so it runs first and the retire guard can pass.
	disabled := false
	disable := func() {
		call(t, app, admin, http.MethodPost, fmt.Sprintf("/v1/admin/catalog/debian-13/locations/%d/disable", loc), nil,
			http.StatusOK, nil)
		disabled = true
	}
	t.Cleanup(func() {
		if !disabled {
			for deadline := time.Now().Add(2 * time.Minute); time.Now().Before(deadline); time.Sleep(10 * time.Second) {
				res := admin.Send(http.MethodPost, fmt.Sprintf("/v1/admin/catalog/debian-13/locations/%d/disable", loc), nil)
				b := app.Body(res)
				if res.StatusCode == http.StatusOK {
					break
				}
				t.Logf("cleanup disable: %d %s; retrying", res.StatusCode, b)
			}
		}
		waitNoTemplateVMs(t, pc, preexisting)
	})

	call(t, app, admin, http.MethodPost, "/v1/admin/server_types",
		map[string]any{"name": "e2e-small", "cores": 1, "memory_mb": 1024, "disk_gb": 10}, http.StatusCreated, nil)
	family, pubNet := "ipv4", map[string]bool{"ipv4": true, "ipv6": false}
	if p, err := netip.ParsePrefix(tg.cidr); err == nil && p.Addr().Is6() {
		family, pubNet = "ipv6", map[string]bool{"ipv4": false, "ipv6": true}
	}
	var pool struct {
		ID int64 `json:"id"`
	}
	call(t, app, admin, http.MethodPost, "/v1/admin/ip_pools", map[string]any{"location_id": loc, "family": family,
		"bridge": tg.bridge, "cidr": tg.cidr, "gateway": tg.gateway, "dns": []string{tg.gateway},
		"range_start": tg.rangeStart, "range_end": tg.rangeEnd}, http.StatusCreated, &pool)
	poolID = pool.ID

	start := time.Now()
	call(t, app, admin, http.MethodPost, fmt.Sprintf("/v1/admin/catalog/debian-13/locations/%d/enable", loc), nil,
		http.StatusOK, nil)
	waitCatalogReady(t, app, admin, "debian-13", loc)
	t.Logf("debian-13 built in %s", time.Since(start).Round(time.Second))

	proj := admin.CreateProject("e2e")
	c := admin.WithProject(proj)
	call(t, app, c, http.MethodPost, "/v1/ssh_keys", map[string]string{"name": "e2e", "public_key": sshKey(t)},
		http.StatusCreated, nil)
	var created struct {
		Server struct {
			ID int64 `json:"id"`
		} `json:"server"`
		Action action `json:"action"`
	}
	name := fmt.Sprintf("nebla-e2e-%d", time.Now().Unix())
	call(t, app, c, http.MethodPost, "/v1/servers", map[string]any{"name": name, "server_type": "e2e-small",
		"image": "debian-13", "location": "e2e-catalog", "ssh_keys": []string{"e2e"}, "public_net": pubNet},
		http.StatusCreated, &created)
	sid := created.Server.ID
	deleted := false
	t.Cleanup(func() {
		if !deleted {
			cleanupServer(t, app, c, sid)
		}
		if left := taggedVMs(t, pc, sid); len(left) > 0 {
			t.Errorf("VMs tagged %s left on the target: %+v", servers.ServerTag(sid), left)
		}
	})
	waitAction(t, app, c, created.Action.ID)
	waitStatus(t, app, c, sid, "running")

	// Wait for guest boot: a first-boot guest drops the ACPI power button.
	time.Sleep(guestBootWait)
	// Without the guest agent the clone shuts down over ACPI.
	var a actionBody
	call(t, app, c, http.MethodPost, fmt.Sprintf("/v1/servers/%d/actions/shutdown", sid), nil, http.StatusCreated, &a)
	waitAction(t, app, c, a.Action.ID)
	deadline := time.Now().Add(3 * time.Minute)
	for st := serverStatus(t, app, c, sid); st != "off"; st = serverStatus(t, app, c, sid) {
		if time.Now().After(deadline) {
			t.Fatalf("server %d is %s 3m after shutdown, want off", sid, st)
		}
		time.Sleep(2 * time.Second)
	}

	call(t, app, c, http.MethodPost, fmt.Sprintf("/v1/servers/%d/actions/rebuild", sid), map[string]string{"image": "debian-13"},
		http.StatusCreated, &a)
	waitAction(t, app, c, a.Action.ID)
	require.Len(t, taggedVMs(t, pc, sid), 1)

	call(t, app, c, http.MethodDelete, fmt.Sprintf("/v1/servers/%d", sid), nil, http.StatusOK, &a)
	waitAction(t, app, c, a.Action.ID)
	deleted = true
	require.Empty(t, taggedVMs(t, pc, sid))

	// The upstream checksum is unchanged, so this is normally a no-op build that ends ready; a swap is fine too.
	call(t, app, admin, http.MethodPost, fmt.Sprintf("/v1/admin/catalog/debian-13/locations/%d/refresh", loc), nil,
		http.StatusOK, nil)
	waitCatalogReady(t, app, admin, "debian-13", loc)

	disable()
	waitNoTemplateVMs(t, pc, preexisting)
}

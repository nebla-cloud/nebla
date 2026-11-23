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

package e2e_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/nebla-cloud/nebla/internal/catalog"
	"github.com/nebla-cloud/nebla/internal/httpapi"
	"github.com/nebla-cloud/nebla/internal/jobs"
	"github.com/nebla-cloud/nebla/internal/osimages"
	"github.com/nebla-cloud/nebla/internal/pve"
	"github.com/nebla-cloud/nebla/internal/pve/fake"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/riverqueue/river"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

// The runner can't reach guests, so commands run over each VM's serial console and probes run guest to guest.
const (
	fipLoginWait = 10 * time.Minute
	fipMoveLimit = 15 * time.Second
	fipUser      = "e2e"
)

// pvePost is a POST against the Proxmox API that fails unless the status is 2xx.
func pvePost(t *testing.T, tg target, path string, form url.Values, out any) {
	t.Helper()
	conn := tg.conn()
	hc := &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{TLSClientConfig: pve.TLSConfig(conn)}}
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(tg.url, "/")+"/api2/json"+path, strings.NewReader(form.Encode()))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "PVEAPIToken="+conn.TokenID+"="+conn.Secret)
	res, err := hc.Do(req)
	require.NoError(t, err, path)
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	require.True(t, res.StatusCode >= 200 && res.StatusCode <= 299, "POST %s: %d %s", path, res.StatusCode, b)
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(b, &env), string(b))
	require.NoError(t, json.Unmarshal(env.Data, out), string(env.Data))
}

// serialConsole is a shell on a VM's serial0, reached through Proxmox's termproxy.
type serialConsole struct {
	t      *testing.T
	name   string
	ws     *websocket.Conn
	cancel context.CancelFunc
	mu     sync.Mutex
	buf    strings.Builder
	upd    chan struct{}
	n      int
}

func openSerial(t *testing.T, tg target, name, node string, vmid int) *serialConsole {
	t.Helper()
	var tp struct {
		Port   json.RawMessage `json:"port"`
		Ticket string          `json:"ticket"`
		User   string          `json:"user"`
	}
	path := fmt.Sprintf("/nodes/%s/qemu/%d", node, vmid)
	pvePost(t, tg, path+"/termproxy", url.Values{"serial": {"serial0"}}, &tp)
	port := strings.Trim(string(tp.Port), `"`)
	conn := tg.conn()
	u := strings.Replace(strings.TrimRight(tg.url, "/"), "http", "ws", 1) + "/api2/json" + path + "/vncwebsocket?" +
		url.Values{"port": {port}, "vncticket": {tp.Ticket}}.Encode()
	ctx, cancel := context.WithCancel(context.Background())
	dctx, dcancel := context.WithTimeout(ctx, 30*time.Second)
	defer dcancel()
	hc := &http.Client{Transport: &http.Transport{TLSClientConfig: pve.TLSConfig(conn)}}
	ws, _, err := websocket.Dial(dctx, u, &websocket.DialOptions{HTTPClient: hc, Subprotocols: []string{"binary"},
		HTTPHeader: http.Header{"Authorization": {"PVEAPIToken=" + conn.TokenID + "=" + conn.Secret}}})
	if err != nil {
		cancel()
		require.NoError(t, err, "serial websocket for %s", name)
	}
	ws.SetReadLimit(1 << 20)
	s := &serialConsole{t: t, name: name, ws: ws, cancel: cancel, upd: make(chan struct{}, 1)}
	require.NoError(t, ws.Write(ctx, websocket.MessageText, []byte(tp.User+":"+tp.Ticket+"\n")))
	go func() {
		for {
			_, b, err := ws.Read(ctx)
			if err != nil {
				return
			}
			s.mu.Lock()
			s.buf.WriteString(strings.ReplaceAll(string(b), "\r", ""))
			s.mu.Unlock()
			select {
			case s.upd <- struct{}{}:
			default:
			}
		}
	}()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(15 * time.Second):
				_ = ws.Write(ctx, websocket.MessageText, []byte("2"))
			}
		}
	}()
	t.Cleanup(s.close)
	return s
}

func (s *serialConsole) close() {
	s.cancel()
	_ = s.ws.CloseNow()
}

func (s *serialConsole) len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Len()
}

func (s *serialConsole) since(off int) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()[off:]
}

func (s *serialConsole) send(in string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	require.NoError(s.t, s.ws.Write(ctx, websocket.MessageText, []byte("0:"+strconv.Itoa(len(in))+":"+in)), s.name)
}

// waitFor waits until the output after off matches re and returns the match.
func (s *serialConsole) waitFor(off int, re *regexp.Regexp, limit time.Duration) ([]string, bool) {
	deadline := time.Now().Add(limit)
	for {
		if m := re.FindStringSubmatch(s.since(off)); m != nil {
			return m, true
		}
		left := time.Until(deadline)
		if left <= 0 {
			return nil, false
		}
		select {
		case <-s.upd:
		case <-time.After(min(left, time.Second)):
		}
	}
}

var (
	reLogin    = regexp.MustCompile(`login: *$`)
	rePassword = regexp.MustCompile(`Password: *$`)
	reLoggedIn = regexp.MustCompile(`NBOK|Login incorrect`)
)

// login signs in on the serial getty, retrying while the guest is still booting, then starts a plain sh without echo.
func (s *serialConsole) login(pw string) {
	t := s.t
	t.Helper()
	start := time.Now()
	for deadline := start.Add(fipLoginWait); ; {
		require.True(t, time.Now().Before(deadline), "%s: no serial login within %s; last output:\n%s", s.name, fipLoginWait, tail(s.since(0)))
		off := s.len()
		s.send("\r")
		if _, ok := s.waitFor(off, reLogin, 15*time.Second); !ok {
			continue
		}
		off = s.len()
		s.send(fipUser + "\r")
		if _, ok := s.waitFor(off, rePassword, 15*time.Second); !ok {
			continue
		}
		off = s.len()
		s.send(pw + "\r")
		time.Sleep(3 * time.Second)
		s.send("echo NB''OK\r")
		if m, ok := s.waitFor(off, reLoggedIn, 30*time.Second); ok && m[0] == "NBOK" {
			break
		}
		time.Sleep(5 * time.Second)
	}
	s.send("exec /bin/sh\r")
	time.Sleep(time.Second)
	s.send("stty -echo; PS1=''; PS2=''\r")
	time.Sleep(time.Second)
	s.mustExec("sudo dmesg -n 1", time.Minute)
	s.exec("timeout 120 cloud-init status --wait >/dev/null 2>&1 || true; cloud-init status --long 2>&1 | head -n 8", 3*time.Minute)
	t.Logf("%s: serial login after %s", s.name, time.Since(start).Round(time.Second))
}

// exec runs cmd in a set -e subshell; the end marker shares its last line, since sudo eats typed-ahead input.
func (s *serialConsole) exec(cmd string, limit time.Duration) (string, int) {
	t := s.t
	t.Helper()
	s.n++
	id := s.n
	off := s.len()
	s.send(fmt.Sprintf("printf '\\n__NB%%s_%d__\\n' S\n(\nset -e\n%s\n); printf '\\n__NB%%s_%d_%%d__\\n' E $?\n", id, cmd, id))
	re := regexp.MustCompile(fmt.Sprintf(`(?s)__NBS_%d__\n(.*)\n__NBE_%d_(\d+)__`, id, id))
	m, ok := s.waitFor(off, re, limit)
	require.True(t, ok, "%s: %q did not finish within %s; output:\n%s", s.name, cmd, limit, tail(s.since(off)))
	code, _ := strconv.Atoi(m[2])
	out := strings.TrimSpace(m[1])
	t.Logf("%s$ %s\n%s\n[exit %d]", s.name, cmd, out, code)
	return out, code
}

func (s *serialConsole) mustExec(cmd string, limit time.Duration) string {
	s.t.Helper()
	out, code := s.exec(cmd, limit)
	require.Zero(s.t, code, "%s: %q failed:\n%s", s.name, cmd, out)
	return out
}

func tail(s string) string {
	if len(s) > 3000 {
		return s[len(s)-3000:]
	}
	return s
}

// probeHost runs ssh from c to ip until it reports want, and returns how long that took.
func probeHost(t *testing.T, c *serialConsole, ip, want string, limit time.Duration) time.Duration {
	t.Helper()
	start := time.Now()
	cmd := "sudo ssh -i /root/fipkey -o BatchMode=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null " +
		"-o LogLevel=ERROR -o ConnectTimeout=2 " + fipUser + "@" + ip + " hostname"
	var last string
	for {
		out, code := c.exec(cmd, time.Minute)
		last = out
		if code == 0 && strings.TrimSpace(out) == want {
			return time.Since(start)
		}
		if time.Since(start) > limit {
			t.Fatalf("ssh from %s to %s never reached %s within %s; last: %q", c.name, ip, want, limit, last)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func fipUserData(t *testing.T, pw string, priv ed25519.PrivateKey, pub ssh.PublicKey) string {
	t.Helper()
	block, err := ssh.MarshalPrivateKey(priv, "")
	require.NoError(t, err)
	doc := map[string]any{
		"users": []any{"default", map[string]any{"name": fipUser, "shell": "/bin/bash", "sudo": "ALL=(ALL) NOPASSWD:ALL",
			"lock_passwd": false, "plain_text_passwd": pw,
			"ssh_authorized_keys": []string{strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub)))}}},
		"write_files": []any{map[string]any{"path": "/root/fipkey", "permissions": "0600",
			"content": string(pem.EncodeToMemory(block))}},
	}
	b, err := json.Marshal(doc)
	require.NoError(t, err)
	return "#cloud-config\n" + string(b) + "\n"
}

type fipOut struct {
	ID          int64  `json:"id"`
	IP          string `json:"ip"`
	Server      *int64 `json:"server"`
	Status      string `json:"status"`
	GuestConfig *struct {
		Server         *string `json:"server"`
		Apply          *string `json:"apply"`
		Persist        *string `json:"persist"`
		PreviousServer *string `json:"previous_server"`
		Remove         *string `json:"remove"`
	} `json:"guest_config"`
}

func fipAddr(f fipOut) string {
	ip, _, _ := strings.Cut(f.IP, "/")
	return ip
}

type fipEnv struct {
	tg     target
	app    *testutil.App
	admin  *testutil.Client
	c      *testutil.Client
	pc     pve.Client
	loc    string
	poolID int64
}

func newFIPEnv(t *testing.T, images []string) fipEnv {
	tg := loadTarget(t)
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	workers := func(w *river.Workers, d *httpapi.Deps, _ *fake.Server) {
		servers.RegisterWorkers(w, servers.WorkerOptions{SQL: d.SQL, Ent: d.Ent, Catalog: d.Catalog, Bus: d.Bus, Logger: log,
			PollInterval: time.Second, Jobs: func() *jobs.Jobs { return d.Jobs }})
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

	const email = "e2e-fip@example.com"
	res := app.PostJSON("/v1/setup", map[string]string{"email": email, "password": testutil.Password, "name": "e2e"})
	require.Equal(t, http.StatusCreated, res.StatusCode, app.Body(res))
	res.Body.Close()
	admin := app.Login(email)

	const locName = "e2e-fip"
	var id struct {
		ID int64 `json:"id"`
	}
	call(t, app, admin, http.MethodPost, "/v1/admin/locations", map[string]any{
		"name": locName, "disk_storage": tg.diskStorage, "iso_storage": tg.isoStorage, "import_storage": tg.importStorage,
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
	t.Cleanup(func() {
		for _, key := range images {
			for deadline := time.Now().Add(2 * time.Minute); time.Now().Before(deadline); time.Sleep(10 * time.Second) {
				res := admin.Send(http.MethodPost, fmt.Sprintf("/v1/admin/catalog/%s/locations/%d/disable", key, loc), nil)
				b := app.Body(res)
				if res.StatusCode == http.StatusOK {
					break
				}
				t.Logf("cleanup disable %s: %d %s; retrying", key, res.StatusCode, b)
			}
		}
		waitNoTemplateVMs(t, pc, preexisting)
	})

	call(t, app, admin, http.MethodPost, "/v1/admin/server_types",
		map[string]any{"name": "e2e-small", "cores": 1, "memory_mb": 1024, "disk_gb": 10}, http.StatusCreated, nil)
	var pool struct {
		ID int64 `json:"id"`
	}
	call(t, app, admin, http.MethodPost, "/v1/admin/ip_pools", map[string]any{"location_id": loc, "family": "ipv4",
		"bridge": tg.bridge, "cidr": tg.cidr, "gateway": tg.gateway, "dns": strings.Split(envOr("E2E_FIP_DNS", "1.1.1.1,9.9.9.9"), ","),
		"range_start": tg.rangeStart, "range_end": tg.rangeEnd}, http.StatusCreated, &pool)
	poolID = pool.ID

	for _, key := range images {
		start := time.Now()
		call(t, app, admin, http.MethodPost, fmt.Sprintf("/v1/admin/catalog/%s/locations/%d/enable", key, loc), nil,
			http.StatusOK, nil)
		waitCatalogReady(t, app, admin, key, loc)
		t.Logf("%s built in %s", key, time.Since(start).Round(time.Second))
	}

	proj := admin.CreateProject("e2e")
	return fipEnv{tg: tg, app: app, admin: admin, c: admin.WithProject(proj), pc: pc, loc: locName, poolID: pool.ID}
}

func (e fipEnv) allocated(t *testing.T) int {
	t.Helper()
	var p struct {
		Allocated int `json:"allocated"`
	}
	call(t, e.app, e.admin, http.MethodGet, fmt.Sprintf("/v1/admin/ip_pools/%d", e.poolID), nil, http.StatusOK, &p)
	return p.Allocated
}

func (e fipEnv) fip(t *testing.T, id int64) fipOut {
	t.Helper()
	var f fipOut
	call(t, e.app, e.c, http.MethodGet, fmt.Sprintf("/v1/floating_ips/%d", id), nil, http.StatusOK, &f)
	return f
}

type fipServer struct {
	id         int64
	name, host string
	node       string
	vmid       int
	con        *serialConsole
}

func TestFloatingIPE2E(t *testing.T) {
	if strings.Contains(os.Getenv("E2E_POOL_CIDR"), ":") {
		t.Skip("the floating IP e2e needs an IPv4 pool")
	}
	images := strings.Split(envOr("E2E_FIP_IMAGES", "debian-13,ubuntu-24.04"), ",")
	for i := range images {
		images[i] = strings.TrimSpace(images[i])
	}
	e := newFIPEnv(t, images)
	for _, img := range images {
		t.Run(img, func(t *testing.T) { fipImage(t, e, img) })
	}
}

func fipImage(t *testing.T, e fipEnv, img string) {
	app, c, pc, tg := e.app, e.c, e.pc, e.tg
	startAlloc := e.allocated(t)
	pwb := make([]byte, 12)
	_, err := rand.Read(pwb)
	require.NoError(t, err)
	pw := hex.EncodeToString(pwb)
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	spub, err := ssh.NewPublicKey(pub)
	require.NoError(t, err)
	userData := fipUserData(t, pw, priv, spub)

	stamp := time.Now().Unix()
	var ids []int64
	var fips []int64
	t.Cleanup(func() {
		for _, id := range ids {
			if left := taggedVMs(t, pc, id); len(left) > 0 {
				t.Errorf("VMs tagged %s left on the target: %+v", servers.ServerTag(id), left)
			}
		}
	})
	create := func(name string) (int64, action) {
		var created struct {
			Server struct {
				ID int64 `json:"id"`
			} `json:"server"`
			Action action `json:"action"`
		}
		call(t, app, c, http.MethodPost, "/v1/servers", map[string]any{"name": name, "server_type": "e2e-small", "image": img,
			"location": e.loc, "user_data": userData, "public_net": map[string]bool{"ipv4": true, "ipv6": false}},
			http.StatusCreated, &created)
		sid := created.Server.ID
		ids = append(ids, sid)
		t.Cleanup(func() { cleanupServer(t, app, c, sid) })
		return sid, created.Action
	}
	deleteFIP := func(id int64) {
		res := c.Send(http.MethodDelete, fmt.Sprintf("/v1/floating_ips/%d", id), nil)
		b := app.Body(res)
		if res.StatusCode == http.StatusNotFound {
			return
		}
		require.Equal(t, http.StatusOK, res.StatusCode, b)
		var a actionBody
		require.NoError(t, json.Unmarshal([]byte(b), &a), b)
		waitAction(t, app, c, a.Action.ID)
	}
	t.Cleanup(func() {
		for _, id := range fips {
			res := c.Send(http.MethodDelete, fmt.Sprintf("/v1/floating_ips/%d", id), nil)
			t.Logf("cleanup delete floating IP %d: %d %s", id, res.StatusCode, app.Body(res))
		}
	})
	attach := func(sid int64, name string) fipServer {
		waitStatus(t, app, c, sid, "running")
		vms := taggedVMs(t, pc, sid)
		require.Len(t, vms, 1)
		con := openSerial(t, tg, name, vms[0].Node, vms[0].VMID)
		con.login(pw)
		return fipServer{id: sid, name: name, host: con.mustExec("hostname", time.Minute), node: vms[0].Node, vmid: vms[0].VMID,
			con: con}
	}

	// One at a time: concurrent clones can race on the next VMID.
	aName, bName := fmt.Sprintf("nebla-fip-a-%d", stamp), fmt.Sprintf("nebla-fip-b-%d", stamp)
	aID, aAct := create(aName)
	waitAction(t, app, c, aAct.ID)
	bID, bAct := create(bName)
	// Assigned while B is still being created, so B's first boot brings it up from cloud-init.
	var boot struct {
		FloatingIP fipOut  `json:"floating_ip"`
		Action     *action `json:"action"`
	}
	call(t, app, c, http.MethodPost, "/v1/floating_ips", map[string]any{"type": "ipv4", "server": strconv.FormatInt(bID, 10)},
		http.StatusCreated, &boot)
	fips = append(fips, boot.FloatingIP.ID)
	require.NotNil(t, boot.Action)
	waitAction(t, app, c, bAct.ID)
	waitAction(t, app, c, boot.Action.ID)
	bootIP := fipAddr(boot.FloatingIP)
	A, B := attach(aID, aName), attach(bID, bName)

	t.Run("cloud-init", func(t *testing.T) {
		A.con.t, B.con.t = t, t
		out := B.con.mustExec("ip -o -4 addr show dev eth0 | cat", time.Minute)
		require.Contains(t, out, " "+bootIP+"/32 ", "floating IP from cloud-init missing on B")
		took := probeHost(t, A.con, bootIP, B.host, time.Minute)
		t.Logf("CLOUD-INIT: %s assigned during create was up on B at first boot; ssh from A reached B in %s", bootIP,
			took.Round(100*time.Millisecond))
	})

	var created struct {
		FloatingIP fipOut `json:"floating_ip"`
	}
	call(t, app, c, http.MethodPost, "/v1/floating_ips", map[string]any{"type": "ipv4", "location": e.loc},
		http.StatusCreated, &created)
	fid := created.FloatingIP.ID
	fips = append(fips, fid)
	ip := fipAddr(created.FloatingIP)
	t.Logf("floating IP %d = %s; A=%s B=%s", fid, ip, A.host, B.host)

	require.True(t, t.Run("assign", func(t *testing.T) {
		A.con.t, B.con.t = t, t
		var a actionBody
		call(t, app, c, http.MethodPost, fmt.Sprintf("/v1/floating_ips/%d/actions/assign", fid),
			map[string]string{"server": strconv.FormatInt(A.id, 10)}, http.StatusCreated, &a)
		waitAction(t, app, c, a.Action.ID)
		f := e.fip(t, fid)
		require.NotNil(t, f.GuestConfig)
		require.NotNil(t, f.GuestConfig.Apply)
		start := time.Now()
		A.con.mustExec(*f.GuestConfig.Apply, 5*time.Minute)
		t.Logf("apply on A took %s", time.Since(start).Round(100*time.Millisecond))
		took := probeHost(t, B.con, ip, A.host, time.Minute)
		t.Logf("ASSIGN: ssh from B to %s reached A %s after apply", ip, took.Round(100*time.Millisecond))
		A.con.mustExec(fmt.Sprintf("ping -c1 -W2 -I %s %s", ip, tg.gateway), time.Minute)
	}))

	require.True(t, t.Run("move", func(t *testing.T) {
		A.con.t, B.con.t = t, t
		var a actionBody
		call(t, app, c, http.MethodPost, fmt.Sprintf("/v1/floating_ips/%d/actions/assign", fid),
			map[string]string{"server": strconv.FormatInt(B.id, 10)}, http.StatusCreated, &a)
		waitAction(t, app, c, a.Action.ID)
		f := e.fip(t, fid)
		require.NotNil(t, f.GuestConfig)
		require.NotNil(t, f.GuestConfig.Apply)
		require.NotNil(t, f.GuestConfig.Remove)
		require.Equal(t, A.name, *f.GuestConfig.PreviousServer)
		out, code := A.con.exec(fmt.Sprintf("ping -c1 -W2 -I %s %s", ip, tg.gateway), time.Minute)
		require.NotZero(t, code, "A could still send from %s after the move:\n%s", ip, out)
		A.con.mustExec(*f.GuestConfig.Remove, 2*time.Minute)
		applyStart := time.Now()
		B.con.mustExec(*f.GuestConfig.Apply, 5*time.Minute)
		arped := time.Now()
		took := probeHost(t, A.con, ip, B.host, fipMoveLimit)
		t.Logf("MOVE LATENCY: ssh from A to %s reached B %s after B's apply (arping) finished; apply took %s", ip,
			took.Round(100*time.Millisecond), arped.Sub(applyStart).Round(100*time.Millisecond))
	}))

	require.True(t, t.Run("persist", func(t *testing.T) {
		A.con.t, B.con.t = t, t
		f := e.fip(t, fid)
		require.NotNil(t, f.GuestConfig)
		require.NotNil(t, f.GuestConfig.Persist)
		B.con.mustExec(*f.GuestConfig.Persist, 2*time.Minute)
		bootID := B.con.mustExec("cat /proc/sys/kernel/random/boot_id", time.Minute)
		B.con.send("sudo systemctl reboot\n")
		time.Sleep(20 * time.Second)
		B.con.close()
		B.con = openSerial(t, tg, B.name, B.node, B.vmid)
		B.con.login(pw)
		require.NotEqual(t, bootID, B.con.mustExec("cat /proc/sys/kernel/random/boot_id", time.Minute), "B did not reboot")
		out := B.con.mustExec("ip -o -4 addr show dev eth0 | cat", time.Minute)
		require.Contains(t, out, " "+ip+"/32 ", "floating IP missing on B after the reboot")
		took := probeHost(t, A.con, ip, B.host, time.Minute)
		t.Logf("PERSIST: after the reboot ssh from A to %s reached B in %s", ip, took.Round(100*time.Millisecond))
	}))

	require.True(t, t.Run("delete", func(t *testing.T) {
		A.con.t, B.con.t = t, t
		A.con.close()
		B.con.close()
		for _, id := range fips {
			deleteFIP(id)
		}
		for _, s := range []fipServer{A, B} {
			var a actionBody
			call(t, app, c, http.MethodDelete, fmt.Sprintf("/v1/servers/%d", s.id), nil, http.StatusOK, &a)
			waitAction(t, app, c, a.Action.ID)
			require.Empty(t, taggedVMs(t, pc, s.id))
		}
		require.Equal(t, startAlloc, e.allocated(t), "pool allocated count")
	}))
}

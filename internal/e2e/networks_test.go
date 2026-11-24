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
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/vnetslot"
	"github.com/nebla-cloud/nebla/internal/networks"
	"github.com/nebla-cloud/nebla/internal/reconcile"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

// The test owns zone netZone and VNets netVNets on the target; it creates them over root SSH and removes them after.
const (
	netZone   = "nbe2e"
	netPrefix = "nbe"
	netPing   = time.Minute
)

var netVNets = []string{"nbe1", "nbe2"}

// pveShell runs commands as root on the Proxmox host over SSH.
type pveShell struct {
	t    *testing.T
	dest string
}

func newPVEShell(t *testing.T, tg target) pveShell {
	t.Helper()
	dest := os.Getenv("E2E_PVE_SSH")
	if dest == "" {
		u, err := url.Parse(tg.url)
		require.NoError(t, err, "E2E_PVE_URL")
		dest = "root@" + u.Hostname()
	}
	return pveShell{t: t, dest: dest}
}

func (p pveShell) run(cmd string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10", p.dest, cmd).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func (p pveShell) must(cmd string) string {
	p.t.Helper()
	out, err := p.run(cmd)
	require.NoError(p.t, err, "%s on %s: %s", cmd, p.dest, out)
	return out
}

// waitLinks waits until the host's nbe* links are exactly want.
func (p pveShell) waitLinks(want ...string) error {
	deadline := time.Now().Add(2 * time.Minute)
	for {
		out, err := p.run("ip -br link | awk '{print $1}' | grep '^" + netPrefix + "' || true")
		if err == nil && strings.Join(strings.Fields(out), ",") == strings.Join(want, ",") {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("host links %q, want %q (err %v)", out, want, err)
		}
		time.Sleep(2 * time.Second)
	}
}

// teardownSDN removes the test zone and VNets and applies; missing ones are fine.
func (p pveShell) teardownSDN() error {
	for _, v := range netVNets {
		p.run("pvesh delete /cluster/sdn/vnets/" + v)
	}
	p.run("pvesh delete /cluster/sdn/zones/" + netZone)
	if out, err := p.run("pvesh set /cluster/sdn"); err != nil {
		return fmt.Errorf("apply sdn: %v: %s", err, out)
	}
	return p.waitLinks()
}

func (p pveShell) rolePrivs(role string) (string, bool) {
	p.t.Helper()
	var roles []struct {
		RoleID string `json:"roleid"`
		Privs  string `json:"privs"`
	}
	require.NoError(p.t, json.Unmarshal([]byte(p.must("pveum role list --output-format json")), &roles))
	for _, r := range roles {
		if r.RoleID == role {
			return r.Privs, true
		}
	}
	return "", false
}

type netEnv struct {
	fipEnv
	c2  *testutil.Client
	rec *reconcile.Reconciler
	// skew is the clock offset (ns) the networks service sees; a pointer so copies of netEnv share it.
	skew *atomic.Int64
}

// reconcilePass runs one reconciler pass over the location, private network pool included.
func (e netEnv) reconcilePass(t *testing.T) {
	t.Helper()
	e.skew.Add(int64(10 * time.Minute)) // step past the cooling-scan throttle
	if err := e.rec.Run(context.Background(), e.locID); err != nil {
		t.Logf("reconcile pass: %v", err)
	}
}

func (e netEnv) slot(t *testing.T, vnet string) *ent.VNetSlot {
	t.Helper()
	sl, err := e.app.Ent.VNetSlot.Query().Where(vnetslot.LocationID(e.locID), vnetslot.Vnet(vnet)).Only(context.Background())
	require.NoError(t, err, "slot %s", vnet)
	return sl
}

func newNetEnv(t *testing.T, images []string) netEnv {
	e := netEnv{fipEnv: newCatalogEnv(t, images, "net"), skew: new(atomic.Int64)}
	tg, app, admin := e.tg, e.app, e.admin
	skew := e.skew
	sh := newPVEShell(t, tg)

	role := envOr("E2E_PVE_ROLE", "Nebla")
	privs, ok := sh.rolePrivs(role)
	require.True(t, ok, "role %s not found on the target", role)
	if !strings.Contains(","+privs+",", ",SDN.Audit,") {
		sh.must("pveum role modify " + role + " --append 1 --privs SDN.Audit")
		t.Cleanup(func() {
			if out, err := sh.run(fmt.Sprintf("pveum role modify %s --privs '%s'", role, privs)); err != nil {
				t.Errorf("cleanup: restore role %s privs: %v: %s", role, err, out)
			}
		})
		t.Logf("added SDN.Audit to role %s", role)
	}

	// A crashed earlier run may have left the zone behind.
	require.NoError(t, sh.teardownSDN(), "pre-clean SDN")
	t.Cleanup(func() {
		if err := sh.teardownSDN(); err != nil {
			t.Errorf("cleanup SDN: %v", err)
		}
	})
	sh.must(fmt.Sprintf("pvesh create /cluster/sdn/zones --zone %s --type simple --ipam pve", netZone))
	for _, v := range netVNets {
		sh.must(fmt.Sprintf("pvesh create /cluster/sdn/vnets --vnet %s --zone %s", v, netZone))
	}
	sh.must("pvesh set /cluster/sdn")
	require.NoError(t, sh.waitLinks(netVNets...))

	nets := networks.New(networks.Options{SQL: app.SQL, Ent: app.Ent, Catalog: app.Catalog, Jobs: app.Jobs, Logger: e.log,
		Now: func() time.Time { return time.Now().Add(time.Duration(skew.Load())) }})
	e.rec = reconcile.New(reconcile.Options{SQL: app.SQL, Ent: app.Ent, Catalog: app.Catalog, Jobs: app.Jobs, Logger: e.log,
		NetworkPool: nets.Reconcile})
	setSDN := func(zone string) *http.Response {
		return admin.Send(http.MethodPut, fmt.Sprintf("/v1/admin/locations/%d", e.locID), map[string]any{
			"name": e.loc, "disk_storage": tg.diskStorage, "iso_storage": tg.isoStorage, "import_storage": tg.importStorage,
			"pve": map[string]any{"url": tg.url, "token_id": tg.tokenID, "token_secret": tg.secret,
				"ca_fingerprint": tg.fingerprint, "insecure_tls": tg.insecure && tg.fingerprint == ""},
			"sdn_zone": zone, "vnet_prefix": netPrefix,
		})
	}
	res := setSDN(netZone)
	require.Equal(t, http.StatusOK, res.StatusCode, app.Body(res))
	// Slot rows block deleting the location, so private networks are turned off first; cooling slots are thawed before.
	t.Cleanup(func() {
		e.reconcilePass(t)
		res := setSDN("")
		if b := app.Body(res); res.StatusCode != http.StatusOK {
			t.Errorf("cleanup: turn private networks off: %d %s", res.StatusCode, b)
		}
	})
	// The reconciler records the datacenter firewall switch, which the pool needs.
	e.reconcilePass(t)
	var loc struct {
		FirewallEnabled bool `json:"firewall_enabled"`
		Private         struct {
			State     string `json:"state"`
			Available int    `json:"available"`
			Unusable  []struct {
				VNet   string `json:"vnet"`
				Reason string `json:"reason"`
			} `json:"unusable"`
		} `json:"private_networks"`
	}
	call(t, app, admin, http.MethodGet, fmt.Sprintf("/v1/admin/locations/%d", e.locID), nil, http.StatusOK, &loc)
	t.Logf("private network pool: %+v (firewall_enabled %v)", loc.Private, loc.FirewallEnabled)
	require.Equal(t, "ready", loc.Private.State)
	require.Equal(t, len(netVNets), loc.Private.Available)

	e.c2 = admin.WithProject(admin.CreateProject("e2e-other"))
	return e
}

func TestPrivateNetworksE2E(t *testing.T) {
	if strings.Contains(os.Getenv("E2E_POOL_CIDR"), ":") {
		t.Skip("the private networks e2e needs an IPv4 pool")
	}
	images := strings.Split(envOr("E2E_NET_IMAGES", "debian-13,ubuntu-24.04"), ",")
	for i := range images {
		images[i] = strings.TrimSpace(images[i])
	}
	e := newNetEnv(t, images)
	for _, img := range images {
		t.Run(img, func(t *testing.T) { netImage(t, e, img) })
	}
}

type netServer struct {
	id         int64
	name       string
	c          *testutil.Client
	node       string
	vmid       int
	publicIP   string
	privateIP  string
	con        *serialConsole
	privateNIC string
}

type serverNets struct {
	PublicNet struct {
		IPv4 *struct {
			IP string `json:"ip"`
		} `json:"ipv4"`
	} `json:"public_net"`
	PrivateNet []struct {
		Network int64  `json:"network"`
		IP      string `json:"ip"`
	} `json:"private_net"`
}

// guestConfig is a network action's guest commands.
type guestConfig struct {
	Apply   *string `json:"apply"`
	Persist *string `json:"persist"`
	Remove  *string `json:"remove"`
	Note    *string `json:"note"`
}

type netAction struct {
	Action      action       `json:"action"`
	GuestConfig *guestConfig `json:"guest_config"`
}

// macOf returns the MAC of a Proxmox NIC value like virtio=BC:24:11:00:00:01,bridge=nbe1.
func macOf(nic string) string {
	m := regexp.MustCompile(`=([0-9A-Fa-f:]{17})`).FindStringSubmatch(nic)
	if m == nil {
		return ""
	}
	return strings.ToLower(m[1])
}

// ping reports whether con reaches ip within limit, retrying single pings.
func ping(con *serialConsole, src, ip string, limit time.Duration) bool {
	cmd := "ping -c1 -W1 " + ip
	if src != "" {
		cmd = "ping -c1 -W1 -I " + src + " " + ip
	}
	for deadline := time.Now().Add(limit); ; {
		if _, code := con.exec(cmd, time.Minute); code == 0 {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(time.Second)
	}
}

func netImage(t *testing.T, e netEnv, img string) {
	app, pc, tg := e.app, e.pc, e.tg
	ctx := context.Background()
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

	var made []netServer
	t.Cleanup(func() {
		for _, s := range made {
			if left := taggedVMs(t, pc, s.id); len(left) > 0 {
				t.Errorf("VMs tagged %s left on the target: %+v", servers.ServerTag(s.id), left)
			}
		}
	})
	createNet := func(c *testutil.Client, name string) int64 {
		var out struct {
			Network struct {
				ID      int64  `json:"id"`
				IPRange string `json:"ip_range"`
			} `json:"network"`
		}
		call(t, app, c, http.MethodPost, "/v1/networks", map[string]any{"name": name, "location": e.loc}, http.StatusCreated, &out)
		id := out.Network.ID
		t.Cleanup(func() {
			res := c.Send(http.MethodDelete, fmt.Sprintf("/v1/networks/%d", id), nil)
			if b := app.Body(res); res.StatusCode != http.StatusNotFound {
				t.Logf("cleanup delete network %d: %d %s", id, res.StatusCode, b)
			}
		})
		t.Logf("network %s = %d, range %s", name, id, out.Network.IPRange)
		return id
	}
	// One at a time: concurrent clones can race on the next VMID.
	create := func(c *testutil.Client, role string, nets []map[string]string) netServer {
		name := fmt.Sprintf("nebla-net-%s-%d", role, stamp)
		body := map[string]any{"name": name, "server_type": "e2e-small", "image": img, "location": e.loc, "user_data": userData,
			"public_net": map[string]bool{"ipv4": true, "ipv6": false}}
		if len(nets) > 0 {
			body["networks"] = nets
		}
		var created struct {
			Server struct {
				ID int64 `json:"id"`
			} `json:"server"`
			Action action `json:"action"`
		}
		call(t, app, c, http.MethodPost, "/v1/servers", body, http.StatusCreated, &created)
		sid := created.Server.ID
		made = append(made, netServer{id: sid})
		t.Cleanup(func() { cleanupServer(t, app, c, sid) })
		waitAction(t, app, c, created.Action.ID)
		waitStatus(t, app, c, sid, "running")
		vms := taggedVMs(t, pc, sid)
		require.Len(t, vms, 1)
		var s serverNets
		call(t, app, c, http.MethodGet, fmt.Sprintf("/v1/servers/%d", sid), nil, http.StatusOK, &s)
		require.NotNil(t, s.PublicNet.IPv4)
		out := netServer{id: sid, name: name, c: c, node: vms[0].Node, vmid: vms[0].VMID, publicIP: s.PublicNet.IPv4.IP}
		if len(s.PrivateNet) > 0 {
			out.privateIP = s.PrivateNet[0].IP
		}
		return out
	}
	login := func(s *netServer) {
		s.con = openSerial(t, tg, s.name, s.node, s.vmid)
		s.con.login(pw)
	}
	privateIP := func(s netServer) string {
		var got serverNets
		call(t, app, s.c, http.MethodGet, fmt.Sprintf("/v1/servers/%d", s.id), nil, http.StatusOK, &got)
		require.Len(t, got.PrivateNet, 1, "server %s private_net", s.name)
		return got.PrivateNet[0].IP
	}

	aNet := createNet(e.c, "a")
	A := create(e.c, "a", []map[string]string{{"network": "a"}})
	require.NotEmpty(t, A.privateIP)
	B := create(e.c, "b", nil)
	bNet := createNet(e.c2, "b")
	aAddr := netip.MustParseAddr(A.privateIP)
	cAddr := aAddr
	for range 50 {
		cAddr = cAddr.Next()
	}
	C := create(e.c2, "c", []map[string]string{{"network": "b", "ip": cAddr.String()}})
	require.Equal(t, cAddr.String(), C.privateIP)
	login(&A)
	login(&B)
	login(&C)
	t.Logf("A=%s/%s B=%s C=%s/%s", A.publicIP, A.privateIP, B.publicIP, C.publicIP, C.privateIP)
	setT := func(t *testing.T) { A.con.t, B.con.t, C.con.t = t, t, t }

	require.True(t, t.Run("create-time", func(t *testing.T) {
		setT(t)
		out := A.con.mustExec("ip -br addr show eth1 | cat", time.Minute)
		require.Contains(t, out, " "+A.privateIP+"/16", "A's private address from cloud-init")
		out = C.con.mustExec("ip -br addr show eth1 | cat", time.Minute)
		require.Contains(t, out, " "+C.privateIP+"/16", "C's private address from cloud-init")
	}))

	require.True(t, t.Run("hot-plug", func(t *testing.T) {
		setT(t)
		var at netAction
		call(t, app, e.c, http.MethodPost, fmt.Sprintf("/v1/servers/%d/actions/attach_to_network", B.id),
			map[string]string{"network": "a"}, http.StatusCreated, &at)
		waitAction(t, app, e.c, at.Action.ID)
		require.NotNil(t, at.GuestConfig)
		require.NotNil(t, at.GuestConfig.Apply)
		cfg, err := pc.Config(ctx, B.node, B.vmid)
		require.NoError(t, err)
		t.Logf("B net1 = %s", cfg["net1"])
		mac := macOf(cfg["net1"])
		require.NotEmpty(t, mac, "B has no net1: %v", cfg)
		B.privateNIC = mac
		B.privateIP = privateIP(B)
		out := B.con.mustExec(fmt.Sprintf("for i in $(seq 30); do ip -br link | grep -qi '%s' && break; sleep 1; done; ip -br link", mac),
			time.Minute)
		require.Contains(t, strings.ToLower(out), mac, "the hot-plugged NIC never showed up in B")
		B.con.mustExec(*at.GuestConfig.Apply, 2*time.Minute)
		B.con.mustExec("ip -br addr | cat", time.Minute)
		require.True(t, ping(B.con, "", A.privateIP, netPing), "B can't ping A's private address %s", A.privateIP)
		require.True(t, ping(A.con, "", B.privateIP, netPing), "A can't ping B's private address %s", B.privateIP)
	}))

	require.True(t, t.Run("isolation", func(t *testing.T) {
		setT(t)
		require.False(t, ping(C.con, "", A.privateIP, 5*time.Second), "C reached A's private address across networks")
		require.False(t, ping(A.con, "", C.privateIP, 5*time.Second), "A reached C's private address across networks")
	}))

	require.True(t, t.Run("anti-spoof", func(t *testing.T) {
		setT(t)
		spoof := aAddr
		for range 100 {
			spoof = spoof.Next()
		}
		iface := fmt.Sprintf("$(ip -br link | awk 'tolower($3) == \"%s\" {print $1}')", B.privateNIC)
		B.con.mustExec(fmt.Sprintf("sudo ip addr add %s/32 dev %s", spoof, iface), time.Minute)
		out, code := B.con.exec(fmt.Sprintf("ping -c2 -W1 -I %s %s", spoof, A.privateIP), time.Minute)
		require.NotZero(t, code, "B sent from %s:\n%s", spoof, out)
		require.Contains(t, out, "100% packet loss")
		require.True(t, ping(B.con, B.privateIP, A.privateIP, 10*time.Second), "B can't ping A from its own address")
		B.con.mustExec(fmt.Sprintf("sudo ip addr del %s/32 dev %s", spoof, iface), time.Minute)
	}))

	var fwID int64
	deleteFirewall := func() {
		if fwID == 0 {
			return
		}
		var del fwActions
		call(t, app, e.c, http.MethodDelete, fmt.Sprintf("/v1/firewalls/%d", fwID), nil, http.StatusOK, &del)
		waitActions(t, app, e.c, del.Actions)
		fwID = 0
	}
	t.Cleanup(func() {
		if fwID != 0 {
			res := e.c.Send(http.MethodDelete, fmt.Sprintf("/v1/firewalls/%d", fwID), nil)
			t.Logf("cleanup delete firewall %d: %d %s", fwID, res.StatusCode, app.Body(res))
		}
	})

	require.True(t, t.Run("firewall", func(t *testing.T) {
		setT(t)
		require.True(t, ping(B.con, "", A.publicIP, netPing), "B can't ping A's public address before the firewall")
		var created struct {
			Firewall struct {
				ID int64 `json:"id"`
			} `json:"firewall"`
			Actions []action `json:"actions"`
		}
		sshIn := map[string]any{"direction": "in", "protocol": "tcp", "port": "22", "source_ips": []string{"0.0.0.0/0"}}
		call(t, app, e.c, http.MethodPost, "/v1/firewalls", map[string]any{"name": fmt.Sprintf("net-ssh-%d", stamp),
			"rules": []any{sshIn}, "apply_to": []any{
				map[string]any{"type": "server", "server": map[string]any{"id": A.id}},
				map[string]any{"type": "server", "server": map[string]any{"id": B.id}},
			}}, http.StatusCreated, &created)
		fwID = created.Firewall.ID
		waitActions(t, app, e.c, created.Actions)
		for _, s := range []netServer{A, B} {
			opts, err := pc.FirewallOptions(ctx, s.node, s.vmid)
			require.NoError(t, err)
			require.Equal(t, "DROP", opts["policy_in"], "%s firewall options %v", s.name, opts)
		}
		// The public address going quiet shows the rules are loaded.
		blocked := false
		for deadline := time.Now().Add(netPing); time.Now().Before(deadline); time.Sleep(time.Second) {
			if _, code := B.con.exec("ping -c1 -W1 "+A.publicIP, time.Minute); code != 0 {
				blocked = true
				break
			}
		}
		require.True(t, blocked, "B still pings A's public address under the SSH-only firewall")
		require.True(t, ping(B.con, "", A.privateIP, 10*time.Second), "B can't ping A's private address under the firewall")
		require.True(t, ping(A.con, "", B.privateIP, 10*time.Second), "A can't ping B's private address under the firewall")
	}))

	require.True(t, t.Run("detach", func(t *testing.T) {
		setT(t)
		var dt netAction
		call(t, app, e.c, http.MethodPost, fmt.Sprintf("/v1/servers/%d/actions/detach_from_network", B.id),
			map[string]string{"network": "a"}, http.StatusCreated, &dt)
		waitAction(t, app, e.c, dt.Action.ID)
		cfg, err := pc.Config(ctx, B.node, B.vmid)
		require.NoError(t, err)
		_, has := cfg["net1"]
		require.False(t, has, "B still has net1: %s", cfg["net1"])
		out := B.con.mustExec(fmt.Sprintf("for i in $(seq 30); do ip -br link | grep -qi '%s' || break; sleep 1; done; ip -br link", B.privateNIC),
			time.Minute)
		require.NotContains(t, strings.ToLower(out), B.privateNIC, "B's private NIC is still in the guest")
		var got serverNets
		call(t, app, e.c, http.MethodGet, fmt.Sprintf("/v1/servers/%d", B.id), nil, http.StatusOK, &got)
		require.Empty(t, got.PrivateNet)
	}))

	require.True(t, t.Run("delete", func(t *testing.T) {
		setT(t)
		for _, s := range []*netServer{&A, &B, &C} {
			s.con.close()
		}
		deleteFirewall()
		slots := map[int64]string{}
		for _, n := range []int64{aNet, bNet} {
			sl, err := app.Ent.VNetSlot.Query().Where(vnetslot.NetworkID(n)).Only(ctx)
			require.NoError(t, err, "slot of network %d", n)
			slots[n] = sl.Vnet
		}
		t.Logf("slots: a=%s b=%s", slots[aNet], slots[bNet])
		for _, s := range []netServer{A, B, C} {
			var a actionBody
			call(t, app, s.c, http.MethodDelete, fmt.Sprintf("/v1/servers/%d", s.id), nil, http.StatusOK, &a)
			waitAction(t, app, s.c, a.Action.ID)
			require.Empty(t, taggedVMs(t, pc, s.id))
		}
		call(t, app, e.c, http.MethodDelete, fmt.Sprintf("/v1/networks/%d", aNet), nil, http.StatusNoContent, nil)
		call(t, app, e.c2, http.MethodDelete, fmt.Sprintf("/v1/networks/%d", bNet), nil, http.StatusNoContent, nil)
		for _, v := range slots {
			require.Equal(t, vnetslot.StateCooling, e.slot(t, v).State, "slot %s after the delete", v)
		}
		e.reconcilePass(t)
		for _, v := range slots {
			sl := e.slot(t, v)
			require.Equal(t, vnetslot.StateFree, sl.State, "slot %s after a reconcile pass (%s)", v, sl.Reason)
		}
	}))
}

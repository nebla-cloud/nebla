//go:build e2e

package e2e_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nebla-cloud/nebla/internal/pve"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

const (
	fwUser      = "e2e"
	fwStateWait = 60 * time.Second
	fwBootWait  = 6 * time.Minute
	udpPort     = 9999
)

// spoofSource picks the highest host address of the pool CIDR outside the allocation range, gateway and avoid.
func spoofSource(t *testing.T, tg target, avoid ...string) string {
	t.Helper()
	p, err := netip.ParsePrefix(tg.cidr)
	require.NoError(t, err, "E2E_POOL_CIDR")
	p = p.Masked()
	lo, err := netip.ParseAddr(tg.rangeStart)
	require.NoError(t, err, "E2E_RANGE start")
	hi, err := netip.ParseAddr(tg.rangeEnd)
	require.NoError(t, err, "E2E_RANGE end")
	last := p.Addr()
	for n := last.Next(); n.IsValid() && p.Contains(n); n = n.Next() {
		last = n
	}
	// last is the broadcast address; start one below it.
	for a := last.Prev(); a.IsValid() && p.Contains(a) && a != p.Addr(); a = a.Prev() {
		inRange := a.Compare(lo) >= 0 && a.Compare(hi) <= 0
		if inRange || a.String() == tg.gateway || slices.Contains(avoid, a.String()) {
			continue
		}
		return a.String()
	}
	t.Fatalf("no address in %s outside range %s-%s to spoof from", tg.cidr, tg.rangeStart, tg.rangeEnd)
	return ""
}

// fwUserData starts an HTTP listener on 8080 and a UDP logger on 9999, and adds a sudo user with key.
func fwUserData(key string) string {
	return `#cloud-config
users:
  - name: ` + fwUser + `
    shell: /bin/bash
    sudo: ALL=(ALL) NOPASSWD:ALL
    ssh_authorized_keys:
      - ` + key + `
write_files:
  - path: /usr/local/bin/e2e-udp.py
    permissions: "0755"
    content: |
      import socket
      s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
      s.bind(("0.0.0.0", 9999))
      while True:
          d, a = s.recvfrom(2048)
          with open("/run/e2e-udp.log", "a") as f:
              f.write("%s %s\n" % (a[0], d.decode(errors="replace")))
  - path: /usr/local/bin/e2e-send.py
    permissions: "0755"
    content: |
      import socket, struct, sys
      src, dst, msg = sys.argv[1], sys.argv[2], sys.argv[3].encode()
      udp = struct.pack("!HHHH", 40000, 9999, 8 + len(msg), 0) + msg
      ip = struct.pack("!BBHHHBBH4s4s", 0x45, 0, 20 + len(udp), 0, 0, 64, 17, 0,
                       socket.inet_aton(src), socket.inet_aton(dst))
      s = socket.socket(socket.AF_INET, socket.SOCK_RAW, socket.IPPROTO_RAW)
      for _ in range(5):
          s.sendto(ip + udp, (dst, 0))
runcmd:
  - [systemd-run, --unit=e2e-http, python3, -m, http.server, "8080", --bind, 0.0.0.0, --directory, /tmp]
  - [systemd-run, --unit=e2e-udp, python3, /usr/local/bin/e2e-udp.py]
`
}

type fwServer struct {
	id   int64
	ip   string
	node string
	vmid int
}

type fwActions struct {
	Actions []action `json:"actions"`
}

func waitActions(t *testing.T, app *testutil.App, c *testutil.Client, as []action) {
	t.Helper()
	for _, a := range as {
		waitAction(t, app, c, a.ID)
	}
}

func tcpOpen(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// waitTCP polls addr until its reachability is want; it returns when the first matching attempt started.
func waitTCP(t *testing.T, addr string, want bool, limit time.Duration) time.Time {
	t.Helper()
	deadline := time.Now().Add(limit)
	for {
		start := time.Now()
		if tcpOpen(addr) == want {
			return start
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s reachable=%v after %s, want %v", addr, !want, limit, want)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func sshRun(signer ssh.Signer, host, cmd string) (string, error) {
	cfg := &ssh.ClientConfig{User: fwUser, Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), Timeout: 5 * time.Second}
	cl, err := ssh.Dial("tcp", net.JoinHostPort(host, "22"), cfg)
	if err != nil {
		return "", err
	}
	defer cl.Close()
	s, err := cl.NewSession()
	if err != nil {
		return "", err
	}
	defer s.Close()
	var out bytes.Buffer
	s.Stdout, s.Stderr = &out, &out
	err = s.Run(cmd)
	return out.String(), err
}

func waitSSH(t *testing.T, signer ssh.Signer, host, cmd string, limit time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(limit)
	for {
		out, err := sshRun(signer, host, cmd)
		if err == nil {
			return out
		}
		if time.Now().After(deadline) {
			t.Fatalf("ssh %s %q: %v: %s", host, cmd, err, out)
		}
		time.Sleep(5 * time.Second)
	}
}

// pveGet is a read-only GET against the Proxmox API; ok is false on a non-2xx status.
func pveGet(t *testing.T, tg target, path string, out any) (ok bool) {
	t.Helper()
	conn := tg.conn()
	hc := &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{TLSClientConfig: pve.TLSConfig(conn)}}
	req, err := http.NewRequest(http.MethodGet, strings.TrimRight(tg.url, "/")+"/api2/json"+path, nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "PVEAPIToken="+conn.TokenID+"="+conn.Secret)
	res, err := hc.Do(req)
	require.NoError(t, err, path)
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	if res.StatusCode < 200 || res.StatusCode > 299 {
		t.Logf("GET %s: %d %s", path, res.StatusCode, strings.TrimSpace(string(b)))
		return false
	}
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(b, &env), string(b))
	if out != nil && len(env.Data) > 0 && string(env.Data) != "null" {
		require.NoError(t, json.Unmarshal(env.Data, out), string(env.Data))
	}
	return true
}

func TestFirewallE2E(t *testing.T) {
	e := newEnv(t)
	app, c, pc, tg := e.app, e.c, e.pc, e.tg
	if !e.pubNet["ipv4"] {
		t.Skip("the firewall e2e needs an IPv4 pool")
	}
	ctx := context.Background()

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	signer, err := ssh.NewSignerFromKey(priv)
	require.NoError(t, err)
	sshPub, err := ssh.NewPublicKey(pub)
	require.NoError(t, err)
	ud := fwUserData(strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub))))

	// Registered first so it runs last, after every server cleanup.
	var ids []int64
	var made []fwServer
	t.Cleanup(func() {
		clean := true
		for _, id := range ids {
			if left := taggedVMs(t, pc, id); len(left) > 0 {
				clean = false
				t.Errorf("VMs tagged %s left on the target: %+v", servers.ServerTag(id), left)
			}
		}
		for _, s := range made {
			for _, sub := range []string{"options", "rules", "ipset"} {
				var v any
				if pveGet(t, tg, fmt.Sprintf("/nodes/%s/qemu/%d/firewall/%s", s.node, s.vmid, sub), &v) && !emptyJSON(v) {
					clean = false
					t.Errorf("VM %d firewall %s still present: %v", s.vmid, sub, v)
				}
			}
		}
		for _, p := range []string{"/cluster/firewall/ipset", "/cluster/firewall/aliases"} {
			var items []struct {
				Name string `json:"name"`
			}
			if !pveGet(t, tg, p, &items) {
				clean = false
				t.Logf("could not read %s; cluster cleanup unverified", p)
			}
			for _, it := range items {
				if strings.HasPrefix(strings.ToLower(it.Name), "nebla") {
					clean = false
					t.Errorf("cluster %s has %q", p, it.Name)
				}
			}
		}
		t.Logf("CLEANUP: clean=%v (servers %+v)", clean, made)
	})

	stamp := time.Now().Unix()
	create := func(name string, labels map[string]string) (int64, action) {
		var created struct {
			Server struct {
				ID int64 `json:"id"`
			} `json:"server"`
			Action action `json:"action"`
		}
		call(t, app, c, http.MethodPost, "/v1/servers", map[string]any{"name": name, "server_type": "e2e-small",
			"image": "e2e-debian", "location": "e2e", "labels": labels, "user_data": ud,
			"public_net": map[string]bool{"ipv4": true, "ipv6": false}}, http.StatusCreated, &created)
		sid := created.Server.ID
		ids = append(ids, sid)
		t.Cleanup(func() { cleanupServer(t, app, c, sid) })
		return sid, created.Action
	}
	// One at a time: concurrent clones can race on the next VMID.
	idA, actA := create(fmt.Sprintf("nebla-fw-a-%d", stamp), map[string]string{"role": "client"})
	waitAction(t, app, c, actA.ID)
	idB, actB := create(fmt.Sprintf("nebla-fw-b-%d", stamp), map[string]string{"role": "web"})
	waitAction(t, app, c, actB.ID)
	lookup := func(sid int64) fwServer {
		var s struct {
			PublicNet struct {
				IPv4 *struct {
					IP string `json:"ip"`
				} `json:"ipv4"`
			} `json:"public_net"`
		}
		call(t, app, c, http.MethodGet, fmt.Sprintf("/v1/servers/%d", sid), nil, http.StatusOK, &s)
		require.NotNil(t, s.PublicNet.IPv4, "server %d has no IPv4", sid)
		vms := taggedVMs(t, pc, sid)
		require.Len(t, vms, 1)
		return fwServer{id: sid, ip: s.PublicNet.IPv4.IP, node: vms[0].Node, vmid: vms[0].VMID}
	}
	waitStatus(t, app, c, idA, "running")
	waitStatus(t, app, c, idB, "running")
	A, B := lookup(idA), lookup(idB)
	made = []fwServer{A, B}
	t.Logf("A=%+v B=%+v", A, B)
	b8080 := net.JoinHostPort(B.ip, "8080")

	// Boot: both reachable over SSH, B's listener up; B tells us the runner's address as it sees it.
	waitSSH(t, signer, A.ip, "true", fwBootWait)
	sshClient := waitSSH(t, signer, B.ip, "echo $SSH_CLIENT", fwBootWait)
	runner := strings.Fields(sshClient)
	require.NotEmpty(t, runner, "SSH_CLIENT on B: %q", sshClient)
	runnerIP, err := netip.ParseAddr(runner[0])
	require.NoError(t, err, "SSH_CLIENT on B: %q", sshClient)
	t.Logf("runner address seen by B: %s", runnerIP)
	waitTCP(t, b8080, true, fwBootWait)

	require.True(t, t.Run("dhcp default", func(t *testing.T) {
		opts, err := pc.FirewallOptions(ctx, A.node, A.vmid)
		require.NoError(t, err)
		t.Logf("A firewall options as created: %v", opts)
		require.NoError(t, pc.SetFirewallOptions(ctx, A.node, A.vmid, map[string]string{"delete": "dhcp"}))
		opts, err = pc.FirewallOptions(ctx, A.node, A.vmid)
		require.NoError(t, err)
		v, set := opts["dhcp"]
		t.Logf("DHCP DEFAULT: options without a dhcp key read back dhcp=%q (set=%v): %v", v, set, opts)
		require.True(t, !set || v == "0",
			"Proxmox reports dhcp=%q for a VM whose options have no dhcp key; v1 VMs would differ from dhcp=0", v)
		require.NoError(t, pc.SetFirewallOptions(ctx, A.node, A.vmid, map[string]string{"dhcp": "0"}))
	}))

	t.Run("rule load time", func(t *testing.T) {
		written := time.Now()
		require.NoError(t, pc.SetFirewallOptions(ctx, B.node, B.vmid, map[string]string{"policy_in": "DROP"}))
		blocked := waitTCP(t, b8080, false, fwStateWait)
		load := blocked.Sub(written)
		t.Logf("RULE LOAD TIME: policy_in=DROP took %s to block %s (FirewallSettle %s)", load.Round(100*time.Millisecond),
			b8080, servers.FirewallSettle)
		require.NoError(t, pc.SetFirewallOptions(ctx, B.node, B.vmid, map[string]string{"policy_in": "ACCEPT"}))
		reopened := waitTCP(t, b8080, true, fwStateWait)
		t.Logf("policy_in=ACCEPT reopened %s after %s", b8080, reopened.Sub(blocked).Round(100*time.Millisecond))
		require.Less(t, load, servers.FirewallSettle, "rule load time exceeds FirewallSettle")
	})

	var created struct {
		Firewall struct {
			ID int64 `json:"id"`
		} `json:"firewall"`
		Actions []action `json:"actions"`
	}
	sshRule := map[string]any{"direction": "in", "protocol": "tcp", "port": "22",
		"source_ips": []string{netip.PrefixFrom(runnerIP, runnerIP.BitLen()).String()}, "description": "runner ssh"}
	udpRule := map[string]any{"direction": "in", "protocol": "udp", "port": fmt.Sprint(udpPort),
		"source_ips": []string{"0.0.0.0/0"}, "description": "spoof probe"}
	selector := []any{map[string]any{"type": "label_selector", "label_selector": map[string]any{"selector": "role=web"}}}
	call(t, app, c, http.MethodPost, "/v1/firewalls", map[string]any{"name": fmt.Sprintf("web-%d", stamp),
		"rules": []any{sshRule, udpRule}}, http.StatusCreated, &created)
	fwID := created.Firewall.ID
	t.Cleanup(func() {
		res := c.Send(http.MethodDelete, fmt.Sprintf("/v1/firewalls/%d", fwID), nil)
		t.Logf("cleanup delete firewall %d: %d %s", fwID, res.StatusCode, app.Body(res))
	})
	waitActions(t, app, c, created.Actions)

	require.True(t, t.Run("apply by selector", func(t *testing.T) {
		var as fwActions
		call(t, app, c, http.MethodPost, fmt.Sprintf("/v1/firewalls/%d/actions/apply_to_resources", fwID),
			map[string]any{"apply_to": selector}, http.StatusCreated, &as)
		require.NotEmpty(t, as.Actions, "applying role=web should touch B")
		waitActions(t, app, c, as.Actions)
		opts, err := pc.FirewallOptions(ctx, B.node, B.vmid)
		require.NoError(t, err)
		for k, v := range map[string]string{"enable": "1", "ipfilter": "1", "macfilter": "1", "policy_in": "DROP"} {
			require.Equal(t, v, opts[k], "B option %s: %v", k, opts)
		}
		waitTCP(t, b8080, false, fwStateWait)
		waitSSH(t, signer, B.ip, "true", fwStateWait)
		aOpts, err := pc.FirewallOptions(ctx, A.node, A.vmid)
		require.NoError(t, err)
		require.Equal(t, "ACCEPT", aOpts["policy_in"], "A has no firewall: %v", aOpts)
	}))

	require.True(t, t.Run("anti-spoofing", func(t *testing.T) {
		// Both probes go out of A as raw packets; only the source differs, and B's firewall admits either.
		send := func(src, msg string) {
			cmd := fmt.Sprintf("sudo python3 /usr/local/bin/e2e-send.py %s %s %s", src, B.ip, msg)
			out, err := sshRun(signer, A.ip, cmd)
			require.NoError(t, err, "%s: %s", cmd, out)
		}
		logB := func() string { return waitSSH(t, signer, B.ip, "cat /run/e2e-udp.log 2>/dev/null; true", fwStateWait) }
		deadline := time.Now().Add(fwStateWait)
		for !strings.Contains(logB(), A.ip+" real") {
			require.False(t, time.Now().After(deadline), "control packet from A's own address never reached B: %q", logB())
			send(A.ip, "real")
			time.Sleep(2 * time.Second)
		}
		spoofSrc := spoofSource(t, tg, A.ip, B.ip)
		t.Logf("SPOOF SOURCE: %s (pool %s, range %s-%s)", spoofSrc, tg.cidr, tg.rangeStart, tg.rangeEnd)
		send(spoofSrc, "spoof")
		time.Sleep(10 * time.Second)
		got := logB()
		t.Logf("B udp log after spoof attempt:\n%s", got)
		require.NotContains(t, got, "spoof", "a packet from A with source %s reached B", spoofSrc)
	}))

	require.True(t, t.Run("remove", func(t *testing.T) {
		var as fwActions
		call(t, app, c, http.MethodPost, fmt.Sprintf("/v1/firewalls/%d/actions/remove_from_resources", fwID),
			map[string]any{"remove_from": selector}, http.StatusCreated, &as)
		waitActions(t, app, c, as.Actions)
		start := time.Now()
		open := waitTCP(t, b8080, true, fwStateWait)
		t.Logf("8080 reachable %s after remove finished", open.Sub(start).Round(100*time.Millisecond))
		opts, err := pc.FirewallOptions(ctx, B.node, B.vmid)
		require.NoError(t, err)
		require.Equal(t, "ACCEPT", opts["policy_in"], "%v", opts)
		require.Equal(t, "1", opts["ipfilter"], "%v", opts)
	}))

	require.True(t, t.Run("stopped server", func(t *testing.T) {
		var a actionBody
		call(t, app, c, http.MethodPost, fmt.Sprintf("/v1/servers/%d/actions/poweroff", B.id), nil, http.StatusCreated, &a)
		waitAction(t, app, c, a.Action.ID)
		waitStatus(t, app, c, B.id, "off")
		var as fwActions
		call(t, app, c, http.MethodPost, fmt.Sprintf("/v1/firewalls/%d/actions/apply_to_resources", fwID),
			map[string]any{"apply_to": selector}, http.StatusCreated, &as)
		waitActions(t, app, c, as.Actions)
		webRule := map[string]any{"direction": "in", "protocol": "tcp", "port": "80", "source_ips": []string{"0.0.0.0/0"},
			"description": "web"}
		call(t, app, c, http.MethodPost, fmt.Sprintf("/v1/firewalls/%d/actions/set_rules", fwID),
			map[string]any{"rules": []any{sshRule, webRule}}, http.StatusCreated, &as)
		require.NotEmpty(t, as.Actions, "set_rules should apply to B")
		waitActions(t, app, c, as.Actions)
		rules, err := pc.FirewallRules(ctx, B.node, B.vmid)
		require.NoError(t, err)
		dports := make([]string, 0, len(rules))
		for _, r := range rules {
			dports = append(dports, r.Dport)
		}
		t.Logf("B rules while off: %+v", rules)
		require.True(t, slices.Contains(dports, "80"), "port 80 rule missing on stopped B: %v", dports)
		require.False(t, slices.Contains(dports, fmt.Sprint(udpPort)), "udp rule should be gone: %v", dports)
		require.Equal(t, "off", serverStatus(t, app, c, B.id))
	}))

	for _, s := range []fwServer{A, B} {
		var a actionBody
		call(t, app, c, http.MethodDelete, fmt.Sprintf("/v1/servers/%d", s.id), nil, http.StatusOK, &a)
		waitAction(t, app, c, a.Action.ID)
		require.Empty(t, taggedVMs(t, pc, s.id))
	}
	var del fwActions
	call(t, app, c, http.MethodDelete, fmt.Sprintf("/v1/firewalls/%d", fwID), nil, http.StatusOK, &del)
	waitActions(t, app, c, del.Actions)
}

func emptyJSON(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case []any:
		return len(x) == 0
	case map[string]any:
		return len(x) == 0
	}
	return false
}

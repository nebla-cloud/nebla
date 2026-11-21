//go:build e2e

package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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
)

// The runner can't reach guests, so traffic is judged by B's netin counter, sampled per window.
const (
	fwBootWait   = 6 * time.Minute
	fwStateWait  = 60 * time.Second
	fwHoldFlat   = 20 * time.Second
	flowWindow   = 2 * time.Second
	flatBytes    = 5 << 10
	flowingBytes = 50 << 10
	udpPort      = 9999
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

// senderUserData runs two ~300 KB/s UDP streams to dst:9999 as a root service: one from the real address, one spoofed.
func senderUserData(dst, spoof string) string {
	return `#cloud-config
write_files:
  - path: /usr/local/bin/e2e-send.py
    permissions: "0755"
    content: |
      import socket, struct, sys, threading, time
      dst, spoof, port = sys.argv[1], sys.argv[2], 9999
      msg = b"x" * 1000
      def pace(send):
          while True:
              t = time.monotonic()
              for _ in range(30):
                  try:
                      send()
                  except OSError:
                      pass
              time.sleep(max(0, 0.1 - (time.monotonic() - t)))
      def real():
          s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
          pace(lambda: s.sendto(msg, (dst, port)))
      def spoofed():
          udp = struct.pack("!HHHH", 40000, port, 8 + len(msg), 0) + msg
          ip = struct.pack("!BBHHHBBH4s4s", 0x45, 0, 20 + len(udp), 0, 0, 64, 17, 0,
                           socket.inet_aton(spoof), socket.inet_aton(dst))
          s = socket.socket(socket.AF_INET, socket.SOCK_RAW, socket.IPPROTO_RAW)
          pace(lambda: s.sendto(ip + udp, (dst, 0)))
      threading.Thread(target=spoofed, daemon=True).start()
      real()
  - path: /etc/systemd/system/e2e-send.service
    content: |
      [Unit]
      After=network-online.target
      Wants=network-online.target
      [Service]
      ExecStart=/usr/bin/python3 /usr/local/bin/e2e-send.py ` + dst + ` ` + spoof + `
      Restart=always
      [Install]
      WantedBy=multi-user.target
runcmd:
  - [systemctl, daemon-reload]
  - [systemctl, enable, --now, e2e-send.service]
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

// waitActions waits for each action and returns the latest finish time.
func waitActions(t *testing.T, app *testutil.App, c *testutil.Client, as []action) time.Time {
	t.Helper()
	var last time.Time
	for _, a := range as {
		waitAction(t, app, c, a.ID)
		var got struct {
			Finished *time.Time `json:"finished"`
		}
		call(t, app, c, http.MethodGet, fmt.Sprintf("/v1/actions/%d", a.ID), nil, http.StatusOK, &got)
		if got.Finished != nil && got.Finished.After(last) {
			last = *got.Finished
		}
	}
	return last
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

func netin(t *testing.T, tg target, s fwServer) float64 {
	t.Helper()
	var st struct {
		Netin float64 `json:"netin"`
	}
	require.True(t, pveGet(t, tg, fmt.Sprintf("/nodes/%s/qemu/%d/status/current", s.node, s.vmid), &st), "status of VM %d", s.vmid)
	return st.Netin
}

// dumpFirewall logs s's live firewall rules, options and net0.
func dumpFirewall(t *testing.T, pc pve.Client, s fwServer, why string) {
	t.Helper()
	ctx := context.Background()
	rules, err := pc.FirewallRules(ctx, s.node, s.vmid)
	t.Logf("[%s] VM %d rules (err=%v):", why, s.vmid, err)
	for _, r := range rules {
		t.Logf("  %+v", r)
	}
	opts, err := pc.FirewallOptions(ctx, s.node, s.vmid)
	t.Logf("[%s] VM %d options (err=%v): %v", why, s.vmid, err, opts)
	cfg, err := pc.Config(ctx, s.node, s.vmid)
	t.Logf("[%s] VM %d net0 (err=%v): %s", why, s.vmid, err, cfg["net0"])
}

// waitFlow samples s's netin until a window is flowing (or flat) as wanted and returns that window's start.
func waitFlow(t *testing.T, pc pve.Client, tg target, s fwServer, flowing bool, limit time.Duration) time.Time {
	t.Helper()
	deadline := time.Now().Add(limit)
	prevT, prev := time.Now(), netin(t, tg, s)
	for {
		time.Sleep(flowWindow)
		now, cur := time.Now(), netin(t, tg, s)
		d := cur - prev
		if (flowing && d >= flowingBytes) || (!flowing && d < flatBytes) {
			t.Logf("netin %+.0f B in %s (flowing=%v)", d, now.Sub(prevT).Round(100*time.Millisecond), flowing)
			return prevT
		}
		if now.After(deadline) {
			dumpFirewall(t, pc, s, "waitFlow timeout")
			t.Fatalf("VM %d netin never became flowing=%v within %s; last window %+.0f B", s.vmid, flowing, limit, d)
		}
		prevT, prev = now, cur
	}
}

// holdFlat fails if any window over dur shows traffic reaching s.
func holdFlat(t *testing.T, tg target, s fwServer, dur time.Duration) {
	t.Helper()
	end := time.Now().Add(dur)
	prev := netin(t, tg, s)
	for time.Now().Before(end) {
		time.Sleep(flowWindow)
		cur := netin(t, tg, s)
		require.Less(t, cur-prev, float64(flatBytes), "VM %d received %.0f B in one window", s.vmid, cur-prev)
		prev = cur
	}
}

func TestFirewallE2E(t *testing.T) {
	e := newEnv(t)
	app, c, pc, tg := e.app, e.c, e.pc, e.tg
	if !e.pubNet["ipv4"] {
		t.Skip("the firewall e2e needs an IPv4 pool")
	}
	ctx := context.Background()

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
	// One at a time: concurrent clones can race on the next VMID.
	create := func(name, userData string, labels map[string]string) fwServer {
		var created struct {
			Server struct {
				ID int64 `json:"id"`
			} `json:"server"`
			Action action `json:"action"`
		}
		body := map[string]any{"name": name, "server_type": "e2e-small", "image": "e2e-debian", "location": "e2e",
			"labels": labels, "public_net": map[string]bool{"ipv4": true, "ipv6": false}}
		if userData != "" {
			body["user_data"] = userData
		}
		call(t, app, c, http.MethodPost, "/v1/servers", body, http.StatusCreated, &created)
		sid := created.Server.ID
		ids = append(ids, sid)
		t.Cleanup(func() { cleanupServer(t, app, c, sid) })
		waitAction(t, app, c, created.Action.ID)
		waitStatus(t, app, c, sid, "running")
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
		out := fwServer{id: sid, ip: s.PublicNet.IPv4.IP, node: vms[0].Node, vmid: vms[0].VMID}
		made = append(made, out)
		return out
	}
	// B first: A's sender needs B's address at boot.
	B := create(fmt.Sprintf("nebla-fw-b-%d", stamp), "", map[string]string{"role": "web"})
	spoof := spoofSource(t, tg, B.ip)
	A := create(fmt.Sprintf("nebla-fw-a-%d", stamp), senderUserData(B.ip, spoof), map[string]string{"role": "client"})
	require.NotEqual(t, A.ip, spoof)
	t.Logf("A=%+v B=%+v", A, B)
	var nodeFW map[string]any
	pveGet(t, tg, fmt.Sprintf("/nodes/%s/firewall/options", B.node), &nodeFW)
	nodeVer, err := pc.NodeVersion(ctx, B.node)
	t.Logf("PVE node %s version %q (err=%v); node firewall options %v; nftables=%v", B.node, nodeVer, err, nodeFW, nodeFW["nftables"])
	t.Logf("SPOOF SOURCE: %s (pool %s, range %s-%s)", spoof, tg.cidr, tg.rangeStart, tg.rangeEnd)

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

	require.True(t, t.Run("baseline", func(t *testing.T) {
		waitFlow(t, pc, tg, B, true, fwBootWait)
	}))

	rule := func(proto, port, src, desc string) map[string]any {
		return map[string]any{"direction": "in", "protocol": proto, "port": port, "source_ips": []string{src}, "description": desc}
	}
	sshAny := rule("tcp", "22", "0.0.0.0/0", "ssh")
	udpFrom := func(ip string) map[string]any { return rule("udp", fmt.Sprint(udpPort), ip+"/32", "probe") }
	selector := []any{map[string]any{"type": "label_selector", "label_selector": map[string]any{"selector": "role=web"}}}
	var created struct {
		Firewall struct {
			ID int64 `json:"id"`
		} `json:"firewall"`
		Actions []action `json:"actions"`
	}
	call(t, app, c, http.MethodPost, "/v1/firewalls", map[string]any{"name": fmt.Sprintf("web-%d", stamp),
		"rules": []any{sshAny}}, http.StatusCreated, &created)
	fwID := created.Firewall.ID
	t.Cleanup(func() {
		res := c.Send(http.MethodDelete, fmt.Sprintf("/v1/firewalls/%d", fwID), nil)
		t.Logf("cleanup delete firewall %d: %d %s", fwID, res.StatusCode, app.Body(res))
	})
	waitActions(t, app, c, created.Actions)
	fwPost := func(rest string, body any) time.Time {
		var as fwActions
		raw, _ := json.Marshal(body)
		t.Logf("POST %s: %s", rest, raw)
		call(t, app, c, http.MethodPost, fmt.Sprintf("/v1/firewalls/%d/actions/%s", fwID, rest), body, http.StatusCreated, &as)
		require.NotEmpty(t, as.Actions, "%s should touch B", rest)
		done := waitActions(t, app, c, as.Actions)
		dumpFirewall(t, pc, B, "after "+rest)
		return done
	}
	setRules := func(rules ...any) time.Time { return fwPost("set_rules", map[string]any{"rules": rules}) }

	require.True(t, t.Run("apply blocks", func(t *testing.T) {
		done := fwPost("apply_to_resources", map[string]any{"apply_to": selector})
		flat := waitFlow(t, pc, tg, B, false, fwStateWait)
		load := flat.Sub(done)
		t.Logf("RULE LOAD TIME: %s from the apply action finishing to B's netin going flat (FirewallSettle %s, %s windows)",
			load.Round(100*time.Millisecond), servers.FirewallSettle, flowWindow)
		opts, err := pc.FirewallOptions(ctx, B.node, B.vmid)
		require.NoError(t, err)
		for k, v := range map[string]string{"enable": "1", "ipfilter": "1", "macfilter": "1", "policy_in": "DROP"} {
			require.Equal(t, v, opts[k], "B option %s: %v", k, opts)
		}
		require.Less(t, load, servers.FirewallSettle, "rule load time exceeds FirewallSettle")
	}))

	require.True(t, t.Run("allow from A", func(t *testing.T) {
		setRules(sshAny, udpFrom(A.ip))
		waitFlow(t, pc, tg, B, true, fwStateWait)
	}))

	require.True(t, t.Run("anti-spoofing", func(t *testing.T) {
		setRules(udpFrom(spoof))
		waitFlow(t, pc, tg, B, false, fwStateWait)
		holdFlat(t, tg, B, fwHoldFlat)
		t.Logf("ANTI-SPOOF: B allows only udp %d from %s and stayed flat for %s; A's spoofed stream never left A",
			udpPort, spoof, fwHoldFlat)
	}))

	require.True(t, t.Run("remove", func(t *testing.T) {
		fwPost("remove_from_resources", map[string]any{"remove_from": selector})
		waitFlow(t, pc, tg, B, true, fwStateWait)
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
		fwPost("apply_to_resources", map[string]any{"apply_to": selector})
		setRules(sshAny, rule("tcp", "80", "0.0.0.0/0", "web"))
		rules, err := pc.FirewallRules(ctx, B.node, B.vmid)
		require.NoError(t, err)
		t.Logf("B rules while off: %+v", rules)
		dports := make([]string, 0, len(rules))
		for _, r := range rules {
			dports = append(dports, r.Dport)
		}
		require.True(t, slices.Contains(dports, "80"), "port 80 rule missing on stopped B: %v", dports)
		require.True(t, slices.Contains(dports, "22"), "port 22 rule missing on stopped B: %v", dports)
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

// emptyJSON reports whether a Proxmox read holds nothing; a lone digest is an empty config file.
func emptyJSON(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case []any:
		return len(x) == 0
	case map[string]any:
		_, hasDigest := x["digest"]
		return len(x) == 0 || (len(x) == 1 && hasDigest)
	}
	return false
}

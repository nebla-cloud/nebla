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
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

type pipOut struct {
	ID     int64  `json:"id"`
	IP     string `json:"ip"`
	Status string `json:"status"`
	Server *struct {
		ID int64 `json:"id"`
	} `json:"server"`
	AutoDelete bool `json:"auto_delete"`
}

type pipServerOut struct {
	PublicNet struct {
		IPv4 *struct {
			ID int64  `json:"id"`
			IP string `json:"ip"`
		} `json:"ipv4"`
	} `json:"public_net"`
	Reruns bool `json:"ip_change_reruns_cloud_init"`
}

type pipAction struct {
	Action   action   `json:"action"`
	Warnings []string `json:"warnings"`
}

// pipUserData is fipUserData plus a runcmd that counts cloud-init user-data runs in /root/runs.
func pipUserData(t *testing.T, pw string, priv ed25519.PrivateKey, pub ssh.PublicKey) string {
	t.Helper()
	var doc map[string]any
	require.NoError(t, json.Unmarshal([]byte(strings.TrimPrefix(fipUserData(t, pw, priv, pub), "#cloud-config\n")), &doc))
	doc["runcmd"] = []any{[]string{"sh", "-c", "date >> /root/runs"}}
	b, err := json.Marshal(doc)
	require.NoError(t, err)
	return "#cloud-config\n" + string(b) + "\n"
}

// keyscan reads the ed25519 host key that answers on ip, as seen from p.
func keyscan(t *testing.T, p *serialConsole, ip string, limit time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(limit)
	for {
		out, code := p.exec("ssh-keyscan -T 5 -t ed25519 "+ip+" 2>/dev/null | cut -d' ' -f2,3", time.Minute)
		if code == 0 && strings.HasPrefix(out, "ssh-ed25519 ") {
			return out
		}
		if time.Now().After(deadline) {
			t.Fatalf("no ed25519 host key on %s from %s within %s; last: %q", ip, p.name, limit, out)
		}
		time.Sleep(2 * time.Second)
	}
}

func hostKey(s fipServer) string {
	return s.con.mustExec("sudo cat /etc/ssh/ssh_host_ed25519_key.pub | cut -d' ' -f1,2", time.Minute)
}

func ipsetHas(t *testing.T, s fipServer, e fipEnv, ip string) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cidrs, _, err := e.pc.FirewallIPSet(ctx, s.node, s.vmid)
	require.NoError(t, err)
	return slices.ContainsFunc(cidrs, func(c string) bool { return strings.TrimSuffix(c, "/32") == ip })
}

func TestPrimaryIPE2E(t *testing.T) {
	if strings.Contains(os.Getenv("E2E_POOL_CIDR"), ":") {
		t.Skip("the primary IP e2e needs an IPv4 pool")
	}
	images := strings.Split(envOr("E2E_PIP_IMAGES", "debian-13,ubuntu-24.04"), ",")
	for i := range images {
		images[i] = strings.TrimSpace(images[i])
	}
	e := newFIPEnv(t, images)
	for _, img := range images {
		t.Run(img, func(t *testing.T) { pipImage(t, e, img) })
	}
}

func pipImage(t *testing.T, e fipEnv, img string) {
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
	userData := pipUserData(t, pw, priv, spub)

	stamp := time.Now().Unix()
	var ids, pips []int64
	t.Cleanup(func() {
		for _, id := range ids {
			if left := taggedVMs(t, pc, id); len(left) > 0 {
				t.Errorf("VMs tagged %s left on the target: %+v", servers.ServerTag(id), left)
			}
		}
	})
	// Registered before any server, so it runs after the servers are gone.
	t.Cleanup(func() {
		for _, id := range pips {
			path := fmt.Sprintf("/v1/primary_ips/%d", id)
			for deadline := time.Now().Add(3 * time.Minute); ; time.Sleep(5 * time.Second) {
				res := c.Send(http.MethodDelete, path, nil)
				b := app.Body(res)
				if res.StatusCode == http.StatusNoContent || res.StatusCode == http.StatusNotFound {
					break
				}
				if time.Now().After(deadline) {
					t.Errorf("cleanup: primary IP %d was not deleted: %d %s", id, res.StatusCode, b)
					break
				}
				t.Logf("cleanup delete primary IP %d: %d %s; retrying", id, res.StatusCode, b)
			}
		}
	})

	getPIP := func(id int64) pipOut {
		var p pipOut
		call(t, app, c, http.MethodGet, fmt.Sprintf("/v1/primary_ips/%d", id), nil, http.StatusOK, &p)
		return p
	}
	// waitUnassigned waits for an IP to be free again: unassigned and no longer releasing.
	waitUnassigned := func(id int64) pipOut {
		deadline := time.Now().Add(2 * time.Minute)
		for {
			p := getPIP(id)
			if p.Server == nil && p.Status == "active" {
				return p
			}
			if time.Now().After(deadline) {
				t.Fatalf("primary IP %d still %s on %+v", id, p.Status, p.Server)
			}
			time.Sleep(2 * time.Second)
		}
	}
	getServer := func(sid int64) pipServerOut {
		var s pipServerOut
		call(t, app, c, http.MethodGet, fmt.Sprintf("/v1/servers/%d", sid), nil, http.StatusOK, &s)
		return s
	}
	create := func(name string, ipv4 any) int64 {
		var created struct {
			Server struct {
				ID int64 `json:"id"`
			} `json:"server"`
			Action action `json:"action"`
		}
		call(t, app, c, http.MethodPost, "/v1/servers", map[string]any{"name": name, "server_type": "e2e-small", "image": img,
			"location": e.loc, "user_data": userData, "public_net": map[string]any{"ipv4": ipv4, "ipv6": false}},
			http.StatusCreated, &created)
		sid := created.Server.ID
		ids = append(ids, sid)
		t.Cleanup(func() { cleanupServer(t, app, c, sid) })
		// One at a time: concurrent clones can race on the next VMID.
		waitAction(t, app, c, created.Action.ID)
		return sid
	}
	attach := func(sid int64, name string) fipServer {
		waitStatus(t, app, c, sid, "running")
		vms := taggedVMs(t, pc, sid)
		require.Len(t, vms, 1)
		con := openSerial(t, tg, name, vms[0].Node, vms[0].VMID)
		con.login(pw)
		return fipServer{id: sid, name: name, host: con.mustExec("hostname", time.Minute), node: vms[0].Node, vmid: vms[0].VMID,
			con: con}
	}
	power := func(sid int64, act, want string) {
		var a actionBody
		call(t, app, c, http.MethodPost, fmt.Sprintf("/v1/servers/%d/actions/%s", sid, act), nil, http.StatusCreated, &a)
		waitAction(t, app, c, a.Action.ID)
		waitStatus(t, app, c, sid, want)
	}
	stop := func(s fipServer) {
		s.con.mustExec("sync", time.Minute)
		s.con.close()
		power(s.id, "poweroff", "off")
	}
	pipAct := func(id int64, act string, body any) {
		var a pipAction
		call(t, app, c, http.MethodPost, fmt.Sprintf("/v1/primary_ips/%d/actions/%s", id, act), body, http.StatusCreated, &a)
		require.Empty(t, a.Warnings, "%s on a new server", act)
		waitAction(t, app, c, a.Action.ID)
	}
	runs := func(s fipServer) []string {
		return strings.FieldsFunc(s.con.mustExec("sudo cat /root/runs", time.Minute), func(r rune) bool { return r == '\n' })
	}

	pName := fmt.Sprintf("nebla-pip-p-%d", stamp)
	P := attach(create(pName, true), pName)

	// 1. A keeps its new IPv4 after A is deleted.
	aName := fmt.Sprintf("nebla-pip-a-%d", stamp)
	aID := create(aName, true)
	a4 := getServer(aID).PublicNet.IPv4
	require.NotNil(t, a4, "A has no IPv4")
	kept := a4.ID
	pips = append(pips, kept)
	var upd pipOut
	call(t, app, c, http.MethodPut, fmt.Sprintf("/v1/primary_ips/%d", kept), map[string]bool{"auto_delete": false},
		http.StatusOK, &upd)
	require.False(t, upd.AutoDelete)
	var del actionBody
	call(t, app, c, http.MethodDelete, fmt.Sprintf("/v1/servers/%d", aID), nil, http.StatusOK, &del)
	waitAction(t, app, c, del.Action.ID)
	require.Empty(t, taggedVMs(t, pc, aID))
	ip, _, _ := strings.Cut(waitUnassigned(kept).IP, "/")
	aIP, _, _ := strings.Cut(a4.IP, "/")
	require.Equal(t, aIP, ip, "kept IP changed address")
	t.Logf("KEEP: primary IP %d = %s outlived A", kept, ip)

	// 2. B boots on the kept IP.
	bName := fmt.Sprintf("nebla-pip-b-%d", stamp)
	bID := create(bName, strconv.FormatInt(kept, 10))
	b4 := getServer(bID).PublicNet.IPv4
	require.NotNil(t, b4, "B has no IPv4")
	require.Equal(t, kept, b4.ID)
	B := attach(bID, bName)
	bKey := hostKey(B)
	probeHost(t, P.con, ip, B.host, 2*time.Minute)
	require.Equal(t, bKey, keyscan(t, P.con, ip, time.Minute), "host key on %s is not B's", ip)
	t.Logf("REUSE: %s reached B from P", ip)

	// 3. C boots on its own IPv4, then swaps it for the kept IP while stopped.
	cName := fmt.Sprintf("nebla-pip-c-%d", stamp)
	cID := create(cName, true)
	c4 := getServer(cID).PublicNet.IPv4
	require.NotNil(t, c4, "C has no IPv4")
	own := c4.ID
	ownIP, _, _ := strings.Cut(c4.IP, "/")
	pips = append(pips, own)
	C := attach(cID, cName)
	cKey := hostKey(C)
	require.NotEqual(t, bKey, cKey)
	require.Len(t, runs(C), 1, "user-data runs on C's first boot")
	require.False(t, getServer(cID).Reruns)

	stop(B)
	pipAct(kept, "unassign", nil)
	waitUnassigned(kept)
	require.Nil(t, getServer(bID).PublicNet.IPv4, "B still has an IPv4")
	stop(C)
	pipAct(own, "unassign", nil)
	waitUnassigned(own)
	call(t, app, c, http.MethodDelete, fmt.Sprintf("/v1/primary_ips/%d", own), nil, http.StatusNoContent, nil)
	pipAct(kept, "assign", map[string]string{"server": strconv.FormatInt(cID, 10)})
	p := getPIP(kept)
	require.NotNil(t, p.Server)
	require.Equal(t, cID, p.Server.ID)
	c4 = getServer(cID).PublicNet.IPv4
	require.NotNil(t, c4)
	require.Equal(t, kept, c4.ID)

	// 4. C comes up on the kept IP without re-running user-data.
	start := time.Now()
	power(cID, "poweron", "running")
	C = attach(cID, cName)
	t.Logf("C back on serial %s after poweron", time.Since(start).Round(time.Second))
	out := C.con.mustExec("ip -o -4 addr show dev eth0 | cat", time.Minute)
	require.Contains(t, out, " "+ip+"/", "kept IP missing on C after the swap")
	require.NotContains(t, out, " "+ownIP+"/", "C kept its old IPv4")
	took := probeHost(t, P.con, ip, C.host, 2*time.Minute)
	require.Equal(t, cKey, hostKey(C), "C's host key changed across the swap")
	require.Equal(t, cKey, keyscan(t, P.con, ip, time.Minute), "host key on %s is not C's", ip)
	require.Len(t, runs(C), 1, "user-data re-ran on C after the IP change")
	t.Logf("SWAP: %s reached C from P %s after login", ip, took.Round(100*time.Millisecond))

	// 5. B starts with no public IPv4, and only C's ipset holds the address.
	power(bID, "poweron", "running")
	deadline := time.Now().Add(time.Minute)
	for ipsetHas(t, B, e, ip) || !ipsetHas(t, C, e, ip) {
		if time.Now().After(deadline) {
			t.Fatalf("%s: in B's ipset %v, in C's %v", ip, ipsetHas(t, B, e, ip), ipsetHas(t, C, e, ip))
		}
		time.Sleep(2 * time.Second)
	}
	probeHost(t, P.con, ip, C.host, time.Minute)
	require.Equal(t, cKey, keyscan(t, P.con, ip, time.Minute), "host key on %s is not C's after B started", ip)
	t.Logf("ISOLATION: B is up without %s; it still reaches C", ip)

	// 6. Delete everything and check the pool is back where it started.
	P.con.close()
	C.con.close()
	for _, sid := range []int64{P.id, bID, cID} {
		call(t, app, c, http.MethodDelete, fmt.Sprintf("/v1/servers/%d", sid), nil, http.StatusOK, &del)
		waitAction(t, app, c, del.Action.ID)
		require.Empty(t, taggedVMs(t, pc, sid))
	}
	waitUnassigned(kept)
	call(t, app, c, http.MethodDelete, fmt.Sprintf("/v1/primary_ips/%d", kept), nil, http.StatusNoContent, nil)
	require.Equal(t, startAlloc, e.allocated(t), "pool allocated count")
}

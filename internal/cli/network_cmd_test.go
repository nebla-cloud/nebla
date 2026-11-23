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

package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/nebla-cloud/nebla/internal/cli/apiclient"
	"github.com/nebla-cloud/nebla/internal/ent/server"
	"github.com/nebla-cloud/nebla/internal/ent/servernetwork"
	"github.com/nebla-cloud/nebla/internal/pve"
)

func newNetHarness(t *testing.T) *harness {
	t.Helper()
	h := newFIPHarness(t)
	e := h.Env
	e.App.PVE.AddSDN(pve.SDNZone{Zone: "nebla", Type: "vxlan"},
		pve.SDNVNet{VNet: "nb0001", Zone: "nebla"}, pve.SDNVNet{VNet: "nb0002", Zone: "nebla"}, pve.SDNVNet{VNet: "nb0003", Zone: "nebla"})
	if err := e.App.Ent.Location.UpdateOneID(e.Location).SetFirewallEnabled(true).Exec(context.Background()); err != nil {
		t.Fatal(err)
	}
	c := e.App.PVE.Conn()
	res := e.Admin.Send(http.MethodPut, "/v1/admin/locations/"+id64(e.Location), map[string]any{
		"name": "eu-1", "disk_storage": "ceph", "iso_storage": "local", "sdn_zone": "nebla",
		"pve": map[string]any{"url": c.BaseURL, "token_id": c.TokenID, "token_secret": c.Secret, "ca_fingerprint": c.CAFingerprint}})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("set zone: %s", e.App.Body(res))
	}
	res.Body.Close()
	return h
}

func TestNetworkCreateAndList(t *testing.T) {
	h := newNetHarness(t)
	out, errOut, code := h.run("network", "create", "--name", "backend", "--location", "eu-1", "--label", "env=prod")
	if code != 0 || !strings.Contains(out, "Network backend created (10.1.0.0/16)") {
		t.Fatalf("create: %d %q %q", code, out, errOut)
	}
	out, _, code = h.run("network", "list")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if code != 0 || len(lines) != 2 {
		t.Fatalf("list: %d %q", code, out)
	}
	if got := strings.Join(strings.Fields(lines[0]), " "); got != "ID NAME LOCATION RANGE SERVERS" {
		t.Fatalf("header %q", got)
	}
	if f := strings.Fields(lines[1]); f[1] != "backend" || f[2] != "eu-1" || f[3] != "10.1.0.0/16" || f[4] != "0" {
		t.Fatalf("row %q", lines[1])
	}
}

func TestNetworkUpdateDelete(t *testing.T) {
	h := newNetHarness(t)
	if _, errOut, code := h.run("network", "create", "--name", "backend", "--location", "eu-1"); code != 0 {
		t.Fatal(errOut)
	}
	if out, errOut, code := h.run("network", "update", "backend", "--name", "back2"); code != 0 || !strings.Contains(out, "Network back2 updated") {
		t.Fatalf("update: %d %q %q", code, out, errOut)
	}
	if out, errOut, code := h.run("network", "update", "back2"); code == 0 || !strings.Contains(out+errOut, "nothing to update") {
		t.Fatalf("empty update: %d %q %q", code, out, errOut)
	}
	if out, errOut, code := h.run("network", "delete", "back2"); code != 0 || !strings.Contains(out, "Network back2 deleted") {
		t.Fatalf("delete: %d %q %q", code, out, errOut)
	}
	if out, _, _ := h.run("network", "list"); strings.Contains(out, "back2") {
		t.Fatalf("still listed: %q", out)
	}
}

func TestServerAttachToNetworkPrintsCommands(t *testing.T) {
	h := newNetHarness(t)
	web := h.Env.RunningServer("web")
	if _, errOut, code := h.run("network", "create", "--name", "backend", "--location", "eu-1"); code != 0 {
		t.Fatal(errOut)
	}
	out, errOut, code := h.run("server", "attach-to-network", "web", "--network", "backend")
	if code != 0 || !strings.Contains(out, "Server web attached to network backend") || !strings.Contains(out, "Run this on web:\n") ||
		!strings.Contains(out, "ip addr add 10.1.0.2/16") || !strings.Contains(out, "To keep it after a reboot:\n") {
		t.Fatalf("attach: %d %q %q", code, out, errOut)
	}
	out, _, code = h.run("server", "describe", "web")
	if code != 0 || !strings.Contains(out, "Private networks:") || !strings.Contains(out, "backend (10.1.0.2)") {
		t.Fatalf("describe: %d %q", code, out)
	}
	h.Env.App.Ent.ServerNetwork.Update().Where(servernetwork.ServerID(web.ServerID)).SetState(servernetwork.StateAttached).ExecX(context.Background())
	out, errOut, code = h.run("server", "detach-from-network", "web", "--network", "backend")
	if code != 0 || !strings.Contains(out, "Server web detached from network backend") ||
		!strings.Contains(out, "Run this on web to remove the saved configuration:\n") {
		t.Fatalf("detach: %d %q %q", code, out, errOut)
	}
}

func TestServerAttachJSONNoProse(t *testing.T) {
	h := newNetHarness(t)
	h.Env.RunningServer("web")
	if _, errOut, code := h.run("network", "create", "--name", "backend", "--location", "eu-1"); code != 0 {
		t.Fatal(errOut)
	}
	out, errOut, code := h.run("-o", "json", "server", "attach-to-network", "web", "--network", "backend", "--ip", "10.1.0.9")
	var v apiclient.NetworkActionCreated
	if err := json.Unmarshal([]byte(out), &v); code != 0 || err != nil || v.GuestConfig == nil || deref(v.GuestConfig.Apply) == "" {
		t.Fatalf("json attach: %d %v %q %q", code, err, out, errOut)
	}
	if !strings.Contains(deref(v.GuestConfig.Apply), "10.1.0.9") || strings.Contains(out, "Run this on") {
		t.Fatalf("json attach content: %q", out)
	}
}

func TestServerCreateWithNetworks(t *testing.T) {
	h := newNetHarness(t)
	for _, n := range []string{"backend", "db"} {
		if _, errOut, code := h.run("network", "create", "--name", n, "--location", "eu-1"); code != 0 {
			t.Fatal(errOut)
		}
	}
	out, errOut, code := h.run("-o", "json", "server", "create", "--name", "app", "--type", "small", "--image", "ubuntu", "--location", "eu-1",
		"--network", "backend", "--network", "db=10.2.0.5")
	if code != 0 {
		t.Fatalf("create: %d %q %q", code, out, errOut)
	}
	var v struct {
		Server apiclient.Server `json:"server"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil || len(v.Server.PrivateNet) != 2 {
		t.Fatalf("private_net: %v %q", err, out)
	}
	if v.Server.PrivateNet[0].Name != "backend" || v.Server.PrivateNet[1].Name != "db" || v.Server.PrivateNet[1].Ip != "10.2.0.5" {
		t.Fatalf("nets %+v", v.Server.PrivateNet)
	}
}

func TestServerAttachStoppedPrintsNote(t *testing.T) {
	h := newNetHarness(t)
	web := h.Env.RunningServer("web")
	if err := h.Env.App.Ent.Server.UpdateOneID(web.ServerID).SetStatus(server.StatusOff).Exec(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, errOut, code := h.run("network", "create", "--name", "backend", "--location", "eu-1"); code != 0 {
		t.Fatal(errOut)
	}
	out, errOut, code := h.run("server", "attach-to-network", "web", "--network", "backend")
	if code != 0 || !strings.Contains(out, "Run these after starting the server.\nRun this on web after starting it:\n") {
		t.Fatalf("stopped attach: %d %q %q", code, out, errOut)
	}
}

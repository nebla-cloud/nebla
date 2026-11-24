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
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/nebla-cloud/nebla/internal/cli/apiclient"
	"github.com/nebla-cloud/nebla/internal/ent/server"
)

const oldServerWarning = "This server was created before IP changes were supported."

func stopServer(t *testing.T, h *harness, id int64) {
	t.Helper()
	if err := h.Env.App.Ent.Server.UpdateOneID(id).SetStatus(server.StatusOff).Exec(t.Context()); err != nil {
		t.Fatal(err)
	}
}

// settleRelease finishes the release the queue-only harness never runs.
func settleRelease(t *testing.T, h *harness, ref string) {
	t.Helper()
	if err := h.Env.App.Ent.IPAddress.UpdateOneID(primaryIPJSON(t, h, ref).Id).ClearReleasingFrom().Exec(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func primaryIPJSON(t *testing.T, h *harness, ref string) apiclient.PrimaryIp {
	t.Helper()
	out, errOut, code := h.run("-o", "json", "primary-ip", "describe", ref)
	var p apiclient.PrimaryIp
	if err := json.Unmarshal([]byte(out), &p); code != 0 || err != nil {
		t.Fatalf("describe %s: %d %v %q %q", ref, code, err, out, errOut)
	}
	return p
}

func TestPrimaryIPCommands(t *testing.T) {
	h := newFIPHarness(t)
	srv := h.Env.CreateServer("web", map[string]any{"public_net": map[string]any{"ipv4": false, "ipv6": false}})
	stopServer(t, h, srv.ServerID)

	out, errOut, code := h.run("primary-ip", "create", "--type", "ipv4", "--location", "eu-1", "--name", "keepme", "--keep", "--label", "env=prod")
	if code != 0 || !strings.Contains(out, "Primary IP keepme created (10.0.0.") {
		t.Fatalf("create: %d %q %q", code, out, errOut)
	}
	if p := primaryIPJSON(t, h, "keepme"); p.AutoDelete || p.Labels["env"] != "prod" {
		t.Fatalf("--keep should set auto_delete=false: %+v", p)
	}

	out, _, code = h.run("primary-ip", "list")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if code != 0 || len(lines) != 2 {
		t.Fatalf("list: %d %q", code, out)
	}
	if got := strings.Join(strings.Fields(lines[0]), " "); got != "ID NAME IP TYPE LOCATION SERVER AUTO-DELETE" {
		t.Fatalf("header %q", got)
	}
	if f := strings.Fields(lines[1]); f[1] != "keepme" || f[3] != "ipv4" || f[4] != "eu-1" || f[5] != "-" || f[6] != "false" {
		t.Fatalf("row %q", lines[1])
	}

	out, errOut, code = h.run("primary-ip", "describe", "keepme")
	if code != 0 || !strings.Contains(out, "Auto-delete:") || !strings.Contains(out, "keepme") {
		t.Fatalf("describe: %d %q %q", code, out, errOut)
	}

	out, errOut, code = h.run("primary-ip", "update", "keepme", "--auto-delete=true", "--description", "front")
	if code != 0 || !strings.Contains(out, "Primary IP keepme updated") {
		t.Fatalf("update: %d %q %q", code, out, errOut)
	}
	if p := primaryIPJSON(t, h, "keepme"); !p.AutoDelete || p.Description != "front" {
		t.Fatalf("update not applied: %+v", p)
	}
	if _, errOut, code = h.run("primary-ip", "update", "keepme", "--auto-delete=false"); code != 0 {
		t.Fatal(errOut)
	}
	if p := primaryIPJSON(t, h, "keepme"); p.AutoDelete {
		t.Fatalf("auto_delete should be false: %+v", p)
	}
	if _, errOut, code := h.run("primary-ip", "update", "keepme"); code == 0 || !strings.Contains(errOut, "nothing to update") {
		t.Fatalf("empty update: %d %q", code, errOut)
	}

	out, errOut, code = h.run("primary-ip", "assign", "keepme", "web")
	if code != 0 || !strings.Contains(out, "Primary IP keepme assigned to web") || !strings.Contains(out, "The new address takes effect when the server starts.") {
		t.Fatalf("assign: %d %q %q", code, out, errOut)
	}
	out, errOut, code = h.run("primary-ip", "unassign", "keepme")
	if code != 0 || !strings.Contains(out, "Primary IP keepme unassigned") || !strings.Contains(out, "The new address takes effect when the server starts.") {
		t.Fatalf("unassign: %d %q %q", code, out, errOut)
	}

	settleRelease(t, h, "keepme")
	out, errOut, code = h.run("primary-ip", "delete", "keepme")
	if code != 0 || !strings.Contains(out, "Primary IP keepme deleted") {
		t.Fatalf("delete: %d %q %q", code, out, errOut)
	}
	if _, _, code = h.run("primary-ip", "describe", "keepme"); code == 0 {
		t.Fatal("deleted IP still describes")
	}
}

func TestPrimaryIPWarningsStderrOnly(t *testing.T) {
	h := newFIPHarness(t)
	srv := h.Env.CreateServer("web", nil)
	stopServer(t, h, srv.ServerID)
	// Clearing net_refresh makes the server an old one.
	if err := h.Env.App.Ent.Server.UpdateOneID(srv.ServerID).SetNetRefresh(false).Exec(t.Context()); err != nil {
		t.Fatal(err)
	}
	out, errOut, code := h.run("primary-ip", "unassign", "web-ipv4")
	if code != 0 || strings.Contains(out, oldServerWarning) || !strings.Contains(errOut, oldServerWarning) {
		t.Fatalf("table: %d out=%q err=%q", code, out, errOut)
	}
	settleRelease(t, h, "web-ipv4")
	if _, errOut, code = h.run("primary-ip", "assign", "web-ipv4", "web"); code != 0 {
		t.Fatal(errOut)
	}
	out, errOut, code = h.run("-o", "json", "primary-ip", "unassign", "web-ipv4")
	var v struct {
		Warnings []string `json:"warnings"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil || code != 0 || len(v.Warnings) != 1 || strings.Contains(errOut, oldServerWarning) || strings.Contains(out, "takes effect") {
		t.Fatalf("json: %d %v out=%q err=%q", code, err, out, errOut)
	}
}

type recorder struct {
	next http.RoundTripper
	mu   sync.Mutex
	reqs []string
}

func (r *recorder) RoundTrip(req *http.Request) (*http.Response, error) {
	r.mu.Lock()
	r.reqs = append(r.reqs, req.Method+" "+req.URL.Path)
	r.mu.Unlock()
	return r.next.RoundTrip(req)
}

func TestServerDeleteKeepIPs(t *testing.T) {
	h := newFIPHarness(t)
	srv := h.Env.CreateServer("web", map[string]any{"public_net": map[string]any{"ipv4": true, "ipv6": true}})
	rec := &recorder{next: http.DefaultTransport}
	prev := http.DefaultClient.Transport
	http.DefaultClient.Transport = rec
	t.Cleanup(func() { http.DefaultClient.Transport = prev })

	if out, errOut, code := h.run("server", "delete", "web", "--keep-ips", "--keep-ip", "web-ipv4"); code == 0 || !strings.Contains(errOut+out, "not both") {
		t.Fatalf("both flags: %d %q %q", code, out, errOut)
	}
	if _, errOut, code := h.run("server", "delete", "web", "--keep-ip", "nope"); code == 0 || !strings.Contains(errOut, "not found") {
		t.Fatalf("unknown ip: %d %q", code, errOut)
	}
	if p := primaryIPJSON(t, h, "web-ipv4"); !p.AutoDelete {
		t.Fatal("nothing should change on a refused delete")
	}

	rec.mu.Lock()
	rec.reqs = nil
	rec.mu.Unlock()
	if _, errOut, code := h.run("server", "delete", "web", "--keep-ip", "web-ipv4"); code != 0 {
		t.Fatal(errOut)
	}
	rec.mu.Lock()
	var puts, del []int
	for i, r := range rec.reqs {
		if strings.HasPrefix(r, "PUT /v1/primary_ips/") {
			puts = append(puts, i)
		}
		if r == "DELETE /v1/servers/"+id64(srv.ServerID) {
			del = append(del, i)
		}
	}
	rec.mu.Unlock()
	if len(puts) != 1 || len(del) != 1 || puts[0] > del[0] {
		t.Fatalf("want one PUT before the DELETE, got puts=%v del=%v in %v", puts, del, rec.reqs)
	}
	if p := primaryIPJSON(t, h, "web-ipv4"); p.AutoDelete {
		t.Fatal("kept IP should have auto_delete=false")
	}
	if p := primaryIPJSON(t, h, "web-ipv6"); !p.AutoDelete {
		t.Fatal("other IP should keep auto_delete=true")
	}
}

func TestServerDeleteKeepAllIPs(t *testing.T) {
	h := newFIPHarness(t)
	h.Env.CreateServer("web", map[string]any{"public_net": map[string]any{"ipv4": true, "ipv6": true}})
	if _, errOut, code := h.run("server", "delete", "web", "--keep-ips"); code != 0 {
		t.Fatal(errOut)
	}
	for _, n := range []string{"web-ipv4", "web-ipv6"} {
		if p := primaryIPJSON(t, h, n); p.AutoDelete {
			t.Fatalf("%s should be kept", n)
		}
	}
}

func TestServerCreateIPv4Ref(t *testing.T) {
	h := newFIPHarness(t)
	if _, errOut, code := h.run("primary-ip", "create", "--type", "ipv4", "--location", "eu-1", "--name", "spare"); code != 0 {
		t.Fatal(errOut)
	}
	spare := primaryIPJSON(t, h, "spare")
	out, errOut, code := h.run("server", "create", "--name", "web", "--type", "small", "--image", "ubuntu", "--location", "eu-1", "--ipv4", "spare", "--ipv6", "true")
	if code != 0 || !strings.Contains(out, "IPv4: "+spare.Ip) {
		t.Fatalf("create: %d %q %q", code, out, errOut)
	}
	if p := primaryIPJSON(t, h, "spare"); p.Server == nil || p.Server.Name != "web" {
		t.Fatalf("spare not assigned: %+v", p)
	}
	if out, errOut, code := h.run("server", "describe", "web"); code != 0 || !strings.Contains(out, spare.Ip+" (spare)") || !strings.Contains(out, "(web-ipv6)") {
		t.Fatalf("describe names: %d %q %q", code, out, errOut)
	}
	if out, errOut, code := h.run("server", "create", "--name", "w2", "--type", "small", "--image", "ubuntu", "--location", "eu-1", "--ipv4", "spare", "--without-ipv4"); code == 0 || !strings.Contains(out+errOut, "--without-ipv4") {
		t.Fatalf("conflict: %d %q %q", code, out, errOut)
	}
	// The boolean forms still work.
	if out, errOut, code := h.run("server", "create", "--name", "w3", "--type", "small", "--image", "ubuntu", "--location", "eu-1", "--ipv4=false", "--ipv6=false"); code != 0 || strings.Contains(out, "IPv4: 10") {
		t.Fatalf("bool forms: %d %q %q", code, out, errOut)
	}
}

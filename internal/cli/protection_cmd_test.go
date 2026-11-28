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
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// bodyRecorder keeps the JSON body of each write request, keyed by "METHOD path".
type bodyRecorder struct {
	next   http.RoundTripper
	mu     sync.Mutex
	bodies map[string]map[string]any
}

func (r *bodyRecorder) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Body != nil && (req.Method == http.MethodPut || req.Method == http.MethodPost) {
		b, _ := io.ReadAll(req.Body)
		req.Body = io.NopCloser(bytes.NewReader(b))
		var m map[string]any
		if json.Unmarshal(b, &m) == nil {
			r.mu.Lock()
			r.bodies[req.Method+" "+req.URL.Path] = m
			r.mu.Unlock()
		}
	}
	return r.next.RoundTrip(req)
}

func recordBodies(t *testing.T) *bodyRecorder {
	t.Helper()
	rec := &bodyRecorder{next: http.DefaultTransport, bodies: map[string]map[string]any{}}
	prev := http.DefaultClient.Transport
	http.DefaultClient.Transport = rec
	t.Cleanup(func() { http.DefaultClient.Transport = prev })
	return rec
}

// only returns the one recorded body for method and a path prefix.
func (r *bodyRecorder) only(t *testing.T, method, prefix string) map[string]any {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	var got map[string]any
	n := 0
	for k, v := range r.bodies {
		if strings.HasPrefix(k, method+" "+prefix) {
			got, n = v, n+1
		}
	}
	if n != 1 {
		t.Fatalf("want one %s %s, got %d in %v", method, prefix, n, r.bodies)
	}
	return got
}

func TestProtectionLine(t *testing.T) {
	yes, no := true, false
	for _, c := range []struct {
		del  bool
		reb  *bool
		want string
	}{{false, nil, "none"}, {true, nil, "delete"}, {false, &yes, "rebuild"}, {true, &yes, "delete, rebuild"}, {false, &no, "none"}} {
		if got := protectionLine(c.del, c.reb); got != c.want {
			t.Errorf("protectionLine(%v, %v) = %q, want %q", c.del, c.reb, got, c.want)
		}
	}
}

func TestVolumeUpdate_ProtectDeleteFalse(t *testing.T) {
	h := newVolumeHarness(t)
	if _, errOut, code := h.run("volume", "create", "--name", "data", "--size", "20", "--location", "eu-1", "--protect-delete"); code != 0 {
		t.Fatal(errOut)
	}
	if out, _, _ := h.run("volume", "describe", "data"); !strings.Contains(out, "Protection:") || !strings.Contains(out, "delete") {
		t.Fatalf("create did not protect: %q", out)
	}
	rec := recordBodies(t)
	if out, errOut, code := h.run("volume", "update", "data", "--protect-delete=false"); code != 0 {
		t.Fatalf("update: %q %q", out, errOut)
	}
	got, _ := json.Marshal(rec.only(t, "PUT", "/v1/volumes/"))
	if string(got) != `{"protection":{"delete":false}}` {
		t.Fatalf("body %s", got)
	}
	if out, _, _ := h.run("volume", "describe", "data"); !strings.Contains(out, "Protection:") || !strings.Contains(out, "none") {
		t.Fatalf("describe: %q", out)
	}
}

func TestVolumeUpdate_NoProtectFlag(t *testing.T) {
	h := newVolumeHarness(t)
	if _, errOut, code := h.run("volume", "create", "--name", "data", "--size", "20", "--location", "eu-1"); code != 0 {
		t.Fatal(errOut)
	}
	rec := recordBodies(t)
	if _, errOut, code := h.run("volume", "update", "data", "--name", "data2"); code != 0 {
		t.Fatal(errOut)
	}
	if _, has := rec.only(t, "PUT", "/v1/volumes/")["protection"]; has {
		t.Fatal("request carried protection")
	}
	if _, errOut, code := h.run("volume", "update", "data2"); code == 0 || !strings.Contains(errOut, "nothing to update") {
		t.Fatalf("no flags: %d %q", code, errOut)
	}
}

func TestVolumeDescribe_Protection(t *testing.T) {
	h := newVolumeHarness(t)
	if _, errOut, code := h.run("volume", "create", "--name", "data", "--size", "20", "--location", "eu-1", "--protect-delete"); code != 0 {
		t.Fatal(errOut)
	}
	if out, _, _ := h.run("volume", "describe", "data"); !strings.Contains(out, "Protection:    delete") {
		t.Fatalf("describe: %q", out)
	}
}

func TestServerUpdate_All(t *testing.T) {
	h := newHarness(t)
	if _, e, code := h.run(createArgs("web1")...); code != 0 {
		t.Fatal(e)
	}
	rec := recordBodies(t)
	out, errOut, code := h.run("server", "update", "web1", "--name", "web2", "--label", "env=prod", "--protect-delete", "--protect-rebuild")
	if code != 0 || !strings.Contains(out, "Server web2 updated") {
		t.Fatalf("update: %d %q %q", code, out, errOut)
	}
	got, _ := json.Marshal(rec.only(t, "PUT", "/v1/servers/"))
	if string(got) != `{"labels":{"env":"prod"},"name":"web2","protection":{"delete":true,"rebuild":true}}` {
		t.Fatalf("body %s", got)
	}
	if out, _, _ := h.run("server", "describe", "web2"); !strings.Contains(out, "Protection:        delete, rebuild") {
		t.Fatalf("describe: %q", out)
	}
	rec.mu.Lock()
	rec.bodies = map[string]map[string]any{}
	rec.mu.Unlock()
	if _, errOut, code := h.run("server", "update", "web2", "--protect-rebuild=false"); code != 0 {
		t.Fatal(errOut)
	}
	got, _ = json.Marshal(rec.only(t, "PUT", "/v1/servers/"))
	if string(got) != `{"protection":{"rebuild":false}}` {
		t.Fatalf("partial body %s", got)
	}
	if out, _, _ := h.run("server", "describe", "web2"); !strings.Contains(out, "Protection:        delete\n") {
		t.Fatalf("describe: %q", out)
	}
	if _, errOut, code := h.run("server", "update", "web2"); code == 0 || !strings.Contains(errOut, "nothing to update") {
		t.Fatalf("no flags: %d %q", code, errOut)
	}
}

func TestServerCreate_ProtectFlags(t *testing.T) {
	h := newHarness(t)
	rec := recordBodies(t)
	if _, e, code := h.run(createArgs("web1", "--protect-delete", "--protect-rebuild=false")...); code != 0 {
		t.Fatal(e)
	}
	got, _ := json.Marshal(rec.only(t, "POST", "/v1/servers")["protection"])
	if string(got) != `{"delete":true,"rebuild":false}` {
		t.Fatalf("protection %s", got)
	}
	if out, _, _ := h.run("server", "describe", "web1"); !strings.Contains(out, "Protection:        delete\n") {
		t.Fatalf("describe: %q", out)
	}
}

func TestServerDelete_ProtectedIPKept(t *testing.T) {
	h := newFIPHarness(t)
	h.Env.CreateServer("web", map[string]any{"public_net": map[string]any{"ipv4": true, "ipv6": true}})
	if _, errOut, code := h.run("primary-ip", "update", "web-ipv4", "--protect-delete"); code != 0 {
		t.Fatal(errOut)
	}
	rec := &recorder{next: http.DefaultTransport}
	prev := http.DefaultClient.Transport
	http.DefaultClient.Transport = rec
	t.Cleanup(func() { http.DefaultClient.Transport = prev })

	out, errOut, code := h.run("server", "delete", "web", "--keep-ip", "web-ipv4")
	if code != 0 {
		t.Fatal(errOut)
	}
	rec.mu.Lock()
	for _, r := range rec.reqs {
		if strings.HasPrefix(r, "PUT /v1/primary_ips/") {
			t.Fatalf("protected IP's auto_delete was flipped: %v", rec.reqs)
		}
	}
	rec.mu.Unlock()
	if p := primaryIPJSON(t, h, "web-ipv4"); !p.AutoDelete || !p.Protection.Delete {
		t.Fatalf("want auto_delete still true: %+v", p)
	}
	if !strings.Contains(out, "Kept web-ipv4") {
		t.Fatalf("summary: %q", out)
	}

	// Without any keep flag the protected IP is still listed as kept.
	h.Env.CreateServer("web2", map[string]any{"public_net": map[string]any{"ipv4": true, "ipv6": false}})
	if _, errOut, code := h.run("primary-ip", "update", "web2-ipv4", "--protect-delete"); code != 0 {
		t.Fatal(errOut)
	}
	out, errOut, code = h.run("server", "delete", "web2")
	if code != 0 || !strings.Contains(out, "Kept web2-ipv4") || !strings.Contains(errOut, "stay in the project after the delete: web2-ipv4") {
		t.Fatalf("implicit: %d %q %q", code, out, errOut)
	}
}

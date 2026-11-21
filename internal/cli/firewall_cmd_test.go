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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nebla-cloud/nebla/internal/cli/apiclient"
)

const inRule = `[{"direction":"in","protocol":"tcp","port":"22","source_ips":["0.0.0.0/0"],"description":"ssh"}]`

func writeRules(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "rules.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestFirewallLifecycle(t *testing.T) {
	h := newHarness(t)
	ok := func(args ...string) string {
		t.Helper()
		out, errOut, code := h.run(args...)
		if code != 0 {
			t.Fatalf("%v: %d %q %q", args, code, out, errOut)
		}
		return out
	}
	ok("firewall", "create", "--name", "web", "--label", "env=prod", "--rules-file", writeRules(t, inRule))
	out := ok("firewall", "describe", "web")
	if !strings.Contains(out, "Inbound") || !strings.Contains(out, "ssh") || strings.Contains(out, "Outbound") {
		t.Fatalf("describe: %q", out)
	}
	ok("firewall", "add-rule", "web", "--direction", "out", "--protocol", "tcp", "--port", "443", "--destination-ips", "0.0.0.0/0")
	out = ok("firewall", "describe", "web")
	if !strings.Contains(out, "Outbound") || !strings.Contains(out, "443") {
		t.Fatalf("describe 2: %q", out)
	}
	if out = ok("firewall", "list"); !strings.Contains(out, "web") || !strings.Contains(out, "APPLIED TO") {
		t.Fatalf("list: %q", out)
	}
	ok(createArgs("web1", "--label", "role=web")...)
	ok("firewall", "apply-to-resource", "web", "--type", "label_selector", "--label-selector", "role=web")
	if out = ok("server", "describe", "web1"); !strings.Contains(out, "web (via role=web)") {
		t.Fatalf("server describe: %q", out)
	}
	if out = ok("firewall", "delete-rule", "web", "--index", "1", "--protocol", "tcp", "--port", "22"); !strings.Contains(out, "inbound tcp 22 from 0.0.0.0/0") {
		t.Fatalf("delete-rule: %q", out)
	}
	if out = ok("firewall", "describe", "web"); strings.Contains(out, "ssh") || !strings.Contains(out, "443") {
		t.Fatalf("after delete-rule: %q", out)
	}
	ok("firewall", "remove-from-resource", "web", "--type", "label_selector", "--label-selector", "role=web")
	if out = ok("server", "describe", "web1"); strings.Contains(out, "via role=web") {
		t.Fatalf("server describe after remove: %q", out)
	}
	ok("firewall", "update", "web", "--name", "web2")
	if _, e, code := h.runEnv(nil, inRule, "firewall", "replace-rules", "web2", "--rules-file", "-"); code != 0 {
		t.Fatal(e)
	}
	ok("firewall", "delete", "web2")
	if out = ok("firewall", "list", "--no-header"); strings.TrimSpace(out) != "" {
		t.Fatalf("list after delete: %q", out)
	}
}

func TestFirewallServerCreateFlag(t *testing.T) {
	h := newHarness(t)
	if _, e, code := h.run("firewall", "create", "--name", "web"); code != 0 {
		t.Fatal(e)
	}
	if _, e, code := h.run(createArgs("web1", "--firewall", "web")...); code != 0 {
		t.Fatal(e)
	}
	out, _, _ := h.run("server", "describe", "web1")
	if !strings.Contains(out, "web (direct)") {
		t.Fatalf("describe: %q", out)
	}
}

func TestFirewallJSONOutput(t *testing.T) {
	h := newHarness(t)
	out, e, code := h.run("-o", "json", "firewall", "create", "--name", "web", "--rules-file", writeRules(t, inRule))
	if code != 0 {
		t.Fatal(e)
	}
	var fw apiclient.Firewall
	if err := json.Unmarshal([]byte(out), &fw); err != nil || fw.Name != "web" || len(fw.Rules) != 1 {
		t.Fatalf("json: %v %q", err, out)
	}
	out, _, code = h.run("-o", "json", "firewall", "describe", "web")
	if code != 0 || json.Unmarshal([]byte(out), &fw) != nil || fw.Name != "web" {
		t.Fatalf("describe json: %d %q", code, out)
	}
}

func TestFirewallBadRuleExitCode(t *testing.T) {
	h := newHarness(t)
	if _, e, code := h.run("firewall", "create", "--name", "web"); code != 0 {
		t.Fatal(e)
	}
	_, errOut, code := h.run("firewall", "add-rule", "web", "--direction", "in", "--protocol", "tcp", "--port", "70000", "--source-ips", "0.0.0.0/0")
	if code == 0 || !strings.Contains(strings.ToLower(errOut), "port") {
		t.Fatalf("bad rule: %d %q", code, errOut)
	}
}

func TestFirewallDeleteRuleGuards(t *testing.T) {
	h := newHarness(t)
	if _, e, code := h.run("firewall", "create", "--name", "web", "--rules-file", writeRules(t, inRule)); code != 0 {
		t.Fatal(e)
	}
	if _, e, code := h.run("firewall", "delete-rule", "web", "--index", "5"); code == 0 || !strings.Contains(e, "no rule 5") {
		t.Fatalf("range: %d %q", code, e)
	}
	_, e, code := h.run("firewall", "delete-rule", "web", "--index", "1", "--port", "80")
	if code == 0 || !strings.Contains(e, "nothing changed") {
		t.Fatalf("mismatch: %d %q", code, e)
	}
	if out, _, _ := h.run("firewall", "describe", "web"); !strings.Contains(out, "ssh") {
		t.Fatalf("rules changed: %q", out)
	}
}

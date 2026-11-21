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
	ok("firewall", "delete-rule", "web", "--index", "1")
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

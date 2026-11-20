package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Hexeption/nebla/internal/cli/apiclient"
)

func runningServer(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t)
	if _, errOut, code := h.run(createArgs("web1")...); code != 0 {
		t.Fatalf("create: %d %q", code, errOut)
	}
	return h
}

func statusOf(t *testing.T, h *harness) string {
	t.Helper()
	out, _, code := h.run("server", "describe", "web1")
	if code != 0 {
		t.Fatalf("describe: %d", code)
	}
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "Status:") {
			return strings.TrimSpace(strings.TrimPrefix(l, "Status:"))
		}
	}
	t.Fatalf("no status in %q", out)
	return ""
}

func TestServerPowerCycle(t *testing.T) {
	h := runningServer(t)
	out, errOut, code := h.run("server", "poweroff", "web1")
	if code != 0 || strings.TrimSpace(out) != "Server web1 powered off" {
		t.Fatalf("poweroff: %d %q %q", code, out, errOut)
	}
	if s := statusOf(t, h); s != "off" {
		t.Fatalf("status %q", s)
	}
	out, errOut, code = h.run("server", "poweron", "web1")
	if code != 0 || strings.TrimSpace(out) != "Server web1 powered on" {
		t.Fatalf("poweron: %d %q %q", code, out, errOut)
	}
	if s := statusOf(t, h); s != "running" {
		t.Fatalf("status %q", s)
	}
	for cmd, msg := range map[string]string{"reboot": "rebooted", "reset": "reset", "shutdown": "shut down"} {
		out, errOut, code = h.run("server", cmd, "web1")
		if code != 0 || strings.TrimSpace(out) != "Server web1 "+msg {
			t.Fatalf("%s: %d %q %q", cmd, code, out, errOut)
		}
		if cmd == "shutdown" {
			h.run("server", "poweron", "web1")
		}
	}
}

func TestServerRebuild(t *testing.T) {
	h := runningServer(t)
	out, errOut, code := h.run("server", "rebuild", "web1", "--image", "ubuntu")
	if code != 0 || strings.TrimSpace(out) != "Server web1 rebuilt from ubuntu" {
		t.Fatalf("rebuild: %d %q %q", code, out, errOut)
	}
}

func TestChangeTypeNeedsStopped(t *testing.T) {
	h := runningServer(t)
	h.Env.Admin.CreateServerType("medium", 4, 4096, 40)
	out, errOut, code := h.run("server", "change-type", "web1", "--type", "medium")
	if code != 1 || out != "" || !strings.HasPrefix(errOut, "error: ") {
		t.Fatalf("running: %d %q %q", code, out, errOut)
	}
	if _, errOut, code = h.run("server", "poweroff", "web1"); code != 0 {
		t.Fatalf("poweroff: %d %q", code, errOut)
	}
	out, errOut, code = h.run("server", "change-type", "web1", "--type", "medium", "--upgrade-disk")
	if code != 0 || strings.TrimSpace(out) != "Server web1 changed to medium" {
		t.Fatalf("change-type: %d %q %q", code, out, errOut)
	}
	out, _, _ = h.run("server", "describe", "web1")
	if !strings.Contains(out, "medium") {
		t.Fatalf("describe: %q", out)
	}
}

func TestActionCommandNoWait(t *testing.T) {
	h := runningServer(t)
	out, errOut, code := h.run("-o", "json", "server", "poweroff", "web1", "--no-wait")
	if code != 0 {
		t.Fatalf("poweroff: %d %q", code, errOut)
	}
	var a apiclient.Action
	if err := json.Unmarshal([]byte(out), &a); err != nil {
		t.Fatalf("not an action: %v %q", err, out)
	}
	if a.Status != apiclient.ActionStatusRunning && a.Status != apiclient.ActionStatusSuccess {
		t.Fatalf("status %q", a.Status)
	}
}

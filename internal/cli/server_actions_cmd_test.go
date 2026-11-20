package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Hexeption/nebla/internal/cli/apiclient"
	"github.com/Hexeption/nebla/internal/testutil"
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

func actionJSON(t *testing.T, h *harness, args ...string) apiclient.Action {
	t.Helper()
	out, errOut, code := h.run(append([]string{"-o", "json", "server"}, args...)...)
	if code != 0 {
		t.Fatalf("%v: %d %q %q", args, code, out, errOut)
	}
	var a apiclient.Action
	if err := json.Unmarshal([]byte(out), &a); err != nil {
		t.Fatalf("%v: not an action: %v %q", args, err, out)
	}
	return a
}

func TestServerPowerCycle(t *testing.T) {
	h := runningServer(t)
	steps := []struct{ cmd, command, msg, status string }{
		{"poweroff", "stop_server", "powered off", "off"},
		{"poweron", "start_server", "powered on", "running"},
		{"reboot", "reboot_server", "rebooted", "running"},
		{"reset", "reset_server", "reset", "running"},
		{"shutdown", "shutdown_server", "shut down", "off"},
	}
	for _, s := range steps {
		if a := actionJSON(t, h, s.cmd, "web1"); a.Command != s.command || a.Status != apiclient.ActionStatusSuccess {
			t.Fatalf("%s: command %q status %q", s.cmd, a.Command, a.Status)
		}
		if got := statusOf(t, h); got != s.status {
			t.Fatalf("after %s: status %q, want %q", s.cmd, got, s.status)
		}
	}
	for _, s := range []struct{ cmd, msg string }{{"poweron", "powered on"}, {"shutdown", "shut down"}} {
		out, errOut, code := h.run("server", s.cmd, "web1")
		if code != 0 || strings.TrimSpace(out) != "Server web1 "+s.msg {
			t.Fatalf("%s: %d %q %q", s.cmd, code, out, errOut)
		}
	}
}

func TestServerRebuild(t *testing.T) {
	h := runningServer(t)
	h.Env.Admin.CreateImage("debian", h.Env.Location, testutil.TemplateVMID)
	out, errOut, code := h.run("server", "rebuild", "web1", "--image", "debian")
	if code != 0 || strings.TrimSpace(out) != "Server web1 rebuilt from debian" {
		t.Fatalf("rebuild: %d %q %q", code, out, errOut)
	}
	out, _, _ = h.run("server", "describe", "web1")
	if !strings.Contains(out, "Image:") || !strings.Contains(out, "debian") {
		t.Fatalf("describe after rebuild: %q", out)
	}
	if a := actionJSON(t, h, "rebuild", "web1", "--image", "ubuntu"); a.Command != "rebuild_server" {
		t.Fatalf("command %q", a.Command)
	}
}

func TestChangeTypeNeedsStopped(t *testing.T) {
	h := runningServer(t)
	h.Env.Admin.CreateServerType("medium", 4, 4096, 40)
	out, errOut, code := h.run("server", "change-type", "web1", "--type", "medium")
	if code != 1 || out != "" || !strings.HasPrefix(errOut, "error: ") {
		t.Fatalf("running: %d %q %q", code, out, errOut)
	}
	if !strings.Contains(errOut, "the server must be stopped") {
		t.Fatalf("409 message not shown: %q", errOut)
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

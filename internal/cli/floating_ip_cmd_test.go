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
	"strings"
	"testing"
	"time"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/cli/apiclient"
	"github.com/nebla-cloud/nebla/internal/ent/action"
)

// newFIPHarness succeeds every running action in the background.
func newFIPHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarnessQueueOnly(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	t.Cleanup(func() { cancel(); <-done })
	client := h.Env.App.Ent
	go func() {
		defer close(done)
		for ctx.Err() == nil {
			ids, _ := client.Action.Query().Where(action.StatusEQ(action.StatusRunning)).IDs(ctx)
			for _, id := range ids {
				_ = actions.Succeed(ctx, client, id)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()
	return h
}

func TestFloatingIPCreateInLocation(t *testing.T) {
	h := newFIPHarness(t)
	out, errOut, code := h.run("floating-ip", "create", "--type", "ipv4", "--location", "eu-1", "--name", "vip", "--label", "env=prod")
	if code != 0 || !strings.Contains(out, "Floating IP vip created") {
		t.Fatalf("create: %d %q %q", code, out, errOut)
	}
	out, _, code = h.run("floating-ip", "list")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if code != 0 || len(lines) != 2 {
		t.Fatalf("list: %d %q", code, out)
	}
	if got := strings.Join(strings.Fields(lines[0]), " "); got != "ID NAME IP LOCATION SERVER STATUS" {
		t.Fatalf("header %q", got)
	}
	if f := strings.Fields(lines[1]); f[1] != "vip" || f[3] != "eu-1" || f[4] != "-" || f[5] != "active" {
		t.Fatalf("row %q", lines[1])
	}
	if out, errOut, code := h.run("floating-ip", "create", "--type", "ipv4"); code == 0 || !strings.Contains(errOut+out, "exactly one") {
		t.Fatalf("no target: %d %q %q", code, out, errOut)
	}
}

func TestFloatingIPAssignPrintsCommands(t *testing.T) {
	h := newFIPHarness(t)
	h.Env.RunningServer("web")
	if _, errOut, code := h.run("floating-ip", "create", "--type", "ipv4", "--location", "eu-1", "--name", "vip"); code != 0 {
		t.Fatal(errOut)
	}
	out, errOut, code := h.run("floating-ip", "assign", "vip", "web")
	if code != 0 || !strings.Contains(out, "Floating IP vip assigned to web") || !strings.Contains(out, "Run this on web:\n") ||
		!strings.Contains(out, "To keep it after a reboot:\n") || strings.Contains(out, "to stop using the address") {
		t.Fatalf("assign: %d %q %q", code, out, errOut)
	}
	jout, _, code := h.run("-o", "json", "floating-ip", "describe", "vip")
	var f apiclient.FloatingIp
	if err := json.Unmarshal([]byte(jout), &f); err != nil || code != 0 || f.GuestConfig == nil || deref(f.GuestConfig.Apply) == "" {
		t.Fatalf("describe json: %d %v %q", code, err, jout)
	}
	if !strings.Contains(out, strings.TrimSpace(deref(f.GuestConfig.Apply))) {
		t.Fatalf("apply text missing: %q", out)
	}
}

func TestFloatingIPJSONAssignNoProse(t *testing.T) {
	h := newFIPHarness(t)
	h.Env.RunningServer("web")
	if _, errOut, code := h.run("floating-ip", "create", "--type", "ipv4", "--location", "eu-1", "--name", "vip"); code != 0 {
		t.Fatal(errOut)
	}
	out, errOut, code := h.run("-o", "json", "floating-ip", "assign", "vip", "web")
	var f apiclient.FloatingIp
	if err := json.Unmarshal([]byte(out), &f); code != 0 || err != nil || f.GuestConfig == nil || deref(f.GuestConfig.Apply) == "" {
		t.Fatalf("json assign: %d %v %q %q", code, err, out, errOut)
	}
	if strings.Contains(out, "Run this on") {
		t.Fatalf("prose in json: %q", out)
	}
}

func TestFloatingIPCreateWithServerPrintsCommands(t *testing.T) {
	h := newFIPHarness(t)
	h.Env.RunningServer("web")
	out, errOut, code := h.run("floating-ip", "create", "--type", "ipv4", "--server", "web")
	if code != 0 || !strings.Contains(out, "Run this on web:\n") {
		t.Fatalf("create --server: %d %q %q", code, out, errOut)
	}
}

func TestFloatingIPDescribeByAddress(t *testing.T) {
	h := newFIPHarness(t)
	if _, errOut, code := h.run("floating-ip", "create", "--type", "ipv4", "--location", "eu-1", "--name", "vip"); code != 0 {
		t.Fatal(errOut)
	}
	jout, _, _ := h.run("-o", "json", "floating-ip", "describe", "vip")
	var f apiclient.FloatingIp
	if err := json.Unmarshal([]byte(jout), &f); err != nil || f.Ip == "" {
		t.Fatalf("%v %q", err, jout)
	}
	out, errOut, code := h.run("floating-ip", "describe", f.Ip)
	if code != 0 || !strings.Contains(out, "Name:") || !strings.Contains(out, "vip") {
		t.Fatalf("describe by address: %d %q %q", code, out, errOut)
	}
}

func TestFloatingIPUpdateUnassignDelete(t *testing.T) {
	h := newFIPHarness(t)
	h.Env.RunningServer("web")
	if _, errOut, code := h.run("floating-ip", "create", "--type", "ipv4", "--location", "eu-1", "--name", "vip"); code != 0 {
		t.Fatal(errOut)
	}
	if out, errOut, code := h.run("floating-ip", "update", "vip", "--name", "vip2", "--description", "front"); code != 0 || !strings.Contains(out, "Floating IP vip2 updated") {
		t.Fatalf("update: %d %q %q", code, out, errOut)
	}
	if _, errOut, code := h.run("floating-ip", "assign", "vip2", "web"); code != 0 {
		t.Fatal(errOut)
	}
	if out, errOut, code := h.run("floating-ip", "unassign", "vip2"); code != 0 || !strings.Contains(out, "Floating IP vip2 unassigned") {
		t.Fatalf("unassign: %d %q %q", code, out, errOut)
	}
	out, errOut, code := h.run("floating-ip", "delete", "vip2")
	if code != 0 || !strings.Contains(out, "Floating IP vip2 deleted") || !strings.Contains(errOut, "Waiting for") {
		t.Fatalf("delete: %d %q %q", code, out, errOut)
	}
}

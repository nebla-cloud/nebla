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
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/nebla-cloud/nebla/internal/ent/server"
	"github.com/nebla-cloud/nebla/internal/ent/snapshot"
)

const cliSnapVMID = 9200

// addCLISnapshot inserts an available snapshot of serverID on shared storage.
func addCLISnapshot(t *testing.T, h *harness, name string, serverID int64, fingerprint string) int64 {
	t.Helper()
	h.Env.App.PVE.AddTemplate("pve1", cliSnapVMID, "ceph")
	return h.Env.App.Ent.Snapshot.Create().SetProjectID(h.Env.Project).SetLocationID(h.Env.Location).SetName(name).
		SetLabels(map[string]string{}).SetSizeGB(10).SetVmid(cliSnapVMID).SetNode("").SetStatus(snapshot.StatusAvailable).
		SetImageID(h.Env.Image).SetOsFlavor("ubuntu").SetOsVersion("24.04").SetServerID(serverID).SetServerName("web1").
		SetNetworkFingerprint(fingerprint).SaveX(context.Background()).ID
}

// bootServer creates a running server and gives its VM a boot disk a snapshot job can copy.
func bootServer(t *testing.T, h *harness, name string) {
	t.Helper()
	ctx := context.Background()
	if _, errOut, code := h.run(createArgs(name)...); code != 0 {
		t.Fatalf("create: %d %q", code, errOut)
	}
	vmid := *h.Env.App.Ent.Server.Query().Where(server.Name(name)).OnlyX(ctx).Vmid
	pc, err := h.Env.App.Catalog.ProxmoxFor(ctx, h.Env.Location)
	if err != nil {
		t.Fatal(err)
	}
	boot, err := pc.AllocVolume(ctx, "pve1", "local-lvm", vmid, fmt.Sprintf("vm-%d-disk-0", vmid), 10)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range map[string]string{
		"scsi0":  boot + ",discard=on,size=10G",
		"net0":   "virtio=BC:24:11:00:00:01,bridge=vmbr0,firewall=1,tag=10",
		"boot":   "order=scsi0;net0",
		"scsihw": "virtio-scsi-single",
		"ostype": "l26",
		"agent":  "1",
	} {
		h.Env.App.PVE.SetVMConfig(vmid, k, v)
	}
}

func TestSnapshotCreateWaits(t *testing.T) {
	h := newHarness(t)
	bootServer(t, h, "web1")
	out, errOut, code := h.run("snapshot", "create", "web1", "--name", "before", "--force-stop")
	if code != 0 || !strings.Contains(out, "Snapshot before created") {
		t.Fatalf("create: %d %q %q", code, out, errOut)
	}
	if !strings.Contains(errOut, "web1 is running. It will be shut down, saved and started again.") {
		t.Fatalf("no running warning: %q", errOut)
	}
	out, _, code = h.run("snapshot", "list")
	if code != 0 || !strings.Contains(out, "before") || !strings.Contains(out, "available") {
		t.Fatalf("list: %d %q", code, out)
	}
}

func TestSnapshotCreatePromptRunning(t *testing.T) {
	h := newHarnessQueueOnly(t)
	h.Env.RunningServer("web1")
	out, errOut, code := h.run("snapshot", "create", "web1", "--no-wait")
	if code != 0 || !strings.Contains(out, "is being created (action ") {
		t.Fatalf("create: %d %q %q", code, out, errOut)
	}
	if strings.Contains(errOut, "[y/N]") || strings.Contains(out, "[y/N]") {
		t.Fatalf("prompted without a TTY: %q %q", out, errOut)
	}
}

// runTTY runs the CLI with stdin as a terminal holding input.
func (h *harness) runTTY(t *testing.T, input string, args ...string) (string, string, int) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	if _, err := w.WriteString(input); err != nil {
		t.Fatal(err)
	}
	w.Close()
	var stdout, stderr bytes.Buffer
	e := Env{Stdin: r, Stdout: &stdout, Stderr: &stderr, Getenv: func(string) string { return "" }, ConfigDir: h.dir,
		HTTPClient: http.DefaultClient, IsTerminal: func(int) bool { return true }}
	code := Run(context.Background(), append([]string{"--config", h.configFile()}, args...), e)
	return stdout.String(), stderr.String(), code
}

func TestSnapshotCreateRunningWarning(t *testing.T) {
	const warn = "web1 is running. It will be shut down, saved and started again."
	t.Run("a terminal asks", func(t *testing.T) {
		h := newHarnessQueueOnly(t)
		h.Env.RunningServer("web1")
		_, errOut, code := h.runTTY(t, "n\n", "snapshot", "create", "web1", "--no-wait")
		if code != 1 || !strings.Contains(errOut, warn) || !strings.Contains(errOut, "Continue? [y/N]") {
			t.Fatalf("create: %d %q", code, errOut)
		}
	})
	t.Run("--yes warns without asking", func(t *testing.T) {
		h := newHarnessQueueOnly(t)
		h.Env.RunningServer("web1")
		out, errOut, code := h.runTTY(t, "", "snapshot", "create", "web1", "--no-wait", "--yes")
		if code != 0 || !strings.Contains(out, "is being created") || !strings.Contains(errOut, warn) || strings.Contains(errOut, "[y/N]") {
			t.Fatalf("create: %d %q %q", code, out, errOut)
		}
	})
}

func TestServerCreateFromSnapshotWithoutLocation(t *testing.T) {
	ctx := context.Background()
	h := newHarnessQueueOnly(t)
	web := h.Env.RunningServer("web1")
	addCLISnapshot(t, h, "before", web.ServerID, "")
	out, errOut, code := h.run("server", "create", "--name", "web2", "--type", "small", "--snapshot", "before", "--no-wait")
	if code != 0 || !strings.Contains(out, "is being created") {
		t.Fatalf("create: %d %q %q", code, out, errOut)
	}
	if got := h.Env.App.Ent.Server.Query().Where(server.Name("web2")).OnlyX(ctx).LocationID; got != h.Env.Location {
		t.Fatalf("location = %d, want the snapshot's %d", got, h.Env.Location)
	}
	_, errOut, code = h.run("server", "create", "--name", "web3", "--type", "small", "--image", "ubuntu", "--no-wait")
	if code != 1 || !strings.Contains(errOut, "--location is required with --image") {
		t.Fatalf("image without location: %d %q", code, errOut)
	}
}

func TestSnapshotRestoreWarnsIdentity(t *testing.T) {
	h := newHarnessQueueOnly(t)
	web := h.Env.RunningServer("web1")
	addCLISnapshot(t, h, "before", web.ServerID, "stale")
	out, errOut, code := h.run("-o", "json", "snapshot", "restore", "before", "--no-wait")
	if code != 0 || !strings.Contains(out, "rebuild_server") {
		t.Fatalf("restore: %d %q %q", code, out, errOut)
	}
	want := "web1's network changed since this snapshot. It will come back with new SSH host keys and run its user data again."
	if !strings.Contains(errOut, want) {
		t.Fatalf("no identity warning: %q", errOut)
	}
}

func TestSnapshotRestoreSourceGone(t *testing.T) {
	h := newHarnessQueueOnly(t)
	web := h.Env.RunningServer("web1")
	addCLISnapshot(t, h, "before", web.ServerID, "")
	h.Env.App.Ent.Snapshot.Update().ClearServerID().ExecX(context.Background())
	out, errOut, code := h.run("snapshot", "restore", "before")
	if code != 1 || out != "" || !strings.Contains(errOut, "This snapshot's server no longer exists. Use `nebla server create --snapshot before` instead.") {
		t.Fatalf("restore: %d %q %q", code, out, errOut)
	}
}

func TestServerCreateSnapshotFlagExclusive(t *testing.T) {
	h := newHarnessQueueOnly(t)
	_, errOut, code := h.run(createArgs("web2", "--snapshot", "before")...)
	if code != 1 || !strings.Contains(errOut, "none of the others can be") {
		t.Fatalf("create: %d %q", code, errOut)
	}
	h.Env.RunningServer("web1")
	_, errOut, code = h.run("server", "rebuild", "web1", "--image", "ubuntu", "--snapshot", "before")
	if code != 1 || !strings.Contains(errOut, "none of the others can be") {
		t.Fatalf("rebuild: %d %q", code, errOut)
	}
}

func TestSnapshotListJSON(t *testing.T) {
	h := newHarnessQueueOnly(t)
	web := h.Env.RunningServer("web1")
	addCLISnapshot(t, h, "before", web.ServerID, "")
	out, errOut, code := h.run("-o", "json", "snapshot", "list", "--server", "web1", "--location", "eu-1")
	if code != 0 {
		t.Fatalf("list: %d %q", code, errOut)
	}
	var got []struct {
		Name   string `json:"name"`
		Server *int64 `json:"server"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil || len(got) != 1 || got[0].Name != "before" || *got[0].Server != web.ServerID {
		t.Fatalf("list json: %v %q", err, out)
	}
	if out, _, code = h.run("snapshot", "describe", "before"); code != 0 || !strings.Contains(out, "Server:") {
		t.Fatalf("describe: %d %q", code, out)
	}
	if out, _, code = h.run("snapshot", "update", "before", "--name", "after"); code != 0 || !strings.Contains(out, "Snapshot after updated") {
		t.Fatalf("update: %d %q", code, out)
	}
}

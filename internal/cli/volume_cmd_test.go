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
	"github.com/nebla-cloud/nebla/internal/ent/volume"
)

// newVolumeHarness offers volumes at eu-1 and succeeds every running action in the background.
func newVolumeHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarnessQueueOnly(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	t.Cleanup(func() { cancel(); <-done })
	client := h.Env.App.Ent
	if err := client.Location.UpdateOneID(h.Env.Location).SetVolumeStorageOk(true).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	go func() {
		defer close(done)
		for ctx.Err() == nil {
			ids, _ := client.Action.Query().Where(action.StatusEQ(action.StatusRunning)).IDs(ctx)
			// Flip the volume first so a command that sees the action done never sees a creating volume.
			_, _ = client.Volume.Update().Where(volume.StatusEQ(volume.StatusCreating)).SetStatus(volume.StatusAvailable).Save(ctx)
			for _, id := range ids {
				_ = actions.Succeed(ctx, client, id)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()
	return h
}

func TestVolumeCreateWaits(t *testing.T) {
	h := newVolumeHarness(t)
	out, errOut, code := h.run("volume", "create", "--name", "data", "--size", "20", "--location", "eu-1", "--label", "env=prod")
	if code != 0 || !strings.Contains(out, " created") || !strings.Contains(errOut, "Waiting for") {
		t.Fatalf("create: %d %q %q", code, out, errOut)
	}
	out, _, code = h.run("volume", "list", "-l", "env=prod")
	if code != 0 || !strings.Contains(out, "data") {
		t.Fatalf("list: %d %q", code, out)
	}
}

func TestVolumeListTable(t *testing.T) {
	h := newVolumeHarness(t)
	h.Env.RunningServer("web")
	if _, errOut, code := h.run("volume", "create", "--name", "data", "--size", "20", "--location", "eu-1"); code != 0 {
		t.Fatal(errOut)
	}
	out, _, code := h.run("volume", "list")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if code != 0 || len(lines) != 2 {
		t.Fatalf("list: %d %q", code, out)
	}
	if f := strings.Fields(lines[0]); strings.Join(f, " ") != "ID NAME SIZE SERVER LOCATION STATUS" {
		t.Fatalf("header %q", lines[0])
	}
	if f := strings.Join(strings.Fields(lines[1])[1:], " "); !strings.HasPrefix(f, "data 20 GB - eu-1 ") {
		t.Fatalf("row %q", lines[1])
	}
}

func TestVolumeAttachByName(t *testing.T) {
	h := newVolumeHarness(t)
	web := h.Env.RunningServer("web")
	if _, errOut, code := h.run("volume", "create", "--name", "data", "--size", "20", "--location", "eu-1"); code != 0 {
		t.Fatal(errOut)
	}
	if _, errOut, code := h.run("volume", "create", "--name", "spare", "--size", "10", "--location", "eu-1"); code != 0 {
		t.Fatal(errOut)
	}
	out, errOut, code := h.run("volume", "attach", "data", "--server", "web")
	if code != 0 || !strings.Contains(out, "Volume data attached to web") {
		t.Fatalf("attach: %d %q %q", code, out, errOut)
	}
	// The harness has no volume workers, so reflect the attach by hand.
	vols, err := h.Env.App.Ent.Volume.Query().Where(volume.Name("data")).All(context.Background())
	if err != nil || len(vols) != 1 {
		t.Fatalf("query: %v %d", err, len(vols))
	}
	if err := h.Env.App.Ent.Volume.UpdateOneID(vols[0].ID).SetStatus(volume.StatusAttached).SetServerID(web.ServerID).SetDevice("virtio1").Exec(context.Background()); err != nil {
		t.Fatal(err)
	}
	out, _, code = h.run("volume", "list", "--server", "web")
	if code != 0 || !strings.Contains(out, "data") || !strings.Contains(out, "web") || strings.Contains(out, "spare") {
		t.Fatalf("list: %d %q", code, out)
	}
}

func TestVolumeDescribeJSON(t *testing.T) {
	h := newVolumeHarness(t)
	if _, errOut, code := h.run("volume", "create", "--name", "data", "--size", "20", "--location", "eu-1"); code != 0 {
		t.Fatal(errOut)
	}
	out, errOut, code := h.run("-o", "json", "volume", "describe", "data")
	if code != 0 {
		t.Fatalf("describe: %d %q", code, errOut)
	}
	var v apiclient.Volume
	if err := json.Unmarshal([]byte(out), &v); err != nil || v.Name != "data" || !strings.HasPrefix(v.LinuxDevice, "/dev/disk/by-id/") {
		t.Fatalf("got %v %+v", err, v)
	}
	out, _, code = h.run("volume", "describe", "data")
	if code != 0 || !strings.Contains(out, "Linux device:") || !strings.Contains(out, "20 GB") {
		t.Fatalf("table: %d %q", code, out)
	}
}

func TestVolumeUpdateResizeDelete(t *testing.T) {
	h := newVolumeHarness(t)
	if _, errOut, code := h.run("volume", "create", "--name", "data", "--size", "20", "--location", "eu-1"); code != 0 {
		t.Fatal(errOut)
	}
	if out, errOut, code := h.run("volume", "update", "data", "--name", "data2"); code != 0 || !strings.Contains(out, "updated") {
		t.Fatalf("update: %d %q %q", code, out, errOut)
	}
	if out, errOut, code := h.run("volume", "resize", "data2", "--size", "30"); code != 0 || !strings.Contains(out, "resized to 30 GB") {
		t.Fatalf("resize: %d %q %q", code, out, errOut)
	}
	if out, errOut, code := h.run("volume", "delete", "data2"); code != 0 || !strings.Contains(out, "Volume data2 deleted") {
		t.Fatalf("delete: %d %q %q", code, out, errOut)
	}
}

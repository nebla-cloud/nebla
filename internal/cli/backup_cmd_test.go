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
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/cli/apiclient"
	"github.com/nebla-cloud/nebla/internal/ent/action"
	"github.com/nebla-cloud/nebla/internal/ent/backup"
	"github.com/nebla-cloud/nebla/internal/servers"
)

// newBackupHarness has backup storage at eu-1, a project quota and a running server "web".
func newBackupHarness(t *testing.T) (*harness, int64) {
	t.Helper()
	h := newHarnessQueueOnly(t)
	ctx := context.Background()
	client := h.Env.App.Ent
	if err := client.Location.UpdateOneID(h.Env.Location).SetBackupStorage("pbs").SetBackupStorageOk(true).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	if err := client.Project.UpdateOneID(h.Env.Project).SetMaxBackups(10).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	return h, h.Env.RunningServer("web").ServerID
}

// putBackup stores a backup row; stale makes it predate a network change.
func putBackup(t *testing.T, h *harness, serverID int64, st backup.Status, stale bool) int64 {
	t.Helper()
	ctx := context.Background()
	srv := h.Env.App.Ent.Server.GetX(ctx, serverID)
	fp, err := servers.NetworkFingerprint(ctx, h.Env.App.Ent, srv)
	if err != nil {
		t.Fatal(err)
	}
	if stale {
		fp = "other"
	}
	c := h.Env.App.Ent.Backup.Create().SetProjectID(h.Env.Project).SetServerID(serverID).SetLocationID(h.Env.Location).
		SetStorage("pbs").SetNode(srv.Node).SetKind(backup.KindManual).SetStatus(st).SetSizeBytes(2048).
		SetNetworkFingerprint(fp).SetInstanceGeneration(servers.InstanceGeneration(srv))
	if st != backup.StatusQueued && st != backup.StatusCreating {
		c.SetVolid(fmt.Sprintf("pbs:backup/vm/101/%d", time.Now().UnixNano()))
	}
	b, err := c.Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return b.ID
}

func enableBackups(t *testing.T, h *harness) {
	t.Helper()
	if _, errOut, code := h.run("server", "backup", "enable", "web", "--keep", "3"); code != 0 {
		t.Fatal(errOut)
	}
}

func serverBackups(t *testing.T, h *harness, name string) apiclient.ServerBackups {
	t.Helper()
	out, errOut, code := h.run("-o", "json", "server", "describe", name)
	if code != 0 {
		t.Fatalf("describe: %d %q", code, errOut)
	}
	var s apiclient.Server
	if err := json.Unmarshal([]byte(out), &s); err != nil {
		t.Fatal(err)
	}
	return s.Backups
}

func TestBackupEnableDisable(t *testing.T) {
	h, _ := newBackupHarness(t)
	out, errOut, code := h.run("server", "backup", "enable", "web", "--keep", "4")
	if code != 0 || !strings.Contains(out, "Backups enabled for web") {
		t.Fatalf("enable: %d %q %q", code, out, errOut)
	}
	if b := serverBackups(t, h, "web"); !b.Enabled || b.Keep != 4 {
		t.Fatalf("after enable: %+v", b)
	}
	out, errOut, code = h.run("server", "backup", "disable", "web")
	if code != 0 || !strings.Contains(out, "Backups disabled for web") {
		t.Fatalf("disable: %d %q %q", code, out, errOut)
	}
	if b := serverBackups(t, h, "web"); b.Enabled || b.Keep != 4 {
		t.Fatalf("after disable: %+v", b)
	}
	if _, _, code = h.run("server", "backup", "enable", "web"); code != 0 {
		t.Fatal("enable without --keep")
	}
	if b := serverBackups(t, h, "web"); !b.Enabled || b.Keep != 4 {
		t.Fatalf("keep not preserved: %+v", b)
	}
}

func TestBackupEnableRejectsBadKeep(t *testing.T) {
	h, _ := newBackupHarness(t)
	_, errOut, code := h.run("server", "backup", "enable", "web", "--keep", "99")
	if code == 0 || errOut == "" {
		t.Fatalf("want failure: %d %q", code, errOut)
	}
}

func TestBackupList(t *testing.T) {
	h, id := newBackupHarness(t)
	bid := putBackup(t, h, id, backup.StatusAvailable, false)
	out, errOut, code := h.run("server", "backup", "list", "web")
	if code != 0 {
		t.Fatalf("list: %d %q", code, errOut)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 || strings.Join(strings.Fields(lines[0]), " ") != "ID KIND STATUS SIZE CREATED" ||
		!strings.HasPrefix(strings.Join(strings.Fields(lines[1]), " "), id64(bid)+" manual available 2.0 KB ") {
		t.Fatalf("list: %q", out)
	}
	out, _, code = h.run("-o", "json", "server", "backup", "list", "web")
	var list []apiclient.Backup
	if err := json.Unmarshal([]byte(out), &list); code != 0 || err != nil || len(list) != 1 || list[0].Id != bid {
		t.Fatalf("json: %d %v %q", code, err, out)
	}
	h2, id2 := newBackupHarness(t)
	_ = id2
	out, _, code = h2.run("-o", "json", "server", "backup", "list", "web")
	if code != 0 || strings.TrimSpace(out) != "[]" {
		t.Fatalf("empty: %d %q", code, out)
	}
}

// settleBackups marks queued and creating backups available after a short pause.
func settleBackups(t *testing.T, h *harness) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	t.Cleanup(func() { cancel(); <-done })
	client := h.Env.App.Ent
	go func() {
		defer close(done)
		for ctx.Err() == nil {
			time.Sleep(100 * time.Millisecond)
			_, _ = client.Backup.Update().Where(backup.StatusIn(backup.StatusQueued, backup.StatusCreating)).
				SetStatus(backup.StatusAvailable).SetVolid("pbs:backup/vm/101/1").Save(ctx)
		}
	}()
}

func TestBackupNowWaits(t *testing.T) {
	h, _ := newBackupHarness(t)
	enableBackups(t, h)
	settleBackups(t, h)
	out, errOut, code := h.run("server", "backup", "now", "web")
	if code != 0 || !strings.Contains(out, "Backup ") || !strings.Contains(out, " of web is available") || !strings.Contains(errOut, "Waiting for backup") {
		t.Fatalf("now: %d %q %q", code, out, errOut)
	}
	out, _, code = h.run("-o", "json", "server", "backup", "list", "web")
	if code != 0 || !strings.Contains(out, `"available"`) {
		t.Fatalf("list: %d %q", code, out)
	}
}

func TestBackupNowFailure(t *testing.T) {
	h, _ := newBackupHarness(t)
	enableBackups(t, h)
	go func() {
		for i := 0; i < 100; i++ {
			time.Sleep(50 * time.Millisecond)
			n, _ := h.Env.App.Ent.Backup.Update().SetStatus(backup.StatusError).Save(context.Background())
			if n > 0 {
				return
			}
		}
	}()
	_, errOut, code := h.run("server", "backup", "now", "web")
	if code == 0 || !strings.Contains(errOut, "failed") {
		t.Fatalf("now: %d %q", code, errOut)
	}
}

func TestBackupNowDropped(t *testing.T) {
	h, _ := newBackupHarness(t)
	enableBackups(t, h)
	// A failed or cancelled backup deletes its row.
	go func() {
		for i := 0; i < 100; i++ {
			time.Sleep(50 * time.Millisecond)
			n, _ := h.Env.App.Ent.Backup.Delete().Exec(context.Background())
			if n > 0 {
				return
			}
		}
	}()
	_, errOut, code := h.run("server", "backup", "now", "web")
	if code == 0 || !strings.Contains(errOut, "failed or was cancelled; see `nebla server actions web`") {
		t.Fatalf("now: %d %q", code, errOut)
	}
}

func TestBackupNowNoWait(t *testing.T) {
	h, _ := newBackupHarness(t)
	enableBackups(t, h)
	out, errOut, code := h.run("server", "backup", "now", "web", "--no-wait")
	if code != 0 || !strings.Contains(out, "Backup ") || !strings.Contains(out, "queued") || strings.Contains(errOut, "Waiting") {
		t.Fatalf("now: %d %q %q", code, out, errOut)
	}
	out, _, code = h.run("-o", "json", "server", "backup", "now", "web", "--no-wait")
	var b apiclient.Backup
	if err := json.Unmarshal([]byte(out), &b); code != 0 || err != nil || b.Status != apiclient.BackupStatusQueued {
		t.Fatalf("json: %d %v %q", code, err, out)
	}
}

func succeedActions(t *testing.T, h *harness) {
	t.Helper()
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
}

func TestBackupDelete(t *testing.T) {
	h, id := newBackupHarness(t)
	succeedActions(t, h)
	bid := putBackup(t, h, id, backup.StatusAvailable, false)
	_, errOut, code := h.runEnv(nil, "n\n", "server", "backup", "delete", "web", id64(bid))
	if code == 0 || !strings.Contains(errOut, "Delete backup") {
		t.Fatalf("declined: %d %q", code, errOut)
	}
	if n := h.Env.App.Ent.Backup.Query().Where(backup.StatusEQ(backup.StatusDeleting)).CountX(context.Background()); n != 0 {
		t.Fatal("declined delete started")
	}
	out, errOut, code := h.run("server", "backup", "delete", "web", id64(bid), "--yes")
	if code != 0 || !strings.Contains(out, "Backup "+id64(bid)+" of web deleted") {
		t.Fatalf("delete: %d %q %q", code, out, errOut)
	}
}

func TestBackupRestoreWarnsIdentity(t *testing.T) {
	h, id := newBackupHarness(t)
	warn := "This server's network changed since this backup. Its network will be reconfigured and SSH host keys will change."
	stale := putBackup(t, h, id, backup.StatusAvailable, true)
	_, errOut, code := h.runEnv(nil, "n\n", "server", "backup", "restore", "web", id64(stale))
	if code == 0 || !strings.Contains(errOut, warn) || !strings.Contains(errOut, "web") {
		t.Fatalf("declined: %d %q", code, errOut)
	}
	if n := h.Env.App.Ent.Action.Query().Where(action.CommandEQ("restore_backup")).CountX(context.Background()); n != 0 {
		t.Fatal("declined restore started")
	}
	fresh := putBackup(t, h, id, backup.StatusAvailable, false)
	_, errOut, code = h.runEnv(nil, "n\n", "server", "backup", "restore", "web", id64(fresh))
	if code == 0 || strings.Contains(errOut, warn) || !strings.Contains(errOut, "Restore server web") {
		t.Fatalf("fresh prompt: %d %q", code, errOut)
	}
}

func TestBackupRestore(t *testing.T) {
	h, id := newBackupHarness(t)
	succeedActions(t, h)
	bid := putBackup(t, h, id, backup.StatusAvailable, false)
	out, errOut, code := h.runEnv(nil, "y\n", "server", "backup", "restore", "web", id64(bid))
	if code != 0 || !strings.Contains(out, "Server web restored from backup "+id64(bid)) || !strings.Contains(errOut, "Waiting for") {
		t.Fatalf("restore: %d %q %q", code, out, errOut)
	}
}

func TestBackupRestoreYesJSON(t *testing.T) {
	h, id := newBackupHarness(t)
	bid := putBackup(t, h, id, backup.StatusAvailable, true)
	out, errOut, code := h.run("-o", "json", "server", "backup", "restore", "web", id64(bid), "--yes", "--no-wait")
	var act apiclient.Action
	if err := json.Unmarshal([]byte(out), &act); code != 0 || err != nil || act.Command != "restore_backup" {
		t.Fatalf("restore: %d %v %q %q", code, err, out, errOut)
	}
}

func TestServerCreateWithBackups(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if err := h.Env.App.Ent.Location.UpdateOneID(h.Env.Location).SetBackupStorage("pbs").SetBackupStorageOk(true).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	if err := h.Env.App.Ent.Project.UpdateOneID(h.Env.Project).SetMaxBackups(10).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	if _, errOut, code := h.run(createArgs("web1", "--backups", "--backup-keep", "5")...); code != 0 {
		t.Fatalf("create: %q", errOut)
	}
	if b := serverBackups(t, h, "web1"); !b.Enabled || b.Keep != 5 {
		t.Fatalf("got %+v", b)
	}
	if _, errOut, code := h.run(createArgs("web2", "--backup-keep", "5")...); code == 0 || !strings.Contains(errOut, "--backups") {
		t.Fatalf("keep without backups: %d %q", code, errOut)
	}
}

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

package servers

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nebla-cloud/nebla/internal/db"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/backup"
	"github.com/nebla-cloud/nebla/internal/ext"
	"github.com/nebla-cloud/nebla/internal/pve"
	"github.com/nebla-cloud/nebla/internal/volumes/vref"
	"github.com/riverqueue/river"
)

const (
	KindRestoreBackup = "restore_backup"
	restoreTimeout    = 2 * time.Hour
	// restoreBusyWait is how long a second run of the same restore snoozes.
	restoreBusyWait = time.Minute
)

// RestoreServerArgs is the restore_backup job; the archive was chosen at request time.
type RestoreServerArgs struct {
	ServerID int64 `json:"server_id"`
	ActionID int64 `json:"action_id"`
	BackupID int64 `json:"backup_id"`
	// Archive is the backup's volume id in Proxmox.
	Archive string `json:"archive"`
	// StartAfter is whether the server was running when the restore was requested.
	StartAfter bool `json:"start_after"`
	// InstanceGeneration pins the cloud-init instance-id generation; 0 uses the new ISO generation.
	InstanceGeneration int `json:"instance_generation,omitempty"`
}

func (RestoreServerArgs) Kind() string                 { return KindRestoreBackup }
func (RestoreServerArgs) InsertOpts() river.InsertOpts { return insertOpts() }

// RestoreServerWorker restores a backup onto a new VM beside the server's, then swaps them as a rebuild does.
type RestoreServerWorker struct {
	river.WorkerDefaults[RestoreServerArgs]
	o         WorkerOptions
	afterStep func(int)
	afterPost func()
	afterUpid func()
}

func NewRestoreServerWorker(o WorkerOptions) *RestoreServerWorker {
	o.defaults()
	return &RestoreServerWorker{o: o}
}

func (*RestoreServerWorker) Timeout(*river.Job[RestoreServerArgs]) time.Duration {
	return restoreTimeout
}

func (w *RestoreServerWorker) Work(ctx context.Context, job *river.Job[RestoreServerArgs]) error {
	// River may rescue a long restore into a second run; only one may touch the server at a time.
	release, ok, err := db.TryAdvisoryLock(ctx, w.o.SQL, "nebla-restore", strconv.FormatInt(job.Args.ServerID, 10))
	if err != nil {
		return err
	}
	if !ok {
		return river.JobSnooze(restoreBusyWait)
	}
	defer release()
	act, row, ret, stop := w.o.begin(ctx, job.Args.ActionID, job.Args.ServerID)
	if stop {
		return ret
	}
	a := job.Args
	r := &rebuildRun{o: &w.o, afterStep: w.afterStep, afterPost: w.afterPost, afterUpid: w.afterUpid, name: "restore", actionID: a.ActionID,
		startAfter: a.StartAfter, instGen: a.InstanceGeneration, archive: a.Archive, row: row,
		finishTx:    func(ctx context.Context, etx *ent.Client) error { return backupAvailable(ctx, etx, a.BackupID) },
		afterFinish: func(ctx context.Context) { w.publishRestored(ctx, a.BackupID) },
		onFail:      func(ctx context.Context) error { return backupAvailable(ctx, w.o.Ent, a.BackupID) }}
	return contract{job: job.JobRow, name: "restore", finished: act.Finished != nil, log: actionLog(w.o.Logger, act, row.ID),
		run: r.run, fail: r.cleanup}.work(ctx)
}

// backupAvailable ends the backup's restoring state.
func backupAvailable(ctx context.Context, c *ent.Client, id int64) error {
	return c.Backup.Update().Where(backup.ID(id), backup.StatusEQ(backup.StatusRestoring)).SetStatus(backup.StatusAvailable).Exec(ctx)
}

func (w *RestoreServerWorker) publishRestored(ctx context.Context, id int64) {
	if w.o.Bus == nil {
		return
	}
	b, err := w.o.Ent.Backup.Get(ctx, id)
	if err != nil {
		w.o.Logger.Warn("restore: load backup for event", "backup_id", id, "err", err)
		return
	}
	w.o.Bus.Publish(ctx, ext.Event{Kind: ext.BackupRestored, ProjectID: b.ProjectID, ServerID: b.ServerID, ResourceType: "backup",
		ResourceID: b.ID, At: time.Now(), Data: map[string]any{"server_id": b.ServerID, "size_bytes": b.SizeBytes,
			"kind": string(b.Kind), "location_id": b.LocationID}})
}

// restore makes the VM at vmid from the archive, retrying on a lost VMID as clone does.
func (b *builder) restore(ctx context.Context) error {
	for range maxVMIDTries {
		if b.st.Upid != nil {
			if moved, err := b.awaitOrMove(ctx, *b.st.Upid); err != nil || !moved {
				return err
			}
			continue
		}
		vmid := b.vmid()
		v, found, err := findVM(ctx, b.pc, vmid)
		if err != nil {
			return err
		}
		if found {
			cfg, err := b.pc.Config(ctx, v.Node, vmid)
			if errors.Is(err, pve.ErrNotFound) {
				continue
			}
			if err != nil {
				return err
			}
			if cfg["lock"] != "" {
				return transient("vm %d is locked (%s); retrying", vmid, cfg["lock"])
			}
			// Our request was sent before its task was saved; only a restore of this server's backup carries its tag.
			if b.st.Nonce != nil && v.Node == b.row.Node && ownedBy(v, b.row.ID) {
				return b.sanitize(ctx)
			}
			if err := b.newVMID(ctx); err != nil {
				return err
			}
			continue
		}
		// The nonce only marks that a request may have been sent; a restored VM cannot carry it.
		if b.st.Nonce == nil {
			nonce := newNonce()
			if err := b.update(ctx, func(s *buildState) { s.Nonce = &nonce }); err != nil {
				return err
			}
		}
		up, err := b.pc.RestoreVM(ctx, b.row.Node, vmid, b.archive, b.diskStorage())
		switch {
		case alreadyExists(err):
			continue
		case err != nil:
			return err
		}
		if b.afterClonePost != nil {
			b.afterClonePost()
		}
		upid := string(up)
		if err := b.update(ctx, func(s *buildState) { s.Upid = &upid }); err != nil {
			return err
		}
		if b.afterUpidSave != nil {
			b.afterUpidSave()
		}
		if moved, err := b.awaitOrMove(ctx, upid); err != nil || !moved {
			return err
		}
	}
	return permanent("no free VMID after %d tries", maxVMIDTries)
}

// volumeKey is a config key a Nebla volume may have been restored into.
var volumeKey = regexp.MustCompile(`^(virtio|unused)\d+$`)

// diskLine is a disk line Proxmox appends to the description for a disk it left out of the backup.
var diskLine = regexp.MustCompile(`^[a-z]+\d+: `)

// sanitize claims the restored VM: the server's tags and VM description, and no Nebla volume keys.
func (b *builder) sanitize(ctx context.Context) error {
	cfg, err := b.pc.Config(ctx, b.row.Node, b.vmid())
	if err != nil {
		return err
	}
	desc, err := b.serverDescription(ctx, cfg["description"])
	if err != nil {
		return err
	}
	kv := map[string]string{"tags": vmTags(b.row.ID)}
	var del []string
	for k, v := range cfg {
		if _, ok := vref.Parse(v); ok && volumeKey.MatchString(k) {
			del = append(del, k)
		}
	}
	switch {
	case desc != "":
		kv["description"] = desc
	case cfg["description"] != "":
		del = append(del, "description")
	}
	if len(del) > 0 {
		slices.Sort(del)
		kv["delete"] = strings.Join(del, ",")
	}
	if err := b.pc.SetConfig(ctx, b.row.Node, b.vmid(), kv); err != nil {
		return err
	}
	return b.update(ctx, func(s *buildState) { s.Upid, s.Nonce, s.Step = nil, nil, stepClone })
}

// serverDescription is the description of the server's VM, or restored without its disk lines once that VM is gone.
func (b *builder) serverDescription(ctx context.Context, restored string) (string, error) {
	if b.row.Vmid != nil {
		cfg, err := b.pc.Config(ctx, b.row.Node, *b.row.Vmid)
		if err == nil {
			return cfg["description"], nil
		}
		if !errors.Is(err, pve.ErrNotFound) {
			return "", err
		}
	}
	lines := slices.DeleteFunc(strings.Split(restored, "\n"), diskLine.MatchString)
	return strings.TrimRight(strings.Join(lines, "\n"), "\n"), nil
}

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

package snapshots

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/db"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/snapshot"
	"github.com/nebla-cloud/nebla/internal/ext"
	"github.com/nebla-cloud/nebla/internal/pve"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/riverqueue/river"
)

// DeleteSnapshotWorker destroys a snapshot's template VM and removes its row.
type DeleteSnapshotWorker struct {
	river.WorkerDefaults[DeleteSnapshotArgs]
	o WorkerOptions
}

func NewDeleteSnapshotWorker(o WorkerOptions) *DeleteSnapshotWorker {
	o.defaults()
	return &DeleteSnapshotWorker{o: o}
}

func (*DeleteSnapshotWorker) Timeout(*river.Job[DeleteSnapshotArgs]) time.Duration { return jobTimeout }

func (w *DeleteSnapshotWorker) Work(ctx context.Context, job *river.Job[DeleteSnapshotArgs]) error {
	a := job.Args
	act, row, ret, stop := w.o.begin(ctx, a.ActionID, a.SnapshotID)
	if stop {
		return ret
	}
	run := func(ctx context.Context) error {
		var pc pve.Client
		connect := func() error {
			var err error
			if pc == nil {
				if pc, err = w.o.pveFor(ctx, row.LocationID); err != nil {
					return servers.StepError("connect to proxmox", err)
				}
			}
			return nil
		}
		if needsRestart(row) {
			acted, err := userActedSince(ctx, w.o.Ent, row)
			if err != nil {
				return err
			}
			if acted {
				// The user set the server's power state since; leave it.
				if row, err = w.o.Ent.Snapshot.UpdateOneID(row.ID).SetWasRunning(false).Save(ctx); err != nil {
					return err
				}
			}
		}
		if needsRestart(row) {
			// A failed create's cleanup never started the server again; that comes first.
			if err := connect(); err != nil {
				return err
			}
			if _, err := w.o.restartSource(ctx, pc, row); err != nil {
				return servers.StepError("start server", err)
			}
		}
		if row.Vmid != nil {
			if err := connect(); err != nil {
				return err
			}
			if err := w.destroy(ctx, pc, row); err != nil {
				return err
			}
		}
		err := db.WithTx(ctx, w.o.SQL, func(_ *sql.Tx, etx *ent.Client) error {
			if err := etx.Snapshot.DeleteOneID(row.ID).Exec(ctx); err != nil && !ent.IsNotFound(err) {
				return err
			}
			return actions.Succeed(ctx, etx, a.ActionID)
		})
		if err != nil {
			return err
		}
		w.o.publish(ctx, ext.SnapshotDeleted, row)
		return nil
	}
	fail := func(ctx context.Context, f servers.Failure) error {
		// Back to the status before the delete; a foreign VM already set the row missing.
		prev := snapshot.Status(a.PrevStatus)
		if snapshot.StatusValidator(prev) != nil || prev == snapshot.StatusDeleting {
			prev = snapshot.StatusAvailable
		}
		err := w.o.Ent.Snapshot.Update().Where(snapshot.ID(row.ID), snapshot.StatusEQ(snapshot.StatusDeleting)).
			SetStatus(prev).Exec(ctx)
		if err != nil {
			return err
		}
		return failAction(ctx, w.o.Ent, a.ActionID, f)
	}
	return w.o.contract(job.JobRow, "delete", act, row.ID, run, fail).Work(ctx)
}

// destroy removes the VM at the row's vmid only when it carries the snapshot's tag; a VM already gone is done.
func (w *DeleteSnapshotWorker) destroy(ctx context.Context, pc pve.Client, row *ent.Snapshot) error {
	vmid := *row.Vmid
	v, found, err := findVM(ctx, pc, vmid)
	if err != nil {
		return servers.StepError("find vm", err)
	}
	if !found {
		return nil
	}
	if !hasTag(v.Tags, SnapshotTag(row.ID)) {
		if err := w.o.Ent.Snapshot.UpdateOneID(row.ID).SetStatus(snapshot.StatusMissing).Exec(ctx); err != nil {
			return err
		}
		return servers.Permanent("vm %d at the snapshot's vmid is not tagged %s, so it was left alone and the snapshot marked missing",
			vmid, SnapshotTag(row.ID))
	}
	up, err := pc.Destroy(ctx, v.Node, vmid)
	if errors.Is(err, pve.ErrNotFound) {
		return nil
	}
	if err == nil {
		err = pc.WaitTask(ctx, v.Node, up)
	}
	if err != nil {
		if _, still, lerr := findVM(ctx, pc, vmid); lerr == nil && !still {
			return nil
		}
		return servers.StepError("destroy vm", err)
	}
	return nil
}

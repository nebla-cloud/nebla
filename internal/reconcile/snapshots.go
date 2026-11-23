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

package reconcile

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nebla-cloud/nebla/internal/db"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/action"
	"github.com/nebla-cloud/nebla/internal/ent/snapshot"
	"github.com/nebla-cloud/nebla/internal/pve"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/nebla-cloud/nebla/internal/snapshots"
)

// syncSnapshots marks available snapshots whose template VM is gone, retries error cleanups and logs untracked templates;
// it never deletes a template it has no row for. all is the VM list already read; allOnline is false when a node may hide VMs.
func (r *Reconciler) syncSnapshots(ctx context.Context, log *slog.Logger, locationID int64, pc pve.Client, all []pve.VM,
	vms view, allOnline bool, listedAt time.Time) error {
	rows, err := r.o.Ent.Snapshot.Query().Where(snapshot.LocationID(locationID)).All(ctx)
	if err != nil {
		return err
	}
	templates := map[int]pve.VM{}
	for _, v := range all {
		if v.Template && slices.Contains(servers.SplitTags(v.Tags), snapshots.TagSnapshot) {
			templates[v.VMID] = v
		}
	}
	known := map[int]bool{}
	ids := map[int64]bool{}
	for _, row := range rows {
		ids[row.ID] = true
		if row.Vmid != nil {
			known[*row.Vmid] = true
		}
	}
	for vmid, v := range templates {
		if !known[vmid] && !ids[tagID(v.Tags)] {
			log.Warn("reconcile: snapshot template without a row left alone", "vmid", vmid, "node", v.Node)
		}
	}
	// A failed listing must not turn every snapshot missing.
	listed := len(all) > 0
	var errs []error
	for _, row := range rows {
		switch row.Status {
		case snapshot.StatusAvailable:
			if !listed || row.Vmid == nil || !r.templateGone(row, templates, vms, allOnline) {
				continue
			}
			if err := r.markSnapshotMissing(ctx, log, row.ID, listedAt); err != nil {
				errs = append(errs, fmt.Errorf("snapshot %d: %w", row.ID, err))
			}
		case snapshot.StatusError:
			o := snapshots.WorkerOptions{SQL: r.o.SQL, Ent: r.o.Ent, Catalog: r.o.Catalog, Logger: log}
			if err := snapshots.RetryCleanup(ctx, o, row, pc); err != nil {
				log.Warn("reconcile: snapshot cleanup retry failed", "snapshot_id", row.ID, "err", err)
			}
		}
	}
	return errors.Join(errs...)
}

// tagID is the snapshot id in a nebla-snapshot-<id> tag, 0 when there is none.
func tagID(tags string) int64 {
	for _, t := range servers.SplitTags(tags) {
		if s, ok := strings.CutPrefix(t, snapshots.TagSnapshot+"-"); ok {
			if id, err := strconv.ParseInt(s, 10, 64); err == nil {
				return id
			}
		}
	}
	return 0
}

// templateGone is true when no template tagged for the row sits at its vmid and no offline node could be hiding it.
func (r *Reconciler) templateGone(row *ent.Snapshot, templates map[int]pve.VM, vms view, allOnline bool) bool {
	if v, ok := templates[*row.Vmid]; ok && slices.Contains(servers.SplitTags(v.Tags), snapshots.SnapshotTag(row.ID)) {
		return false
	}
	if row.Node != "" {
		return vms.online[row.Node]
	}
	return allOnline
}

// markSnapshotMissing re-reads the row under its lock and marks it unless a job owns it or an action ran since the list.
func (r *Reconciler) markSnapshotMissing(ctx context.Context, log *slog.Logger, id int64, listedAt time.Time) error {
	return db.WithTx(ctx, r.o.SQL, func(_ *sql.Tx, etx *ent.Client) error {
		row, err := etx.Snapshot.Query().Where(snapshot.ID(id)).ForUpdate().Only(ctx)
		if ent.IsNotFound(err) {
			return nil
		}
		if err != nil || row.Status != snapshot.StatusAvailable {
			return err
		}
		busy, err := etx.Action.Query().Where(snapshots.ActionsOf(row.ProjectID, row.ID),
			action.Or(action.StatusEQ(action.StatusRunning), action.FinishedGTE(listedAt))).Exist(ctx)
		if err != nil || busy {
			return err
		}
		log.Warn("reconcile: snapshot template gone", "snapshot_id", row.ID, "vmid", *row.Vmid)
		return etx.Snapshot.UpdateOneID(row.ID).SetStatus(snapshot.StatusMissing).Exec(ctx)
	})
}

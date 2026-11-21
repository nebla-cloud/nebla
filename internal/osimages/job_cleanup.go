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

package osimages

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/nebla-cloud/nebla/internal/db"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/catalogbuild"
	"github.com/nebla-cloud/nebla/internal/pve"
	"github.com/riverqueue/river"
)

// rowBuilding is true while the build row is building again, which may reuse the same import volume name.
const rowBuilding = `select exists (select 1 from catalog_builds where id = $1 and status = 'building')`

type CleanupWorker struct {
	river.WorkerDefaults[CleanupArgs]
	s *Service
}

func (*CleanupWorker) Timeout(*river.Job[CleanupArgs]) time.Duration { return cleanupTimeout }

func (w *CleanupWorker) Work(ctx context.Context, job *river.Job[CleanupArgs]) error {
	err := w.work(ctx, job.Args)
	var snooze *river.JobSnoozeError
	if err != nil && job.Attempt >= job.MaxAttempts && !errors.As(err, &snooze) {
		w.s.o.Logger.Error("catalog cleanup discarded, VM or volume may be left behind", "build_id", job.Args.BuildID,
			"location_id", job.Args.LocationID, "node", job.Args.Node, "vmid", job.Args.VMID, "volume", job.Args.Volume,
			"attempt", job.Attempt, "err", err)
	}
	return err
}

func (w *CleanupWorker) work(ctx context.Context, a CleanupArgs) error {
	var busy bool
	if err := w.s.o.SQL.QueryRowContext(ctx, rowBuilding, a.BuildID).Scan(&busy); err != nil {
		return err
	}
	if busy {
		return river.JobSnooze(retireSnooze)
	}
	pc, err := w.s.o.Catalog.ProxmoxFor(ctx, a.LocationID)
	if err != nil {
		return err
	}
	if a.VMID != 0 {
		if err := destroyTagged(ctx, pc, a.VMID, a.Tag); err != nil {
			return err
		}
	}
	if a.Volume == "" {
		return nil
	}
	return w.deleteVolume(ctx, pc, a)
}

// deleteVolume holds the row so an Enable can't restart it and download the same volume meanwhile.
func (w *CleanupWorker) deleteVolume(ctx context.Context, pc pve.Client, a CleanupArgs) error {
	return db.WithTx(ctx, w.s.o.SQL, func(tx *sql.Tx, _ *ent.Client) error {
		var status string
		err := tx.QueryRowContext(ctx, `select status from catalog_builds where id = $1 for share`, a.BuildID).Scan(&status)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if status == string(catalogbuild.StatusBuilding) {
			return river.JobSnooze(retireSnooze)
		}
		storage, _, _ := strings.Cut(a.Volume, ":")
		return deleteVolume(ctx, pc, a.Node, storage, a.Volume)
	})
}

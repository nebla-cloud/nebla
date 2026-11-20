package osimages

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/Hexeption/nebla/internal/db"
	"github.com/Hexeption/nebla/internal/ent"
	"github.com/Hexeption/nebla/internal/ent/catalogbuild"
	"github.com/Hexeption/nebla/internal/pve"
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

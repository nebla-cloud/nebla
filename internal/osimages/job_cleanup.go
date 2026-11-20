package osimages

import (
	"context"
	"strings"
	"time"

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
	a := job.Args
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
	if err := destroyTagged(ctx, pc, a.VMID, a.Tag); err != nil {
		return err
	}
	if a.Volume == "" {
		return nil
	}
	storage, _, _ := strings.Cut(a.Volume, ":")
	return deleteVolume(ctx, pc, a.Node, storage, a.Volume)
}

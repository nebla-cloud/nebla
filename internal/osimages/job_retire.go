package osimages

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Hexeption/nebla/internal/pve"
	"github.com/Hexeption/nebla/internal/servers"
	"github.com/riverqueue/river"
)

const (
	retireTimeout = 5 * time.Minute
	retireSnooze  = time.Minute
)

// inflightClone is true while a create/rebuild could still clone the template.
const inflightClone = `select exists (
	select 1 from servers where location_id = $1 and template_vmid = $2 and status in ('initializing', 'rebuilding')
) or exists (
	select 1 from river_job where kind = $3 and (args->>'template_vmid')::bigint = $2
		and state in ('available', 'pending', 'running', 'scheduled', 'retryable')
)`

type RetireWorker struct {
	river.WorkerDefaults[RetireArgs]
	s *Service
}

func (*RetireWorker) Timeout(*river.Job[RetireArgs]) time.Duration { return retireTimeout }

func (w *RetireWorker) Work(ctx context.Context, job *river.Job[RetireArgs]) error {
	a := job.Args
	log := w.s.o.Logger.With("job_id", job.ID, "location_id", a.LocationID, "vmid", a.VMID, "tag", a.Tag)
	var busy bool
	if err := w.s.o.SQL.QueryRowContext(ctx, inflightClone, a.LocationID, a.VMID, servers.KindRebuildServer).Scan(&busy); err != nil {
		return err
	}
	if busy {
		return river.JobSnooze(retireSnooze)
	}
	pc, err := w.s.o.Catalog.ProxmoxFor(ctx, a.LocationID)
	if err != nil {
		return err
	}
	v, found, err := findVM(ctx, pc, a.VMID)
	if err != nil || !found {
		return err
	}
	cfg, err := pc.Config(ctx, v.Node, a.VMID)
	if errors.Is(err, pve.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if !v.Template || !hasTag(cfg["tags"], a.Tag) {
		log.Warn("not retiring a vm that is not the tagged template")
		return nil
	}
	if cfg["lock"] != "" {
		return fmt.Errorf("vm %d is locked (%s)", a.VMID, cfg["lock"])
	}
	up, err := pc.Destroy(ctx, v.Node, a.VMID)
	if err == nil {
		err = pc.WaitTask(ctx, v.Node, up)
	}
	if errors.Is(err, pve.ErrNotFound) {
		return nil
	}
	return err
}

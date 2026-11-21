package volumes

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/db"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/volume"
	"github.com/nebla-cloud/nebla/internal/ext"
	"github.com/nebla-cloud/nebla/internal/pve"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/nebla-cloud/nebla/internal/volumes/vref"
	"github.com/riverqueue/river"
)

// DeleteVolumeWorker removes a detached volume's disk and row.
type DeleteVolumeWorker struct {
	river.WorkerDefaults[DeleteVolumeArgs]
	o WorkerOptions
}

func NewDeleteVolumeWorker(o WorkerOptions) *DeleteVolumeWorker {
	o.defaults()
	return &DeleteVolumeWorker{o: o}
}

func (*DeleteVolumeWorker) Timeout(*river.Job[DeleteVolumeArgs]) time.Duration { return jobTimeout }

func (w *DeleteVolumeWorker) Work(ctx context.Context, job *river.Job[DeleteVolumeArgs]) error {
	a := job.Args
	act, row, ret, stop := w.o.begin(ctx, a.ActionID, a.VolumeID)
	if stop {
		return ret
	}
	run := func(ctx context.Context) error {
		if row.ServerID != nil {
			return servers.Permanent("volume is attached to server %d", *row.ServerID)
		}
		pc, _, err := w.o.connect(ctx, row.LocationID)
		if err != nil {
			return err
		}
		node, err := w.o.storageNode(ctx, row, nil)
		if err != nil {
			return err
		}
		// An absent disk counts as deleted: real Proxmox may fail freeing it with a task error, not a 404.
		_, present, err := diskPresent(ctx, pc, node, row.Storage, row.ID)
		if err != nil {
			return servers.StepError("list storage", err)
		}
		if present {
			if err := w.remove(ctx, pc, node, row); err != nil {
				return err
			}
		}
		err = db.WithTx(ctx, w.o.SQL, func(_ *sql.Tx, etx *ent.Client) error {
			if err := etx.Volume.DeleteOneID(row.ID).Exec(ctx); err != nil {
				return err
			}
			return actions.Succeed(ctx, etx, a.ActionID)
		})
		if err != nil {
			return err
		}
		w.o.publish(ctx, ext.VolumeDeleted, row, row.SizeGB, false)
		return nil
	}
	fail := func(ctx context.Context, f servers.Failure) error {
		err := w.o.Ent.Volume.Update().Where(volume.ID(row.ID), volume.StatusEQ(volume.StatusDeleting)).
			SetStatus(volume.StatusAvailable).Exec(ctx)
		if err != nil {
			return err
		}
		return failAction(ctx, w.o.Ent, a.ActionID, f)
	}
	return w.o.contract(job.JobRow, "delete", act, row.ID, run, fail).Work(ctx)
}

func (w *DeleteVolumeWorker) remove(ctx context.Context, pc pve.Client, node string, row *ent.Volume) error {
	err := pc.DeleteVolume(ctx, node, row.Storage, row.Storage+":"+vref.Name(row.ID))
	switch {
	case err == nil, errors.Is(err, pve.ErrNotFound):
		return nil
	case isForbidden(err):
		return servers.Permanent("delete volume on storage %s: the token lacks Datastore.Allocate there; "+
			"grant NeblaISO on /storage/%s; re-run the setup command (%v)", row.Storage, row.Storage, err)
	}
	if _, still, lerr := diskPresent(ctx, pc, node, row.Storage, row.ID); lerr == nil && !still {
		return nil
	}
	return servers.StepError("delete volume", err)
}

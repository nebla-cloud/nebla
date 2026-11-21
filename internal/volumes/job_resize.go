package volumes

import (
	"context"
	"database/sql"
	"time"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/db"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ext"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/riverqueue/river"
)

// ResizeVolumeWorker grows an attached volume in Proxmox; a detached one keeps the size saved at request for its next attach.
type ResizeVolumeWorker struct {
	river.WorkerDefaults[ResizeVolumeArgs]
	o WorkerOptions
}

func NewResizeVolumeWorker(o WorkerOptions) *ResizeVolumeWorker {
	o.defaults()
	return &ResizeVolumeWorker{o: o}
}

func (*ResizeVolumeWorker) Timeout(*river.Job[ResizeVolumeArgs]) time.Duration { return jobTimeout }

func (w *ResizeVolumeWorker) Work(ctx context.Context, job *river.Job[ResizeVolumeArgs]) error {
	a := job.Args
	act, row, ret, stop := w.o.begin(ctx, a.ActionID, a.VolumeID)
	if stop {
		return ret
	}
	run := func(ctx context.Context) error {
		pc, vms, err := w.o.connect(ctx, row.LocationID)
		if err != nil {
			return err
		}
		attached := row.ServerID != nil
		applied := row.AppliedGB
		if attached {
			srv, err := w.o.loadServer(ctx, *row.ServerID)
			if err != nil {
				return err
			}
			vm, err := serverVM(vms, srv)
			if err != nil {
				return err
			}
			cfg, err := pc.Config(ctx, vm.Node, vm.VMID)
			if err != nil {
				return servers.StepError("read vm config", err)
			}
			key := ourKey(cfg, row.ID)
			if key == "" {
				return servers.Permanent("vm %d config does not hold the volume", vm.VMID)
			}
			if err := pc.ResizeDisk(ctx, vm.Node, vm.VMID, key, a.SizeGB); err != nil {
				return servers.StepError("grow volume", err)
			}
			applied = a.SizeGB
		}
		err = db.WithTx(ctx, w.o.SQL, func(_ *sql.Tx, etx *ent.Client) error {
			if err := etx.Volume.UpdateOneID(row.ID).SetAppliedGB(applied).Exec(ctx); err != nil {
				return err
			}
			return actions.Succeed(ctx, etx, a.ActionID)
		})
		if err != nil {
			return err
		}
		w.o.publish(ctx, ext.VolumeResized, row, a.SizeGB, attached)
		return nil
	}
	// A resize that never reached the disk gives the extra GB, and so the quota, back; one that did is recorded.
	// Proxmox errors here are logged, never returned, so the action always fails.
	fail := func(ctx context.Context, f servers.Failure) error {
		cur, err := w.o.Ent.Volume.Get(ctx, row.ID)
		if err != nil {
			return err
		}
		if a.PrevSizeGB > 0 && cur.SizeGB == a.SizeGB && cur.AppliedGB < a.SizeGB {
			var u *ent.VolumeUpdateOne
			switch grown, err := w.grown(ctx, cur, a.SizeGB); {
			case err != nil:
				// Unknown size on storage: keep size_gb, as the rescue does for attached volumes.
				w.o.Logger.Warn("resize failed; disk size unknown, size_gb kept", "volume_id", cur.ID, "err", err)
			case grown:
				u = cur.Update().SetAppliedGB(a.SizeGB)
			default:
				u = cur.Update().SetSizeGB(a.PrevSizeGB)
			}
			if u != nil {
				if err := u.Exec(ctx); err != nil {
					return err
				}
			}
		}
		return failAction(ctx, w.o.Ent, a.ActionID, f)
	}
	return w.o.contract(job.JobRow, "resize", act, row.ID, run, fail).Work(ctx)
}

// grown reports whether row's disk is already sizeGB; a detached volume's disk is never touched by resize.
func (w *ResizeVolumeWorker) grown(ctx context.Context, row *ent.Volume, sizeGB int) (bool, error) {
	if row.ServerID == nil {
		return false, nil
	}
	srv, err := w.o.Ent.Server.Get(ctx, *row.ServerID)
	if err != nil && !ent.IsNotFound(err) {
		return false, err
	}
	pc, err := w.o.pveFor(ctx, row.LocationID)
	if err != nil {
		return false, err
	}
	node, err := w.o.storageNode(ctx, row, srv)
	if err != nil {
		return false, err
	}
	it, ok, err := diskPresent(ctx, pc, node, row.Storage, row.Volid, row.ID)
	if err != nil {
		return false, err
	}
	return ok && it.Size >= int64(sizeGB)<<30, nil
}

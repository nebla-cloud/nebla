package servers

import (
	"context"
	"database/sql"
	"time"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/db"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/server"
	"github.com/nebla-cloud/nebla/internal/ext"
	"github.com/nebla-cloud/nebla/internal/ipam"
	"github.com/nebla-cloud/nebla/internal/pve"
	"github.com/riverqueue/river"
)

const deleteTimeout = 5 * time.Minute

func (DeleteServerArgs) InsertOpts() river.InsertOpts { return insertOpts() }

// DeleteServerWorker removes a server's VMs and ISOs, frees its IPs and deletes the row.
type DeleteServerWorker struct {
	river.WorkerDefaults[DeleteServerArgs]
	o WorkerOptions
}

func NewDeleteServerWorker(o WorkerOptions) *DeleteServerWorker {
	o.defaults()
	return &DeleteServerWorker{o: o}
}

func (*DeleteServerWorker) Timeout(*river.Job[DeleteServerArgs]) time.Duration { return deleteTimeout }

func (w *DeleteServerWorker) Work(ctx context.Context, job *river.Job[DeleteServerArgs]) error {
	act, row, ret, stop := w.o.begin(ctx, job.Args.ActionID, job.Args.ServerID)
	if stop {
		return ret
	}
	var pc pve.Client
	run := func(ctx context.Context) error {
		var err error
		if pc, err = w.o.Catalog.ProxmoxFor(ctx, row.LocationID); err != nil {
			return &stepError{"connect to proxmox", err}
		}
		return w.run(ctx, pc, row, job.Args.ActionID)
	}
	fail := func(ctx context.Context, f failure) error {
		cur, err := w.o.Ent.Server.Get(ctx, row.ID)
		if err != nil {
			return err
		}
		if pc == nil {
			pc, _ = w.o.Catalog.ProxmoxFor(ctx, row.LocationID)
		}
		st := realStatus(ctx, pc, cur.ID, cur.Vmid, StatusError)
		if err := w.o.Ent.Server.UpdateOneID(cur.ID).SetStatus(server.Status(st)).Exec(ctx); err != nil {
			return err
		}
		return failAction(ctx, w.o.Ent, job.Args.ActionID, f)
	}
	return contract{job: job.JobRow, name: "delete", finished: act.Finished != nil, log: actionLog(w.o.Logger, act, row.ID),
		run: run, fail: fail}.work(ctx)
}

// run removes the server's VMs (unproven untagged ones are left alone) and ISOs, then its IPs and row.
func (w *DeleteServerWorker) run(ctx context.Context, pc pve.Client, row *ent.Server, actionID int64) error {
	loc, err := w.o.Catalog.GetLocation(ctx, row.LocationID)
	if err != nil {
		return &stepError{"load location", err}
	}
	vols, err := attachedVolumes(ctx, w.o.Ent, row.ID)
	if err != nil {
		return err
	}
	for _, vmid := range []*int{row.Vmid, row.RebuildVmid} {
		if vmid != nil {
			if err := unplugServerVolumes(ctx, pc, w.o.Logger, row.ID, *vmid, vols); err != nil {
				return &stepError{"detach volumes", err}
			}
		}
	}
	if row.Vmid != nil {
		if err := destroyOrForget(ctx, pc, row.ID, *row.Vmid, CloneProof{UPID: row.CloneUpid, Nonce: row.CloneNonce}); err != nil {
			return &stepError{"destroy vm", err}
		}
		if err := w.o.Ent.Server.UpdateOneID(row.ID).ClearVmid().ClearCloneUpid().ClearCloneNonce().Exec(ctx); err != nil {
			return err
		}
	}
	if row.RebuildVmid != nil {
		if err := destroyOrForget(ctx, pc, row.ID, *row.RebuildVmid, CloneProof{UPID: row.RebuildCloneUpid, Nonce: row.RebuildCloneNonce}); err != nil {
			return &stepError{"destroy rebuild vm", err}
		}
		if err := w.o.Ent.Server.UpdateOneID(row.ID).ClearRebuildVmid().ClearRebuildCloneUpid().ClearRebuildCloneNonce().Exec(ctx); err != nil {
			return err
		}
	}
	// A rebuild may have left the generation before or after the current one.
	for gen := max(row.IsoGeneration-1, 1); gen <= row.IsoGeneration+1; gen++ {
		if err := deleteISO(ctx, pc, row.Node, loc.ISOStorage, isoVolid(loc.ISOStorage, row.ID, gen)); err != nil {
			return &stepError{"delete cloud-init iso", err}
		}
	}
	err = db.WithTx(ctx, w.o.SQL, func(_ *sql.Tx, etx *ent.Client) error {
		if err := ipam.Release(ctx, etx, row.ID); err != nil {
			return err
		}
		if err := detachVolumesTx(ctx, etx, row.ID); err != nil {
			return err
		}
		if err := etx.Server.DeleteOneID(row.ID).Exec(ctx); err != nil {
			return err
		}
		return actions.Succeed(ctx, etx, actionID)
	})
	if err != nil {
		return err
	}
	w.o.publish(ctx, ext.ServerDeleted, row, map[string]any{"location": loc.Name})
	return nil
}

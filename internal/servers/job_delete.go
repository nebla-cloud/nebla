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
	"database/sql"
	"time"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/db"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/ipaddress"
	"github.com/nebla-cloud/nebla/internal/ent/server"
	"github.com/nebla-cloud/nebla/internal/ext"
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
	w.deleteBackups(ctx, pc, row)
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
	// A rebuild or sync may have left the generation before or after the current one.
	err = w.o.withISOLease(ctx, row.ID, func(*isoLeaseHeld) error {
		cur, err := w.o.Ent.Server.Get(ctx, row.ID)
		if err != nil {
			return err
		}
		for gen := max(cur.IsoGeneration-1, 1); gen <= cur.IsoGeneration+1; gen++ {
			if err := deleteISO(ctx, pc, row.Node, loc.ISOStorage, isoVolid(loc.ISOStorage, row.ID, gen)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return &stepError{"delete cloud-init iso", err}
	}
	var freed []*ent.IPAddress
	err = db.WithTx(ctx, w.o.SQL, func(_ *sql.Tx, etx *ent.Client) error {
		freed = nil
		if err := lockIPsOf(ctx, etx, row.ID); err != nil {
			return err
		}
		var err error
		if freed, err = releasePrimaries(ctx, etx, row.ID); err != nil {
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
	for _, r := range freed {
		publishPrimary(ctx, w.o.Bus, ext.PrimaryIPDeleted, r)
	}
	w.o.publish(ctx, ext.ServerDeleted, row, map[string]any{"location": loc.Name})
	return nil
}

// lockIPsOf locks every IP row, floating or primary, the server's delete will change, keeping the IP → server lock order.
func lockIPsOf(ctx context.Context, etx *ent.Client, serverID int64) error {
	_, err := etx.IPAddress.Query().Where(ipaddress.Or(ipaddress.ServerID(serverID), ipaddress.ReleasingFromID(serverID),
		ipaddress.PreviousServerID(serverID))).Order(ipaddress.ByID()).Unique(false).ForUpdate().IDs(ctx)
	return err
}

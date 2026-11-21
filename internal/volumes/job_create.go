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

package volumes

import (
	"context"
	"database/sql"
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

// allocMark is the create progress set just before the allocation is sent.
const allocMark = 10

// CreateVolumeWorker allocates the disk and, with a server, attaches it in the same action.
type CreateVolumeWorker struct {
	river.WorkerDefaults[CreateVolumeArgs]
	o WorkerOptions
}

func NewCreateVolumeWorker(o WorkerOptions) *CreateVolumeWorker {
	o.defaults()
	return &CreateVolumeWorker{o: o}
}

func (*CreateVolumeWorker) Timeout(*river.Job[CreateVolumeArgs]) time.Duration { return jobTimeout }

func (w *CreateVolumeWorker) Work(ctx context.Context, job *river.Job[CreateVolumeArgs]) error {
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
		row := row
		if row.Status == volume.StatusCreating {
			// Progress at allocMark means an earlier attempt sent the allocation.
			if row, err = w.alloc(ctx, pc, row, a, act.Progress >= allocMark); err != nil {
				return err
			}
			if err := actions.Progress(ctx, w.o.Ent, a.ActionID, 30); err != nil {
				return err
			}
		}
		attached := a.ServerID != nil
		if attached {
			if row, err = w.o.plug(ctx, pc, vms, row, *a.ServerID, a.ActionID, 60); err != nil {
				return err
			}
		}
		err = db.WithTx(ctx, w.o.SQL, func(_ *sql.Tx, etx *ent.Client) error {
			if err := etx.Volume.UpdateOneID(row.ID).SetStatus(settledStatus(row)).Exec(ctx); err != nil {
				return err
			}
			return actions.Succeed(ctx, etx, a.ActionID)
		})
		if err != nil {
			return err
		}
		w.o.publish(ctx, ext.VolumeCreated, row, row.SizeGB, attached)
		return nil
	}
	fail := func(ctx context.Context, f servers.Failure) error {
		cur, err := w.o.Ent.Volume.Get(ctx, row.ID)
		if err != nil {
			return err
		}
		st := settledStatus(cur)
		if cur.Status == volume.StatusCreating {
			st = volume.StatusError
		}
		if err := cur.Update().SetStatus(st).Exec(ctx); err != nil {
			return err
		}
		if err := failAction(ctx, w.o.Ent, a.ActionID, f); err != nil {
			return err
		}
		// The disk exists and counts toward usage even though the attach failed.
		if st != volume.StatusError {
			w.o.publish(ctx, ext.VolumeCreated, cur, cur.SizeGB, cur.ServerID != nil)
		}
		return nil
	}
	return w.o.contract(job.JobRow, "create", act, row.ID, run, fail).Work(ctx)
}

// alloc creates the disk, on shared storage through the server's node if any, and records its volid.
// "already exists" with our exact name counts as done only when retried, an earlier attempt having sent the allocation.
func (w *CreateVolumeWorker) alloc(ctx context.Context, pc pve.Client, row *ent.Volume, a CreateVolumeArgs, retried bool) (*ent.Volume, error) {
	var srv *ent.Server
	if a.ServerID != nil {
		var err error
		if srv, err = w.o.Ent.Server.Get(ctx, *a.ServerID); err != nil && !ent.IsNotFound(err) {
			return nil, err
		}
	}
	node, err := w.o.storageNode(ctx, row, srv)
	if err != nil {
		return nil, err
	}
	volid := vref.Volid(row.Storage, row.ID, a.File)
	conflict := func() error {
		return servers.Permanent("a disk named %s already exists on %s; it was not created by this volume", volid, row.Storage)
	}
	if !retried {
		_, exists, err := diskPresent(ctx, pc, node, row.Storage, volid)
		if err != nil {
			return nil, servers.StepError("list storage before allocating volume", err)
		}
		if exists {
			return nil, conflict()
		}
	}
	if err := actions.Progress(ctx, w.o.Ent, a.ActionID, allocMark); err != nil {
		return nil, err
	}
	got, err := pc.AllocVolume(ctx, node, row.Storage, vref.OwnerVMID, vref.Filename(row.ID, a.File), row.SizeGB)
	switch {
	case err == nil && got != volid:
		return nil, servers.Permanent("allocate volume: storage %s returned volid %q, want %q", row.Storage, got, volid)
	case err == nil:
	case !alreadyExists(err):
		return nil, servers.StepError("allocate volume", err)
	case !retried:
		return nil, conflict()
	default:
		_, ok, lerr := diskPresent(ctx, pc, node, row.Storage, volid)
		if lerr != nil {
			return nil, servers.StepError("list storage", lerr)
		}
		if !ok {
			return nil, servers.StepError("allocate volume", err)
		}
	}
	return row.Update().SetVolid(volid).SetAppliedGB(row.SizeGB).SetStatus(volume.StatusAvailable).Save(ctx)
}

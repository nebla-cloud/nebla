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
	"errors"
	"time"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/db"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/volume"
	"github.com/nebla-cloud/nebla/internal/ext"
	"github.com/nebla-cloud/nebla/internal/pve"
	"github.com/nebla-cloud/nebla/internal/servers"
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
		if row.Volid != "" {
			pc, _, err := w.o.connect(ctx, row.LocationID)
			if err != nil {
				return err
			}
			node, err := w.o.storageNode(ctx, row, nil)
			if err != nil {
				return err
			}
			if err := w.remove(ctx, pc, node, row); err != nil {
				return err
			}
		}
		err := db.WithTx(ctx, w.o.SQL, func(_ *sql.Tx, etx *ent.Client) error {
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
		st := volume.StatusAvailable
		if row.Volid == "" {
			st = volume.StatusError
		}
		err := w.o.Ent.Volume.Update().Where(volume.ID(row.ID), volume.StatusEQ(volume.StatusDeleting)).
			SetStatus(st).Exec(ctx)
		if err != nil {
			return err
		}
		return failAction(ctx, w.o.Ent, a.ActionID, f)
	}
	return w.o.contract(job.JobRow, "delete", act, row.ID, run, fail).Work(ctx)
}

func (w *DeleteVolumeWorker) remove(ctx context.Context, pc pve.Client, node string, row *ent.Volume) error {
	_, present, err := diskPresent(ctx, pc, node, row.Storage, row.Volid)
	if err != nil {
		return servers.StepError("list storage", err)
	}
	if !present {
		return nil
	}
	err = pc.DeleteVolume(ctx, node, row.Storage, row.Volid)
	switch {
	case err == nil, errors.Is(err, pve.ErrNotFound):
		return nil
	case isForbidden(err):
		return servers.Permanent("delete volume on storage %s: the token lacks Datastore.Allocate there; "+
			"grant NeblaISO on /storage/%s; re-run the setup command (%v)", row.Storage, row.Storage, err)
	}
	if _, still, lerr := diskPresent(ctx, pc, node, row.Storage, row.Volid); lerr == nil && !still {
		return nil
	}
	return servers.StepError("delete volume", err)
}

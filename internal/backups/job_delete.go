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

package backups

import (
	"context"
	"database/sql"
	"time"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/db"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/backup"
	"github.com/nebla-cloud/nebla/internal/ext"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/riverqueue/river"
)

// DeleteBackupWorker deletes a backup's archive and row.
type DeleteBackupWorker struct {
	river.WorkerDefaults[DeleteBackupArgs]
	o WorkerOptions
}

func NewDeleteBackupWorker(o WorkerOptions) *DeleteBackupWorker {
	o.defaults()
	return &DeleteBackupWorker{o: o}
}

func (*DeleteBackupWorker) Timeout(*river.Job[DeleteBackupArgs]) time.Duration { return jobTimeout }

func (w *DeleteBackupWorker) Work(ctx context.Context, job *river.Job[DeleteBackupArgs]) error {
	a := job.Args
	act, err := w.o.Ent.Action.Get(ctx, a.ActionID)
	if ent.IsNotFound(err) {
		return river.JobCancel(err)
	}
	if err != nil {
		return err
	}
	row, err := w.o.Ent.Backup.Get(ctx, a.BackupID)
	if ent.IsNotFound(err) {
		// Already gone, with its server or by an earlier attempt.
		if act.Finished == nil {
			return actions.Succeed(ctx, w.o.Ent, act.ID)
		}
		return nil
	}
	if err != nil {
		return err
	}
	run := func(ctx context.Context) error {
		if row.Volid != nil {
			if err := w.o.deleteArchive(ctx, row); err != nil {
				return err
			}
		}
		err := db.WithTx(ctx, w.o.SQL, func(_ *sql.Tx, etx *ent.Client) error {
			if err := etx.Backup.DeleteOneID(row.ID).Exec(ctx); err != nil && !ent.IsNotFound(err) {
				return err
			}
			return actions.Succeed(ctx, etx, act.ID)
		})
		if err != nil {
			return err
		}
		if row.Volid != nil {
			w.o.publish(ctx, ext.BackupDeleted, row, nil)
		}
		return nil
	}
	fail := func(ctx context.Context, f servers.Failure) error {
		st := backup.StatusAvailable
		if row.Volid == nil {
			st = backup.StatusError
		}
		err := w.o.Ent.Backup.Update().Where(backup.ID(row.ID), backup.StatusEQ(backup.StatusDeleting)).SetStatus(st).Exec(ctx)
		if err != nil {
			return err
		}
		return failAction(ctx, w.o.Ent, act.ID, f)
	}
	log := w.o.Logger.With("backup_id", row.ID, "server_id", row.ServerID, "action_id", act.ID, "command", act.Command)
	return servers.Contract{Job: job.JobRow, Name: "delete backup", Finished: act.Finished != nil, Log: log, Run: run, Fail: fail}.Work(ctx)
}

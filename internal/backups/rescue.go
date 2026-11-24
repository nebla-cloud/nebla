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
	"errors"

	"github.com/nebla-cloud/nebla/internal/db"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/jobs"
	"github.com/nebla-cloud/nebla/internal/servers"
)

// live is a SQL condition on river_job alias j: a job that can still run.
const live = `j.state not in ('discarded', 'cancelled', 'completed')`

// RescueOrphaned re-enqueues pending backups whose job was lost and puts deletes and restores that lost their action back.
// It runs at startup, after the server rescue has failed orphaned actions.
func RescueOrphaned(ctx context.Context, sqlDB *sql.DB, j *jobs.Jobs) error {
	return db.WithTx(ctx, sqlDB, func(tx *sql.Tx, _ *ent.Client) error {
		if err := requeueLost(ctx, tx, j); err != nil {
			return err
		}
		if err := releaseRestoring(ctx, tx); err != nil {
			return err
		}
		// Startup only: a running prune also holds rows deleting without an action. A delete job's from is the status before it.
		_, err := tx.ExecContext(ctx, `
			update backups b set status = coalesce(
				(select j.args->>'from' from river_job j where j.kind = $1 and (j.args->>'backup_id')::bigint = b.id
					order by j.id desc limit 1),
				case when b.volid is null then 'error' else 'available' end)
			where b.status = 'deleting' and not exists (
				select 1 from actions a join river_job j on j.id = a.river_job_id
				where a.command = $1 and a.status = 'running' and (j.args->>'backup_id')::bigint = b.id and `+live+`)`,
			KindDeleteBackup)
		return err
	})
}

// releaseRestoring makes restoring backups available again once no live restore_backup action holds them.
func releaseRestoring(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
		update backups b set status = 'available'
		where b.status = 'restoring' and not exists (
			select 1 from actions a join river_job j on j.id = a.river_job_id
			where a.command = $1 and a.status = 'running' and (j.args->>'backup_id')::bigint = b.id and `+live+`)`,
		servers.KindRestoreBackup)
	return err
}

// requeueLost enqueues a create_backup job for each pending backup that has no live one.
func requeueLost(ctx context.Context, tx *sql.Tx, j *jobs.Jobs) error {
	q, err := tx.QueryContext(ctx, `
		select b.id from backups b where b.status in ('queued', 'creating') and not exists (
			select 1 from river_job j where j.kind = $1 and (j.args->>'backup_id')::bigint = b.id and `+live+`)
		order by b.id for update of b skip locked`, KindCreateBackup)
	if err != nil {
		return err
	}
	var ids []int64
	for q.Next() {
		var id int64
		if err := q.Scan(&id); err != nil {
			q.Close()
			return err
		}
		ids = append(ids, id)
	}
	if err := errors.Join(q.Err(), q.Close()); err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := j.InsertTx(ctx, tx, CreateBackupArgs{BackupID: id}, nil); err != nil {
			return err
		}
	}
	return nil
}

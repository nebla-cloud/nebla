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

package snapshots

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/db"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/snapshot"
	"github.com/nebla-cloud/nebla/internal/jobs"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverdatabasesql"
)

// RescueOrphaned requeues orphaned snapshot actions, a lost create only to clean up, and returns how many.
func RescueOrphaned(ctx context.Context, c *ent.Client, sqlDB *sql.DB, _ servers.Publisher) (int, error) {
	ins, err := river.NewClient(riverdatabasesql.New(sqlDB), &river.Config{})
	if err != nil {
		return 0, fmt.Errorf("river inserter: %w", err)
	}
	n := 0
	err = db.WithTx(ctx, sqlDB, func(tx *sql.Tx, etx *ent.Client) error {
		n = 0
		// Rescues run at startup and from the reconciler; one at a time so no action gets two jobs.
		if _, err := tx.ExecContext(ctx, `select pg_advisory_xact_lock(hashtext('snapshots:rescue'))`); err != nil {
			return err
		}
		q, err := tx.QueryContext(ctx, `
			select a.id, a.command, a.resources, (select j.args from river_job j where j.id = a.river_job_id) from actions a
			where a.command = any($1) and `+jobs.Orphaned+`
			order by a.id for update of a`, Commands)
		if err != nil {
			return fmt.Errorf("find orphaned snapshot actions: %w", err)
		}
		type lost struct {
			id       int64
			command  string
			snapshot int64
			args     []byte
		}
		var found []lost
		for q.Next() {
			var l lost
			var doc []byte
			if err := q.Scan(&l.id, &l.command, &doc, &l.args); err != nil {
				q.Close()
				return err
			}
			var res []actions.Resource
			if err := json.Unmarshal(doc, &res); err != nil {
				q.Close()
				return err
			}
			if i := slices.IndexFunc(res, func(r actions.Resource) bool { return r.Type == ResourceType }); i >= 0 {
				l.snapshot = res[i].ID
			}
			found = append(found, l)
		}
		if err := errors.Join(q.Err(), q.Close()); err != nil {
			return err
		}
		for _, l := range found {
			if err := rescueOne(ctx, tx, etx, ins, l.id, l.command, l.snapshot, l.args); err != nil {
				return fmt.Errorf("rescue action %d: %w", l.id, err)
			}
			n++
		}
		return nil
	})
	return n, err
}

func rescueOne(ctx context.Context, tx *sql.Tx, etx *ent.Client, ins *river.Client[*sql.Tx], actionID int64, command string,
	snapshotID int64, prevArgs []byte) error {
	var args river.JobArgs
	switch command {
	case KindCreateSnapshot:
		// The new job sees the row in error and only compensates.
		err := etx.Snapshot.Update().Where(snapshot.ID(snapshotID), snapshot.StatusEQ(snapshot.StatusCreating)).
			SetStatus(snapshot.StatusError).Exec(ctx)
		if err != nil {
			return err
		}
		args = CreateSnapshotArgs{SnapshotID: snapshotID, ActionID: actionID}
	case KindDeleteSnapshot:
		var prev DeleteSnapshotArgs
		if prevArgs != nil {
			_ = json.Unmarshal(prevArgs, &prev)
		}
		if prev.PrevStatus == "" {
			st, err := inferPrevStatus(ctx, etx, snapshotID)
			if err != nil {
				return err
			}
			prev.PrevStatus = st
		}
		args = DeleteSnapshotArgs{SnapshotID: snapshotID, ActionID: actionID, PrevStatus: prev.PrevStatus}
	default:
		return fmt.Errorf("unknown command %q", command)
	}
	res, err := ins.InsertTx(ctx, tx, args, nil)
	if err != nil {
		return err
	}
	return actions.AttachJob(ctx, etx, actionID, res.Job.ID)
}

// inferPrevStatus guesses a lost delete's earlier status: error for a failed create's leftover or a half-made row, else available.
func inferPrevStatus(ctx context.Context, etx *ent.Client, id int64) (string, error) {
	row, err := etx.Snapshot.Get(ctx, id)
	if ent.IsNotFound(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if row.WasRunning || row.Vmid == nil || row.Step < stepFinish {
		return string(snapshot.StatusError), nil
	}
	return string(snapshot.StatusAvailable), nil
}

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
	"errors"

	"github.com/nebla-cloud/nebla/internal/db"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/server"
	"github.com/nebla-cloud/nebla/internal/ent/snapshot"
	"github.com/nebla-cloud/nebla/internal/pve"
	"github.com/nebla-cloud/nebla/internal/servers"
)

// RetryCleanup finishes a failed create's cleanup on an error row: it starts the source server if it still needs it,
// destroys the VM only when tag and nonce prove it ours, then deletes the row. Any failure leaves the row in error.
func RetryCleanup(ctx context.Context, o WorkerOptions, row *ent.Snapshot, pc pve.Client) error {
	o.defaults()
	row, err := claimRetry(ctx, o, row)
	if err != nil || row == nil {
		return err
	}
	c := &create{o: &o, row: row, log: o.Logger, pc: pc}
	if err := c.undo(ctx, false); err != nil {
		return err
	}
	return db.WithTx(ctx, o.SQL, func(_ *sql.Tx, etx *ent.Client) error {
		cur, err := lockSnapshot(ctx, etx, row.ProjectID, row.ID)
		if errors.Is(err, errNotFound) {
			return nil
		}
		if err != nil || cur.Status != snapshot.StatusError {
			return err
		}
		busy, err := hasRunningAction(ctx, etx, row.ProjectID, row.ID)
		if err != nil || busy {
			return err
		}
		return etx.Snapshot.DeleteOneID(row.ID).Exec(ctx)
	})
}

// claimRetry re-reads the row under the server and snapshot locks, nil to skip; a user action since clears was_running.
func claimRetry(ctx context.Context, o WorkerOptions, row *ent.Snapshot) (*ent.Snapshot, error) {
	var out *ent.Snapshot
	err := db.WithTx(ctx, o.SQL, func(_ *sql.Tx, etx *ent.Client) error {
		if row.ServerID != nil {
			_, err := etx.Server.Query().Where(server.ID(*row.ServerID)).ForUpdate().Only(ctx)
			if err != nil && !ent.IsNotFound(err) {
				return err
			}
			busy, err := servers.HasRunningAction(ctx, etx, row.ProjectID, *row.ServerID)
			if err != nil || busy {
				return err
			}
		}
		cur, err := lockSnapshot(ctx, etx, row.ProjectID, row.ID)
		if errors.Is(err, errNotFound) {
			return nil
		}
		if err != nil || cur.Status != snapshot.StatusError {
			return err
		}
		busy, err := hasRunningAction(ctx, etx, row.ProjectID, row.ID)
		if err != nil || busy {
			return err
		}
		if needsRestart(cur) {
			acted, err := userActedSince(ctx, etx, cur)
			if err != nil {
				return err
			}
			if acted {
				if cur, err = etx.Snapshot.UpdateOneID(cur.ID).SetWasRunning(false).Save(ctx); err != nil {
					return err
				}
			}
		}
		out = cur
		return nil
	})
	return out, err
}

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
	"github.com/nebla-cloud/nebla/internal/ent/snapshot"
	"github.com/nebla-cloud/nebla/internal/pve"
)

// RetryCleanup finishes a failed create's cleanup on an error row: it starts the source server if it still needs it,
// destroys the VM only when tag and nonce prove it ours, then deletes the row. Any failure leaves the row in error.
func RetryCleanup(ctx context.Context, o WorkerOptions, row *ent.Snapshot, pc pve.Client) error {
	o.defaults()
	busy, err := hasRunningAction(ctx, o.Ent, row.ProjectID, row.ID)
	if err != nil || busy {
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

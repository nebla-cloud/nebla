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
	"errors"
	"fmt"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/nebla-cloud/nebla/internal/db"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/server"
	"github.com/nebla-cloud/nebla/internal/ent/setting"
)

const backupSlotBackfillKey = "servers.backup_slot_backfilled"

// BackfillBackupSlots spreads servers from before backups, all at slot 0, over the day, once per install.
func BackfillBackupSlots(ctx context.Context, sqlDB *sql.DB) (int, error) {
	n := 0
	err := db.WithTx(ctx, sqlDB, func(_ *sql.Tx, etx *ent.Client) error {
		_, err := etx.Setting.Create().SetID(backupSlotBackfillKey).SetValue("true").
			OnConflictColumns(setting.FieldID).DoNothing().ID(ctx)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		n, err = etx.Server.Update().Where(server.BackupSlot(0)).Modify(func(u *entsql.UpdateBuilder) {
			u.Set(server.FieldBackupSlot, entsql.Expr(fmt.Sprintf("(%s * %d) %% 1440", server.FieldID, backupSlotPrime)))
		}).Save(ctx)
		return err
	})
	return n, err
}

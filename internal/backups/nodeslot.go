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
	"fmt"

	"github.com/nebla-cloud/nebla/internal/db"
)

// nodeSlot takes the one backup slot of a node, held until release; ok is false while another backup holds it.
func nodeSlot(ctx context.Context, sqlDB *sql.DB, locationID int64, node string) (release func(), ok bool, err error) {
	return db.TryAdvisoryLock(ctx, sqlDB, "nebla-backup", fmt.Sprintf("%d/%s", locationID, node))
}

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

	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/backup"
	"github.com/nebla-cloud/nebla/internal/ent/server"
	"github.com/nebla-cloud/nebla/internal/projects"
	"github.com/nebla-cloud/nebla/internal/servers"
)

// UsageWith adds the project's stored backups and its reserved backup slots to next's usage.
func UsageWith(next projects.UsageFunc) projects.UsageFunc {
	return func(ctx context.Context, tx *ent.Client, projectID int64) (projects.Usage, error) {
		u, err := next(ctx, tx, projectID)
		if err != nil {
			return projects.Usage{}, err
		}
		rows, err := tx.Backup.Query().Where(backup.ProjectID(projectID), servers.StoredBackups).Select(backup.FieldServerID).All(ctx)
		if err != nil {
			return projects.Usage{}, err
		}
		u.Backups = len(rows)
		stored := map[int64]int{}
		for _, r := range rows {
			stored[r.ServerID]++
		}
		srvs, err := tx.Server.Query().Where(server.ProjectID(projectID)).
			Select(server.FieldID, server.FieldBackupsEnabled, server.FieldBackupKeep).All(ctx)
		if err != nil {
			return projects.Usage{}, err
		}
		for _, s := range srvs {
			if s.BackupsEnabled {
				u.BackupsReserved += s.BackupKeep
			} else {
				u.BackupsReserved += stored[s.ID]
			}
		}
		return u, nil
	}
}

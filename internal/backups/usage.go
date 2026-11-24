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
		var counts []struct {
			ServerID int64 `json:"server_id"`
			Count    int   `json:"count"`
		}
		err = tx.Backup.Query().Where(backup.ProjectID(projectID), servers.StoredBackups).
			GroupBy(backup.FieldServerID).Aggregate(ent.Count()).Scan(ctx, &counts)
		if err != nil {
			return projects.Usage{}, err
		}
		stored := make(map[int64]int, len(counts))
		for _, c := range counts {
			stored[c.ServerID] = c.Count
			u.Backups += c.Count
		}
		srvs, err := tx.Server.Query().Where(server.ProjectID(projectID)).
			Select(server.FieldID, server.FieldBackupsEnabled, server.FieldBackupKeep).All(ctx)
		if err != nil {
			return projects.Usage{}, err
		}
		for _, s := range srvs {
			u.BackupsReserved += servers.BackupReservation(s.BackupsEnabled, s.BackupKeep, stored[s.ID])
		}
		return u, nil
	}
}

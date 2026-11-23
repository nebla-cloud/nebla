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

// Package snapshots holds server snapshots.
package snapshots

import (
	"context"

	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/snapshot"
	"github.com/nebla-cloud/nebla/internal/projects"
)

// HasSnapshotsWith reports next's answer, or whether the project still owns snapshots.
func HasSnapshotsWith(next projects.HasServersFunc) projects.HasServersFunc {
	return func(ctx context.Context, tx *ent.Client, projectID int64) (bool, error) {
		busy, err := next(ctx, tx, projectID)
		if err != nil || busy {
			return busy, err
		}
		return tx.Snapshot.Query().Where(snapshot.ProjectID(projectID)).Exist(ctx)
	}
}

// UsageWith adds the project's snapshots and their GB to next's usage.
func UsageWith(next projects.UsageFunc) projects.UsageFunc {
	return func(ctx context.Context, tx *ent.Client, projectID int64) (projects.Usage, error) {
		u, err := next(ctx, tx, projectID)
		if err != nil {
			return projects.Usage{}, err
		}
		rows, err := tx.Snapshot.Query().Where(snapshot.ProjectID(projectID)).Select(snapshot.FieldSizeGB).All(ctx)
		if err != nil {
			return projects.Usage{}, err
		}
		u.Snapshots = len(rows)
		for _, r := range rows {
			u.SnapshotGB += r.SizeGB
		}
		return u, nil
	}
}

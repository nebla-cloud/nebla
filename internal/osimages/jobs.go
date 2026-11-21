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

package osimages

import (
	"context"

	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/jobs"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

const (
	KindBuild   = "os_template_build"
	KindRetire  = "os_template_retire"
	KindCleanup = "os_template_cleanup"
)

// BuildArgs builds or refreshes one catalog_builds row; the worker no-ops unless the row is building with its job id.
type BuildArgs struct {
	BuildID int64 `json:"build_id"`
}

func (BuildArgs) Kind() string { return KindBuild }

func (BuildArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: jobs.QueueMaintenance, MaxAttempts: MaxAttempts + spareAttempts}
}

// RetireArgs destroys a catalog template VM once no server can still clone it.
type RetireArgs struct {
	LocationID int64  `json:"location_id"`
	Node       string `json:"node"`
	VMID       int    `json:"vmid"`
	Tag        string `json:"tag"`
}

func (RetireArgs) Kind() string { return KindRetire }

func (RetireArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: jobs.QueueMaintenance, UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: jobs.LiveStates}}
}

// CleanupArgs destroys a failed build's half-built VM and its import volume; Volume "" skips the volume.
type CleanupArgs struct {
	BuildID    int64  `json:"build_id"`
	LocationID int64  `json:"location_id"`
	Node       string `json:"node"`
	VMID       int    `json:"vmid"`
	Tag        string `json:"tag"`
	Volume     string `json:"volume"`
}

func (CleanupArgs) Kind() string { return KindCleanup }

func (CleanupArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: jobs.QueueMaintenance, UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: jobs.LiveStates}}
}

// TemplatesPending is the catalog.Hooks.TemplatesPending implementation; retryable counts as live here.
func TemplatesPending(ctx context.Context, c *ent.Client, locationID int64) (bool, error) {
	states := []string{string(rivertype.JobStateRetryable)}
	for _, st := range jobs.LiveStates {
		states = append(states, string(st))
	}
	rows, err := c.QueryContext(ctx, `select exists (select 1 from river_job where kind in ($1, $2)
		and (args->>'location_id')::bigint = $3 and state::text = any($4))`,
		KindRetire, KindCleanup, locationID, states)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	var ok bool
	if rows.Next() {
		if err := rows.Scan(&ok); err != nil {
			return false, err
		}
	}
	return ok, rows.Err()
}

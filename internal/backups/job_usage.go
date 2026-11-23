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
	"slices"
	"time"

	"github.com/nebla-cloud/nebla/internal/ent/backup"
	"github.com/nebla-cloud/nebla/internal/ext"
	"github.com/nebla-cloud/nebla/internal/jobs"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

const (
	KindUsage  = "backup_usage"
	usageBatch = 500
)

// billed are the statuses whose archive exists in storage.
var billed = []backup.Status{backup.StatusAvailable, backup.StatusRestoring, backup.StatusDeleting}

// UsageArgs is the hourly job that publishes backup.usage for every stored backup.
type UsageArgs struct{}

func (UsageArgs) Kind() string { return KindUsage }

// InsertOpts allow one run per server usage period, even across restarts.
func (UsageArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: jobs.QueueMaintenance, MaxAttempts: 1, UniqueOpts: river.UniqueOpts{ByPeriod: servers.UsageInterval,
		ByState: append(slices.Clone(jobs.LiveStates), rivertype.JobStateCompleted)}}
}

// UsagePeriodicJob schedules the usage job every servers.UsageInterval.
func UsagePeriodicJob() *river.PeriodicJob {
	return river.NewPeriodicJob(river.PeriodicInterval(servers.UsageInterval),
		func() (river.JobArgs, *river.InsertOpts) { return UsageArgs{}, nil }, &river.PeriodicJobOpts{ID: KindUsage, RunOnStart: true})
}

type UsageWorker struct {
	river.WorkerDefaults[UsageArgs]
	o WorkerOptions
}

func NewUsageWorker(o WorkerOptions) *UsageWorker {
	o.defaults()
	return &UsageWorker{o: o}
}

func (*UsageWorker) Timeout(*river.Job[UsageArgs]) time.Duration { return jobTimeout }

// Work publishes each stored backup's size snapshot; pending, failed and missing backups are skipped.
func (w *UsageWorker) Work(ctx context.Context, job *river.Job[UsageArgs]) error {
	if w.o.Bus == nil {
		return nil
	}
	at := time.Now()
	if job.JobRow != nil && !job.CreatedAt.IsZero() {
		at = job.CreatedAt
	}
	// period matches server.usage, so consumers dedupe on (resource, period) alike.
	period := at.UTC().Truncate(servers.UsageInterval).Format(time.RFC3339)
	var after int64
	for {
		rows, err := w.o.Ent.Backup.Query().Where(backup.IDGT(after), backup.StatusIn(billed...)).Order(backup.ByID()).Limit(usageBatch).All(ctx)
		if err != nil {
			return err
		}
		for _, r := range rows {
			w.o.publish(ctx, ext.BackupUsage, r, map[string]any{"period": period, "status": string(r.Status)})
		}
		if len(rows) < usageBatch {
			return nil
		}
		after = rows[len(rows)-1].ID
	}
}

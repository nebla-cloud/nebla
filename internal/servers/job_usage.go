package servers

import (
	"context"
	"slices"
	"time"

	"github.com/nebla-cloud/nebla/internal/ent/server"
	"github.com/nebla-cloud/nebla/internal/ext"
	"github.com/nebla-cloud/nebla/internal/jobs"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

const (
	KindUsage     = "server_usage"
	UsageInterval = time.Hour
	usageBatch    = 500
)

// UsageArgs is the hourly job that publishes server.usage for every server.
type UsageArgs struct{}

func (UsageArgs) Kind() string { return KindUsage }

// InsertOpts allow one run per hour, even across restarts, since the periodic schedule lives in memory.
func (UsageArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: jobs.QueueMaintenance, MaxAttempts: 1, UniqueOpts: river.UniqueOpts{ByPeriod: UsageInterval,
		ByState: append(slices.Clone(jobs.LiveStates), rivertype.JobStateCompleted)}}
}

// UsagePeriodicJob schedules the usage job every UsageInterval.
func UsagePeriodicJob() *river.PeriodicJob {
	return river.NewPeriodicJob(river.PeriodicInterval(UsageInterval),
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

func (*UsageWorker) Timeout(*river.Job[UsageArgs]) time.Duration { return 5 * time.Minute }

// Work publishes each server's type, status and size snapshot.
func (w *UsageWorker) Work(ctx context.Context, job *river.Job[UsageArgs]) error {
	if w.o.Bus == nil {
		return nil
	}
	types, err := w.o.Ent.ServerType.Query().All(ctx)
	if err != nil {
		return err
	}
	names := make(map[int64]string, len(types))
	for _, t := range types {
		names[t.ID] = t.Name
	}
	now := time.Now()
	at := now
	if job.JobRow != nil && !job.CreatedAt.IsZero() {
		at = job.CreatedAt
	}
	// period matches the uniqueness window, so consumers can dedupe on (server_id, period).
	period := at.UTC().Truncate(UsageInterval).Format(time.RFC3339)
	var after int64
	for {
		rows, err := w.o.Ent.Server.Query().Where(server.IDGT(after)).Order(server.ByID()).Limit(usageBatch).All(ctx)
		if err != nil {
			return err
		}
		for _, r := range rows {
			w.o.Bus.Publish(ctx, ext.Event{Kind: ext.ServerUsage, ProjectID: r.ProjectID, ServerID: r.ID, ResourceType: "server", ResourceID: r.ID, At: now, Data: map[string]any{
				"server_type": names[r.ServerTypeID], "status": string(r.Status),
				"cores": r.Cores, "memory_mb": r.MemoryMB, "disk_gb": r.DiskGB, "period": period, "location_id": r.LocationID,
			}})
		}
		if len(rows) < usageBatch {
			return nil
		}
		after = rows[len(rows)-1].ID
	}
}

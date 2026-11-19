package servers

import (
	"context"
	"time"

	"github.com/Hexeption/nebla/internal/ent/server"
	"github.com/Hexeption/nebla/internal/ext"
	"github.com/Hexeption/nebla/internal/jobs"
	"github.com/riverqueue/river"
)

const (
	KindUsage     = "server_usage"
	UsageInterval = time.Hour
	usageBatch    = 500
)

// UsageArgs is the hourly job that publishes server.usage for every server.
type UsageArgs struct{}

func (UsageArgs) Kind() string { return KindUsage }

func (UsageArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: jobs.QueueMaintenance, MaxAttempts: 1, UniqueOpts: river.UniqueOpts{ByState: jobs.LiveStates}}
}

// UsagePeriodicJob schedules the usage job every UsageInterval.
func UsagePeriodicJob() *river.PeriodicJob {
	return river.NewPeriodicJob(river.PeriodicInterval(UsageInterval),
		func() (river.JobArgs, *river.InsertOpts) { return UsageArgs{}, nil }, &river.PeriodicJobOpts{ID: KindUsage})
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
func (w *UsageWorker) Work(ctx context.Context, _ *river.Job[UsageArgs]) error {
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
	var after int64
	for {
		rows, err := w.o.Ent.Server.Query().Where(server.IDGT(after)).Order(server.ByID()).Limit(usageBatch).All(ctx)
		if err != nil {
			return err
		}
		for _, r := range rows {
			w.o.Bus.Publish(ctx, ext.Event{Kind: ext.ServerUsage, ProjectID: r.ProjectID, ServerID: r.ID, At: now, Data: map[string]any{
				"server_type": names[r.ServerTypeID], "status": string(r.Status),
				"cores": r.Cores, "memory_mb": r.MemoryMB, "disk_gb": r.DiskGB,
			}})
		}
		if len(rows) < usageBatch {
			return nil
		}
		after = rows[len(rows)-1].ID
	}
}

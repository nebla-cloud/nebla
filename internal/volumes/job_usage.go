package volumes

import (
	"context"
	"slices"
	"time"

	"github.com/nebla-cloud/nebla/internal/ent/volume"
	"github.com/nebla-cloud/nebla/internal/ext"
	"github.com/nebla-cloud/nebla/internal/jobs"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

const (
	KindUsage  = "volume_usage"
	usageBatch = 500
)

// UsageArgs is the hourly job that publishes volume.usage for every volume.
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

func (*UsageWorker) Timeout(*river.Job[UsageArgs]) time.Duration { return 5 * time.Minute }

// Work publishes each volume's size snapshot.
func (w *UsageWorker) Work(ctx context.Context, job *river.Job[UsageArgs]) error {
	if w.o.Bus == nil {
		return nil
	}
	now := time.Now()
	at := now
	if job.JobRow != nil && !job.CreatedAt.IsZero() {
		at = job.CreatedAt
	}
	// period matches server.usage, so consumers dedupe on (resource, period) alike.
	period := at.UTC().Truncate(servers.UsageInterval).Format(time.RFC3339)
	var after int64
	for {
		rows, err := w.o.Ent.Volume.Query().Where(volume.IDGT(after)).Order(volume.ByID()).Limit(usageBatch).All(ctx)
		if err != nil {
			return err
		}
		for _, r := range rows {
			w.o.Bus.Publish(ctx, ext.Event{Kind: ext.VolumeUsage, ProjectID: r.ProjectID, ResourceType: ResourceType, ResourceID: r.ID, At: now,
				Data: map[string]any{"size_gb": r.SizeGB, "location_id": r.LocationID, "attached": r.ServerID != nil, "period": period}})
		}
		if len(rows) < usageBatch {
			return nil
		}
		after = rows[len(rows)-1].ID
	}
}

package enroll

import (
	"context"
	"slices"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/nebla-cloud/nebla/internal/jobs"
)

const (
	KindSweep     = "enrollment_sweep"
	SweepInterval = time.Hour
)

// SweepArgs is the hourly job that deletes long-expired enrollments.
type SweepArgs struct{}

func (SweepArgs) Kind() string { return KindSweep }

// InsertOpts allow one run per hour, even across restarts.
func (SweepArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: jobs.QueueMaintenance, MaxAttempts: 1, UniqueOpts: river.UniqueOpts{ByPeriod: SweepInterval,
		ByState: append(slices.Clone(jobs.LiveStates), rivertype.JobStateCompleted)}}
}

// SweepPeriodicJob schedules the sweep every SweepInterval.
func SweepPeriodicJob() *river.PeriodicJob {
	return river.NewPeriodicJob(river.PeriodicInterval(SweepInterval),
		func() (river.JobArgs, *river.InsertOpts) { return SweepArgs{}, nil }, &river.PeriodicJobOpts{ID: KindSweep, RunOnStart: true})
}

// Register adds the sweep worker.
func Register(w *river.Workers, s *Service) { river.AddWorker(w, &sweepWorker{s: s}) }

type sweepWorker struct {
	river.WorkerDefaults[SweepArgs]
	s *Service
}

func (*sweepWorker) Timeout(*river.Job[SweepArgs]) time.Duration { return time.Minute }

func (w *sweepWorker) Work(ctx context.Context, _ *river.Job[SweepArgs]) error {
	_, err := w.s.Sweep(ctx)
	return err
}

package reconcile

import (
	"context"
	"time"

	"github.com/Hexeption/nebla/internal/jobs"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

const (
	KindAll      = "reconcile_all"
	KindLocation = "reconcile_location"
	Interval     = 30 * time.Second
	// locationTimeout bounds one location's run so a hung Proxmox only holds its own job.
	locationTimeout = 2 * time.Minute
)

// AllArgs fans out one LocationArgs job per location.
type AllArgs struct{}

func (AllArgs) Kind() string { return KindAll }

func (AllArgs) InsertOpts() river.InsertOpts {
	return maintenance(river.UniqueOpts{ByState: jobs.LiveStates})
}

// LocationArgs reconciles one location; it is unique per location while queued or running.
type LocationArgs struct {
	LocationID int64 `json:"location_id"`
}

func (LocationArgs) Kind() string { return KindLocation }

func (LocationArgs) InsertOpts() river.InsertOpts {
	return maintenance(river.UniqueOpts{ByArgs: true, ByState: jobs.LiveStates})
}

// maintenance jobs never retry; the next tick is the retry.
func maintenance(u river.UniqueOpts) river.InsertOpts {
	return river.InsertOpts{Queue: jobs.QueueMaintenance, MaxAttempts: 1, UniqueOpts: u}
}

// PeriodicJob schedules the fan-out every Interval.
func PeriodicJob() *river.PeriodicJob {
	return river.NewPeriodicJob(river.PeriodicInterval(Interval),
		func() (river.JobArgs, *river.InsertOpts) { return AllArgs{}, nil }, &river.PeriodicJobOpts{ID: KindAll, RunOnStart: true})
}

// Register adds the reconcile workers.
func Register(w *river.Workers, r *Reconciler) {
	river.AddWorker(w, &allWorker{r: r})
	river.AddWorker(w, &locationWorker{r: r})
}

type allWorker struct {
	river.WorkerDefaults[AllArgs]
	r *Reconciler
}

func (w *allWorker) Work(ctx context.Context, _ *river.Job[AllArgs]) error {
	ids, err := w.r.o.Ent.Location.Query().IDs(ctx)
	if err != nil || len(ids) == 0 {
		return err
	}
	params := make([]river.InsertManyParams, len(ids))
	for i, id := range ids {
		params[i] = river.InsertManyParams{Args: LocationArgs{LocationID: id}}
	}
	_, err = river.ClientFromContext[pgx.Tx](ctx).InsertMany(ctx, params)
	return err
}

type locationWorker struct {
	river.WorkerDefaults[LocationArgs]
	r *Reconciler
}

func (*locationWorker) Timeout(*river.Job[LocationArgs]) time.Duration { return locationTimeout }

// Work logs a failed run instead of returning it; Run already changed nothing it could not trust.
func (w *locationWorker) Work(ctx context.Context, job *river.Job[LocationArgs]) error {
	if err := w.r.Run(ctx, job.Args.LocationID); err != nil {
		w.r.o.Logger.Warn("reconcile failed", "location_id", job.Args.LocationID, "err", err)
	}
	return nil
}

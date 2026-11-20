// Package jobs runs background work on River and enqueues it atomically with domain writes.
package jobs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverdatabasesql"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertype"
)

const (
	QueueDefault = river.QueueDefault
	MaxWorkers   = 10
	// QueueMaintenance runs periodic upkeep apart from user actions.
	QueueMaintenance      = "maintenance"
	MaxMaintenanceWorkers = 4
	// RescueStuckJobsAfter must exceed every worker's Timeout.
	RescueStuckJobsAfter = 15 * time.Minute
	// SoftStopTimeout is how long Stop lets jobs finish before cancelling their contexts.
	SoftStopTimeout = 20 * time.Second
)

// Jobs holds the worker client and an insert-only client over the shared *sql.DB.
type Jobs struct {
	// Client works jobs; use it for queries like JobGet, not for inserts inside domain transactions.
	Client   *river.Client[pgx.Tx]
	inserter *river.Client[*sql.Tx]
	sql      *sql.DB
}

// LiveStates are the job states unique maintenance jobs dedupe on, so a slow run is never doubled.
var LiveStates = []rivertype.JobState{rivertype.JobStateAvailable, rivertype.JobStatePending, rivertype.JobStateRunning, rivertype.JobStateScheduled}

// PingArgs is a no-op job; River needs one worker to start, and it doubles as a liveness probe.
type PingArgs struct{}

func (PingArgs) Kind() string { return "ping" }

type pingWorker struct{ river.WorkerDefaults[PingArgs] }

func (pingWorker) Work(context.Context, *river.Job[PingArgs]) error { return nil }

type options struct {
	periodic     []*river.PeriodicJob
	pollInterval time.Duration
	softStop     time.Duration
	logger       *slog.Logger
}

type Option func(*options)

func WithPeriodicJobs(jobs ...*river.PeriodicJob) Option {
	return func(o *options) { o.periodic = append(o.periodic, jobs...) }
}

// WithPollInterval sets how often idle workers poll for jobs; tests use a short one.
func WithPollInterval(d time.Duration) Option { return func(o *options) { o.pollInterval = d } }

// WithSoftStopTimeout overrides SoftStopTimeout; tests use a short one.
func WithSoftStopTimeout(d time.Duration) Option { return func(o *options) { o.softStop = d } }

func WithLogger(l *slog.Logger) Option { return func(o *options) { o.logger = l } }

// New builds both clients; sqlDB must be the *sql.DB that db.WithTx and ent use.
func New(pool *pgxpool.Pool, sqlDB *sql.DB, workers *river.Workers, opts ...Option) (*Jobs, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	if workers == nil {
		workers = river.NewWorkers()
	}
	if o.softStop <= 0 {
		o.softStop = SoftStopTimeout
	}
	if err := river.AddWorkerSafely(workers, pingWorker{}); err != nil {
		return nil, fmt.Errorf("river workers: %w", err)
	}
	cfg := &river.Config{
		Queues:               map[string]river.QueueConfig{QueueDefault: {MaxWorkers: MaxWorkers}, QueueMaintenance: {MaxWorkers: MaxMaintenanceWorkers}},
		Workers:              workers,
		PeriodicJobs:         o.periodic,
		Logger:               o.logger,
		RescueStuckJobsAfter: RescueStuckJobsAfter,
		SoftStopTimeout:      o.softStop,
	}
	if o.pollInterval > 0 {
		cfg.FetchPollInterval = o.pollInterval
		cfg.FetchCooldown = min(o.pollInterval, river.FetchCooldownDefault)
	}
	client, err := river.NewClient(riverpgxv5.New(pool), cfg)
	if err != nil {
		return nil, fmt.Errorf("river client: %w", err)
	}
	inserter, err := river.NewClient(riverdatabasesql.New(sqlDB), &river.Config{Logger: o.logger})
	if err != nil {
		return nil, fmt.Errorf("river inserter: %w", err)
	}
	return &Jobs{Client: client, inserter: inserter, sql: sqlDB}, nil
}

// InsertTx enqueues a job in the caller's transaction so it commits or rolls back with it.
func (j *Jobs) InsertTx(ctx context.Context, tx *sql.Tx, args river.JobArgs, opts *river.InsertOpts) (*rivertype.JobInsertResult, error) {
	return j.inserter.InsertTx(ctx, tx, args, opts)
}

// StartAction creates a running action and its job in tx; workers must Succeed or Fail it before returning nil.
func (j *Jobs) StartAction(ctx context.Context, tx *sql.Tx, etx *ent.Client, projectID int64, command string,
	resources []actions.Resource, args func(actionID int64) river.JobArgs, opts *river.InsertOpts) (actions.Action, error) {
	a, err := actions.Create(ctx, etx, projectID, command, resources)
	if err != nil {
		return actions.Action{}, err
	}
	res, err := j.InsertTx(ctx, tx, args(a.ID), opts)
	if err != nil {
		return actions.Action{}, err
	}
	if err := actions.AttachJob(ctx, etx, a.ID, res.Job.ID); err != nil {
		return actions.Action{}, err
	}
	a.RiverJobID = &res.Job.ID
	return a, nil
}

func (j *Jobs) Start(ctx context.Context) error { return j.Client.Start(ctx) }

// stopCancelWait bounds the hard stop that follows a Stop that ran out of time.
const stopCancelWait = 5 * time.Second

// Stop lets jobs finish, cancels them after SoftStopTimeout, and hard-cancels if ctx runs out first.
func (j *Jobs) Stop(ctx context.Context) error {
	err := j.Client.Stop(ctx)
	if err == nil || ctx.Err() == nil {
		return err
	}
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), stopCancelWait)
	defer cancel()
	return errors.Join(err, j.Client.StopAndCancel(cctx))
}

const (
	OrphanedCode    = actions.CodeOrphaned
	OrphanedMessage = "the job running this action was lost; retry the request"
	// Orphaned is a SQL condition on actions alias a: running, with no job left that can finish it.
	Orphaned = `a.status = 'running' and a.finished is null and not exists (
		select 1 from river_job j where j.id = a.river_job_id
		and j.state not in ('discarded', 'cancelled', 'completed'))`
)

// ServerResource is the servers resource type; reconcile rescues those actions, so the sweep skips them.
const ServerResource = "server"

// FailOrphanedActions marks orphaned actions without a server resource as error/orphaned.
func (j *Jobs) FailOrphanedActions(ctx context.Context) (int, error) {
	res, err := j.sql.ExecContext(ctx, `
		update actions a set status = 'error', error_code = $1, error_message = $2, finished = now()
		where not a.resources @> jsonb_build_array(jsonb_build_object('type', $3::text)) and `+Orphaned,
		OrphanedCode, OrphanedMessage, ServerResource)
	if err != nil {
		return 0, fmt.Errorf("fail orphaned actions: %w", err)
	}
	n, err := res.RowsAffected()
	return int(n), err
}

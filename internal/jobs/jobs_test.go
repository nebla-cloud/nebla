package jobs_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/Hexeption/nebla/internal/actions"
	"github.com/Hexeption/nebla/internal/db"
	"github.com/Hexeption/nebla/internal/ent"
	"github.com/Hexeption/nebla/internal/jobs"
	"github.com/Hexeption/nebla/internal/testutil"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/stretchr/testify/require"
)

type noopArgs struct {
	ActionID int64 `json:"action_id"`
}

func (noopArgs) Kind() string { return "test_noop" }

type noopWorker struct {
	river.WorkerDefaults[noopArgs]
	ent *ent.Client
}

func (w *noopWorker) Work(ctx context.Context, job *river.Job[noopArgs]) error {
	if err := actions.Progress(ctx, w.ent, job.Args.ActionID, 50); err != nil {
		return err
	}
	return actions.Succeed(ctx, w.ent, job.Args.ActionID)
}

type env struct {
	pool *pgxpool.Pool
	sql  *sql.DB
	ent  *ent.Client
	jobs *jobs.Jobs
	pid  int64
}

func setup(t *testing.T) env {
	t.Helper()
	pool := testutil.NewDB(t)
	client, sqlDB := db.NewEnt(pool)
	t.Cleanup(func() { client.Close() })
	workers := river.NewWorkers()
	river.AddWorker(workers, &noopWorker{ent: client})
	j, err := jobs.New(pool, sqlDB, workers, jobs.WithPollInterval(50*time.Millisecond))
	require.NoError(t, err)
	pid := client.Project.Create().SetName("p").SaveX(context.Background()).ID
	return env{pool: pool, sql: sqlDB, ent: client, jobs: j, pid: pid}
}

func (e env) count(t *testing.T, table string) int {
	t.Helper()
	var n int
	require.NoError(t, e.pool.QueryRow(context.Background(), "select count(*) from "+table).Scan(&n))
	return n
}

func (e env) start(t *testing.T, ctx context.Context) actions.Action {
	t.Helper()
	var a actions.Action
	require.NoError(t, db.WithTx(ctx, e.sql, func(tx *sql.Tx, etx *ent.Client) error {
		var err error
		a, err = e.jobs.StartAction(ctx, tx, etx, e.pid, "test", nil,
			func(id int64) river.JobArgs { return noopArgs{ActionID: id} }, nil)
		return err
	}))
	return a
}

func TestActionAndJobCommitTogether(t *testing.T) {
	ctx := context.Background()
	e := setup(t)

	boom := errors.New("boom")
	err := db.WithTx(ctx, e.sql, func(tx *sql.Tx, etx *ent.Client) error {
		a, err := actions.Create(ctx, etx, e.pid, "test", nil)
		if err != nil {
			return err
		}
		res, err := e.jobs.InsertTx(ctx, tx, noopArgs{ActionID: a.ID}, nil)
		if err != nil {
			return err
		}
		if err := actions.AttachJob(ctx, etx, a.ID, res.Job.ID); err != nil {
			return err
		}
		return boom
	})
	require.ErrorIs(t, err, boom)
	require.Zero(t, e.count(t, "actions"))
	require.Zero(t, e.count(t, "river_job"))

	a := e.start(t, ctx)
	require.Equal(t, 1, e.count(t, "actions"))
	require.Equal(t, 1, e.count(t, "river_job"))
	require.NotNil(t, a.RiverJobID)
	var kind string
	require.NoError(t, e.pool.QueryRow(ctx, "select kind from river_job where id = $1", *a.RiverJobID).Scan(&kind))
	require.Equal(t, "test_noop", kind)
}

func TestWorkerRunsAction(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	require.NoError(t, e.jobs.Start(ctx))
	t.Cleanup(func() {
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = e.jobs.Stop(sctx)
	})
	a := e.start(t, ctx)
	got := testutil.WaitAction(t, e.ent, a.ID)
	require.Equal(t, actions.StatusSuccess, got.Status)
	require.Equal(t, 100, got.Progress)
}

func TestWorkJobRunsSynchronously(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	a, err := actions.Create(ctx, e.ent, e.pid, "test", nil)
	require.NoError(t, err)
	res, err := testutil.WorkJob(t, e.pool, &noopWorker{ent: e.ent}, noopArgs{ActionID: a.ID})
	require.NoError(t, err)
	require.Equal(t, river.EventKindJobCompleted, res.EventKind)
	got, err := actions.Get(ctx, e.ent, e.pid, a.ID)
	require.NoError(t, err)
	require.Equal(t, actions.StatusSuccess, got.Status)
}

func TestOrphanedActionFailed(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	setState := func(jobID int64, state string) {
		_, err := e.pool.Exec(ctx, "update river_job set state = $2::river_job_state, finalized_at = case when $2::text in ('discarded','cancelled','completed') then now() end where id = $1",
			jobID, state)
		require.NoError(t, err)
	}

	noJob, err := actions.Create(ctx, e.ent, e.pid, "test", nil)
	require.NoError(t, err)
	gone, err := actions.Create(ctx, e.ent, e.pid, "test", nil)
	require.NoError(t, err)
	require.NoError(t, actions.AttachJob(ctx, e.ent, gone.ID, 987654))
	discarded, cancelled, running, available := e.start(t, ctx), e.start(t, ctx), e.start(t, ctx), e.start(t, ctx)
	setState(*discarded.RiverJobID, "discarded")
	setState(*cancelled.RiverJobID, "cancelled")
	setState(*running.RiverJobID, "running")
	completed := e.start(t, ctx)
	setState(*completed.RiverJobID, "completed")
	done := e.start(t, ctx)
	require.NoError(t, actions.Succeed(ctx, e.ent, done.ID))
	setState(*done.RiverJobID, "discarded")

	n, err := e.jobs.FailOrphanedActions(ctx)
	require.NoError(t, err)
	require.Equal(t, 5, n)

	for _, a := range []actions.Action{noJob, gone, discarded, cancelled, completed} {
		got, err := actions.Get(ctx, e.ent, e.pid, a.ID)
		require.NoError(t, err)
		require.Equal(t, actions.StatusError, got.Status)
		require.NotNil(t, got.Finished)
		require.Equal(t, "orphaned", got.Error.Code)
		require.NotEmpty(t, got.Error.Message)
	}
	for _, a := range []actions.Action{running, available} {
		got, err := actions.Get(ctx, e.ent, e.pid, a.ID)
		require.NoError(t, err)
		require.Equal(t, actions.StatusRunning, got.Status)
	}
	got, err := actions.Get(ctx, e.ent, e.pid, done.ID)
	require.NoError(t, err)
	require.Equal(t, actions.StatusSuccess, got.Status)

	n, err = e.jobs.FailOrphanedActions(ctx)
	require.NoError(t, err)
	require.Zero(t, n)
}

func TestStartsWithNoWorkersAndRunsPing(t *testing.T) {
	ctx := context.Background()
	pool := testutil.NewDB(t)
	client, sqlDB := db.NewEnt(pool)
	t.Cleanup(func() { client.Close() })
	j := testutil.StartJobs(t, pool, sqlDB, river.NewWorkers())
	var id int64
	require.NoError(t, db.WithTx(ctx, sqlDB, func(tx *sql.Tx, _ *ent.Client) error {
		res, err := j.InsertTx(ctx, tx, jobs.PingArgs{}, nil)
		if err != nil {
			return err
		}
		id = res.Job.ID
		return nil
	}))
	row := testutil.WaitJob(t, j, id)
	require.Equal(t, "completed", string(row.State))
}

type blockArgs struct{}

func (blockArgs) Kind() string { return "test_block" }

type blockWorker struct {
	river.WorkerDefaults[blockArgs]
	started, cancelled chan struct{}
}

func (w *blockWorker) Work(ctx context.Context, _ *river.Job[blockArgs]) error {
	close(w.started)
	<-ctx.Done()
	close(w.cancelled)
	return ctx.Err()
}

// TestStopCancelsRunningJobs checks a soft stop cancels job contexts instead of waiting out the stop deadline.
func TestStopCancelsRunningJobs(t *testing.T) {
	ctx := context.Background()
	pool := testutil.NewDB(t)
	_, sqlDB := db.NewEnt(pool)
	w := &blockWorker{started: make(chan struct{}), cancelled: make(chan struct{})}
	workers := river.NewWorkers()
	river.AddWorker(workers, w)
	j, err := jobs.New(pool, sqlDB, workers, jobs.WithPollInterval(50*time.Millisecond), jobs.WithSoftStopTimeout(100*time.Millisecond))
	require.NoError(t, err)
	require.NoError(t, j.Start(ctx))
	_, err = j.Client.Insert(ctx, blockArgs{}, nil)
	require.NoError(t, err)
	select {
	case <-w.started:
	case <-time.After(10 * time.Second):
		t.Fatal("job never started")
	}
	sctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	start := time.Now()
	require.NoError(t, j.Stop(sctx))
	require.Less(t, time.Since(start), 3*time.Second)
	select {
	case <-w.cancelled:
	default:
		t.Fatal("job context not cancelled")
	}
}

func TestDefaultSoftStopTimeout(t *testing.T) {
	require.Equal(t, 20*time.Second, jobs.SoftStopTimeout)
}

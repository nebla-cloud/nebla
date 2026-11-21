package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nebla-cloud/nebla/internal/auth"
	"github.com/nebla-cloud/nebla/internal/catalog"
	"github.com/nebla-cloud/nebla/internal/config"
	"github.com/nebla-cloud/nebla/internal/db"
	"github.com/nebla-cloud/nebla/internal/enroll"
	"github.com/nebla-cloud/nebla/internal/ext"
	"github.com/nebla-cloud/nebla/internal/httpapi"
	"github.com/nebla-cloud/nebla/internal/jobs"
	"github.com/nebla-cloud/nebla/internal/mail"
	"github.com/nebla-cloud/nebla/internal/osimages"
	"github.com/nebla-cloud/nebla/internal/projects"
	"github.com/nebla-cloud/nebla/internal/reconcile"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/nebla-cloud/nebla/internal/settings"
	"github.com/nebla-cloud/nebla/internal/volumes"
	"github.com/riverqueue/river"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck())
	}
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := db.Migrate(ctx, pool); err != nil {
		return err
	}
	sender, err := mail.New(cfg.SMTPURL, cfg.BaseURL)
	if err != nil {
		return err
	}
	client, sqlDB := db.NewEnt(pool)
	defer client.Close()
	log := slog.Default()
	exts := ext.Defaults(settings.New(client))
	bus := ext.NewBus(exts.BillingSink)
	projectHooks, catalogHooks := httpapi.ServerHooks(projects.Hooks{}, catalog.Hooks{})
	cat := catalog.New(sqlDB, client, catalog.Options{SecretKey: cfg.SecretKey, Hooks: catalogHooks})
	osi := osimages.New(osimages.Options{SQL: sqlDB, Ent: client, Catalog: cat, Logger: log})
	workers := river.NewWorkers()
	osimages.Register(workers, osi)
	servers.RegisterWorkers(workers, servers.WorkerOptions{SQL: sqlDB, Ent: client, Catalog: cat, Bus: bus, Logger: log})
	volumes.RegisterWorkers(workers, volumes.WorkerOptions{SQL: sqlDB, Ent: client, Catalog: cat, Bus: bus, Logger: log})
	rec := reconcile.New(reconcile.Options{SQL: sqlDB, Ent: client, Catalog: cat, Logger: log,
		VolumeRescue: func(ctx context.Context) error {
			_, err := volumes.RescueOrphaned(ctx, client, sqlDB, bus)
			return err
		}})
	reconcile.Register(workers, rec)
	enr := enroll.New(sqlDB, client, enroll.Options{SecretKey: cfg.SecretKey, BaseURL: cfg.BaseURL, Catalog: cat, Logger: log})
	enroll.Register(workers, enr)
	passkeys, err := auth.NewPasskeys(client, auth.New(client, exts.Registration), cfg.PasskeyOrigin())
	if err != nil {
		return err
	}
	auth.RegisterPasskeyJobs(workers, passkeys)
	runner, err := jobs.New(pool, sqlDB, workers, jobs.WithLogger(log),
		jobs.WithPeriodicJobs(reconcile.PeriodicJob(), servers.UsagePeriodicJob(), volumes.UsagePeriodicJob(), enroll.SweepPeriodicJob(),
			auth.ChallengeCleanupPeriodicJob()))
	if err != nil {
		return err
	}
	osi.SetJobs(runner)
	rec.SetJobs(runner)
	// Server actions get the DB-only rescue so their rows are repaired; the sweep catches the rest.
	if err := rec.RescueAll(ctx); err != nil {
		return err
	}
	rescued, err := volumes.RescueOrphaned(ctx, client, sqlDB, bus)
	if err != nil {
		return err
	}
	if rescued > 0 {
		log.Warn("failed orphaned volume actions", "count", rescued)
	}
	if _, err := projects.BackfillCreatedBy(ctx, client); err != nil {
		return err
	}
	filled, err := projects.BackfillDefaultQuota(ctx, client, cfg.DefaultQuota)
	if err != nil {
		return err
	}
	if filled > 0 {
		log.Info("backfilled default project quota", "count", filled)
	}
	filledVol, err := projects.BackfillVolumeQuota(ctx, client, cfg.DefaultQuota)
	if err != nil {
		return err
	}
	if filledVol > 0 {
		log.Info("backfilled volume project quota", "count", filledVol)
	}
	n, err := runner.FailOrphanedActions(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		log.Warn("failed orphaned actions", "count", n)
	}
	// River gets its own context so a signal stops it via Stop, not by cancelling Start's context.
	if err := runner.Start(context.WithoutCancel(ctx)); err != nil {
		bus.Close()
		return err
	}
	srv := &http.Server{
		Addr: cfg.Listen,
		Handler: httpapi.New(httpapi.Deps{Pool: pool, SQL: sqlDB, Ent: client, Config: cfg, Logger: log, Mail: sender, Jobs: runner,
			Ext: exts, Bus: bus, Catalog: cat, OSImages: osi, Enroll: enr, Passkeys: passkeys, CatalogHooks: catalogHooks, ProjectHooks: projectHooks}),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       120 * time.Second,
		// No WriteTimeout: it would cut console websockets.
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	log.Info("listening", "addr", cfg.Listen)
	var serveErr error
	select {
	case serveErr = <-errc:
	case <-ctx.Done():
		log.Info("shutting down")
	}
	shutdown(log, srv, runner, bus)
	if errors.Is(serveErr, http.ErrServerClosed) {
		serveErr = nil
	}
	return serveErr
}

// shutdown drains HTTP, then River, then pending event deliveries; the DB closes after via defers.
func shutdown(log *slog.Logger, srv *http.Server, runner *jobs.Jobs, bus *ext.Bus) {
	hctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(hctx); err != nil {
		log.Error("stop http", "err", err)
	}
	jctx, jcancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer jcancel()
	if err := runner.Stop(jctx); err != nil {
		log.Error("stop jobs", "err", err)
	}
	bus.Close()
}

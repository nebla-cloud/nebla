package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Hexeption/nebla/internal/catalog"
	"github.com/Hexeption/nebla/internal/config"
	"github.com/Hexeption/nebla/internal/db"
	"github.com/Hexeption/nebla/internal/ext"
	"github.com/Hexeption/nebla/internal/httpapi"
	"github.com/Hexeption/nebla/internal/jobs"
	"github.com/Hexeption/nebla/internal/mail"
	"github.com/Hexeption/nebla/internal/projects"
	"github.com/Hexeption/nebla/internal/reconcile"
	"github.com/Hexeption/nebla/internal/servers"
	"github.com/Hexeption/nebla/internal/settings"
	"github.com/riverqueue/river"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()
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
	defer bus.Close()
	projectHooks, catalogHooks := httpapi.ServerHooks(projects.Hooks{}, catalog.Hooks{})
	cat := catalog.New(sqlDB, client, catalog.Options{SecretKey: cfg.SecretKey, Hooks: catalogHooks})
	workers := river.NewWorkers()
	servers.RegisterWorkers(workers, servers.WorkerOptions{SQL: sqlDB, Ent: client, Catalog: cat, Bus: bus, Logger: log})
	reconcile.Register(workers, reconcile.New(reconcile.Options{SQL: sqlDB, Ent: client, Catalog: cat, Logger: log}))
	runner, err := jobs.New(pool, sqlDB, workers, jobs.WithLogger(log),
		jobs.WithPeriodicJobs(reconcile.PeriodicJob(), servers.UsagePeriodicJob()))
	if err != nil {
		return err
	}
	n, err := runner.FailOrphanedActions(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		log.Warn("failed orphaned actions", "count", n)
	}
	if err := runner.Start(ctx); err != nil {
		return err
	}
	defer func() {
		sctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := runner.Stop(sctx); err != nil {
			log.Error("stop jobs", "err", err)
		}
	}()
	srv := &http.Server{
		Addr: cfg.Listen,
		Handler: httpapi.New(httpapi.Deps{Pool: pool, SQL: sqlDB, Ent: client, Config: cfg, Logger: log, Mail: sender, Jobs: runner,
			Ext: exts, Bus: bus, Catalog: cat, CatalogHooks: catalogHooks, ProjectHooks: projectHooks}),
		ReadHeaderTimeout: 10 * time.Second,
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	log.Info("listening", "addr", cfg.Listen)
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	select {
	case err := <-errc:
		return err
	case <-sig:
		log.Info("shutting down")
		return srv.Close()
	}
}

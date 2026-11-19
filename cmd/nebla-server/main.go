package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/Hexeption/nebla/internal/config"
	"github.com/Hexeption/nebla/internal/db"
	"github.com/Hexeption/nebla/internal/httpapi"
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
	log := slog.Default()
	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           httpapi.New(httpapi.Deps{Pool: pool, Config: cfg, Logger: log}),
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Info("listening", "addr", cfg.Listen)
	return srv.ListenAndServe()
}

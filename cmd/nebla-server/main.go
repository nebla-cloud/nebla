package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"

	"github.com/Hexeption/nebla/internal/config"
	"github.com/Hexeption/nebla/internal/db"
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
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	slog.Info("listening", "addr", cfg.Listen)
	return http.ListenAndServe(cfg.Listen, mux)
}

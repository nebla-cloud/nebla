package testutil

import (
	"context"
	"database/sql"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nebla-cloud/nebla/internal/db"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

// NewDB starts a throwaway Postgres 17, migrates it, and returns a pool.
func NewDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	ctr, err := postgres.Run(ctx, "postgres:17",
		postgres.WithDatabase("nebla"),
		postgres.WithUsername("nebla"),
		postgres.WithPassword("nebla"),
		// Waits for the second ready log and the mapped port; replacing it reintroduces "port not found".
		postgres.BasicWaitStrategies(),
	)
	testcontainers.CleanupContainer(t, ctr)
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}
	url, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}
	pool, err := db.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	return pool
}

// NewEnt starts a migrated Postgres like NewDB and returns an ent client over it.
func NewEnt(t *testing.T) *ent.Client {
	t.Helper()
	c, _ := entFor(t, NewDB(t))
	return c
}

func entFor(t *testing.T, pool *pgxpool.Pool) (*ent.Client, *sql.DB) {
	c, sqlDB := db.NewEnt(pool)
	t.Cleanup(func() { c.Close() })
	return c, sqlDB
}

// NewEntSQL is NewEnt plus the *sql.DB the client shares, for db.WithTx.
func NewEntSQL(t *testing.T) (*ent.Client, *sql.DB) {
	t.Helper()
	return entFor(t, NewDB(t))
}

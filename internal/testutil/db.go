package testutil

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nebla-cloud/nebla/internal/db"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

// templateDB is the migrated database each test's database is cloned from.
const templateDB = "nebla"

// pgServer is one Postgres container per test binary; Ryuk removes it when the binary exits.
var pgServer struct {
	once  sync.Once
	admin *url.URL // connects to the postgres maintenance database
	err   error
	mu    sync.Mutex // serializes CREATE DATABASE from the template
	next  atomic.Int64
}

func startServer() (*url.URL, error) {
	ctx := context.Background()
	ctr, err := postgres.Run(ctx, "postgres:17",
		postgres.WithDatabase(templateDB),
		postgres.WithUsername("nebla"),
		postgres.WithPassword("nebla"),
		// Waits for the second ready log and the mapped port; replacing it reintroduces "port not found".
		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		return nil, fmt.Errorf("start postgres: %w", err)
	}
	raw, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return nil, fmt.Errorf("connection string: %w", err)
	}
	pool, err := db.Open(ctx, raw)
	if err != nil {
		return nil, err
	}
	err = db.Migrate(ctx, pool)
	// A template must have no open connections while it is cloned.
	pool.Close()
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	u.Path = "/postgres"
	return u, nil
}

// NewDB returns a pool on a fresh migrated database, cloned from a template on the binary's shared Postgres 17.
func NewDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	pgServer.once.Do(func() { pgServer.admin, pgServer.err = startServer() })
	if pgServer.err != nil {
		t.Fatal(pgServer.err)
	}
	name := fmt.Sprintf("test_%d", pgServer.next.Add(1))
	admin, err := pgx.Connect(ctx, pgServer.admin.String())
	if err != nil {
		t.Fatalf("connect postgres: %v", err)
	}
	defer admin.Close(ctx)
	pgServer.mu.Lock()
	_, err = admin.Exec(ctx, fmt.Sprintf("create database %s template %s", name, templateDB))
	pgServer.mu.Unlock()
	if err != nil {
		t.Fatalf("create database: %v", err)
	}
	t.Cleanup(func() {
		c, err := pgx.Connect(ctx, pgServer.admin.String())
		if err != nil {
			t.Errorf("drop database %s: %v", name, err)
			return
		}
		defer c.Close(ctx)
		if _, err := c.Exec(ctx, fmt.Sprintf("drop database %s with (force)", name)); err != nil {
			t.Errorf("drop database %s: %v", name, err)
		}
	})
	u := *pgServer.admin
	u.Path = "/" + name
	pool, err := db.Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
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

package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/Hexeption/nebla/internal/ent"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
)

//go:embed migrations/*.sql
var migrations embed.FS

func Open(ctx context.Context, url string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return pool, nil
}

// SQL wraps the pool as a *sql.DB; closing it leaves the pool open.
func SQL(pool *pgxpool.Pool) *sql.DB { return stdlib.OpenDBFromPool(pool) }

// NewEnt returns an ent client and the one *sql.DB it shares with WithTx.
func NewEnt(pool *pgxpool.Pool) (*ent.Client, *sql.DB) {
	sqlDB := SQL(pool)
	return EntFromSQL(sqlDB), sqlDB
}

func EntFromSQL(sqlDB *sql.DB) *ent.Client {
	return ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, sqlDB)))
}

// WithTx runs fn in one *sql.Tx with an ent client bound to it; any exit but a commit rolls back.
func WithTx(ctx context.Context, sqlDB *sql.DB, fn func(tx *sql.Tx, etx *ent.Client) error) error {
	tx, err := sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	// Runs on error returns, panics (which keep propagating) and runtime.Goexit alike.
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	// ent's own Tx over the joined driver: builders join it and a nested Tx() fails with ent.ErrTxStarted.
	entTx, err := ent.NewClient(ent.Driver(joinDriver{entsql.NewDriver(dialect.Postgres, entsql.Conn{ExecQuerier: tx})})).Tx(ctx)
	if err != nil {
		return err
	}
	if err := fn(tx, entTx.Client()); err != nil {
		_ = entTx.Rollback()
		return err
	}
	if err := entTx.Commit(); err != nil {
		return err
	}
	committed = true
	return tx.Commit()
}

// joinDriver runs ent on an open *sql.Tx; its Tx joins it with a no-op commit and rollback.
type joinDriver struct{ *entsql.Driver }

func (d joinDriver) Tx(context.Context) (dialect.Tx, error) {
	return joinTx{dialect.NopTx(d), d.Driver}, nil
}

// joinTx keeps raw ExecContext and QueryContext working inside WithTx.
type joinTx struct {
	dialect.Tx
	d *entsql.Driver
}

func (t joinTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return t.d.ExecContext(ctx, query, args...)
}

func (t joinTx) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return t.d.QueryContext(ctx, query, args...)
}

func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	sqlDB := SQL(pool)
	defer sqlDB.Close()
	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, mustSub())
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	// River owns its tables; they stay out of the Atlas migrations.
	rm, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		return fmt.Errorf("migrate river: %w", err)
	}
	if _, err := rm.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		return fmt.Errorf("migrate river: %w", err)
	}
	return nil
}

func mustSub() fs.FS {
	sub, err := fs.Sub(migrations, "migrations")
	if err != nil {
		panic(err)
	}
	return sub
}

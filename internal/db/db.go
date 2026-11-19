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

// WithTx runs fn in one *sql.Tx with an ent client bound to it; fn's error rolls back.
func WithTx(ctx context.Context, sqlDB *sql.DB, fn func(tx *sql.Tx, etx *ent.Client) error) error {
	tx, err := sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
	}()
	etx := ent.NewClient(ent.Driver(txDriver{entsql.NewDriver(dialect.Postgres, entsql.Conn{ExecQuerier: tx})}))
	if err := fn(tx, etx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// txDriver binds ent to an open *sql.Tx; ent's own Tx calls (UpdateOne, edges) join it.
type txDriver struct{ *entsql.Driver }

func (d txDriver) Tx(context.Context) (dialect.Tx, error) { return dialect.NopTx(d), nil }

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
	return nil
}

func mustSub() fs.FS {
	sub, err := fs.Sub(migrations, "migrations")
	if err != nil {
		panic(err)
	}
	return sub
}

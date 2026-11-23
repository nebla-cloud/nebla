// Copyright (C) 2026 The Nebla contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package db_test

import (
	"context"
	"database/sql"
	"errors"
	"runtime"
	"testing"
	"time"

	"github.com/nebla-cloud/nebla/internal/db"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/backup"
	"github.com/nebla-cloud/nebla/internal/ent/setting"
	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestMigrateIsIdempotent(t *testing.T) {
	pool := testutil.NewDB(t)
	require.NoError(t, db.Migrate(context.Background(), pool))
	var n int
	require.NoError(t, pool.QueryRow(context.Background(), "select count(*) from users").Scan(&n))
}

func TestWithTxSharesOneTransaction(t *testing.T) {
	ctx := context.Background()
	pool := testutil.NewDB(t)
	client, sqlDB := db.NewEnt(pool)
	t.Cleanup(func() { client.Close() })

	write := func(suffix string) func(*sql.Tx, *ent.Client) error {
		return func(tx *sql.Tx, etx *ent.Client) error {
			if err := etx.Setting.Create().SetID("ent_" + suffix).SetValue("1").Exec(ctx); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, "insert into settings (key, value) values ($1, '1')", "raw_"+suffix)
			return err
		}
	}
	keys := func() []string {
		return client.Setting.Query().Order(setting.ByID()).IDsX(ctx)
	}

	require.NoError(t, db.WithTx(ctx, sqlDB, write("ok")))
	require.Equal(t, []string{"ent_ok", "raw_ok"}, keys())

	boom := errors.New("boom")
	err := db.WithTx(ctx, sqlDB, func(tx *sql.Tx, etx *ent.Client) error {
		require.NoError(t, write("bad")(tx, etx))
		return boom
	})
	require.ErrorIs(t, err, boom)
	require.Equal(t, []string{"ent_ok", "raw_ok"}, keys())
}

func TestWithTxSupportsUpdateOne(t *testing.T) {
	ctx := context.Background()
	pool := testutil.NewDB(t)
	client, sqlDB := db.NewEnt(pool)
	t.Cleanup(func() { client.Close() })
	require.NoError(t, client.Setting.Create().SetID("k").SetValue("1").Exec(ctx))

	err := db.WithTx(ctx, sqlDB, func(_ *sql.Tx, etx *ent.Client) error {
		return etx.Setting.UpdateOneID("k").SetValue("2").Exec(ctx)
	})
	require.NoError(t, err)
	require.Equal(t, "2", client.Setting.GetX(ctx, "k").Value)
}

func TestWithTxNestedTxFails(t *testing.T) {
	ctx := context.Background()
	pool := testutil.NewDB(t)
	client, sqlDB := db.NewEnt(pool)
	t.Cleanup(func() { client.Close() })

	err := db.WithTx(ctx, sqlDB, func(_ *sql.Tx, etx *ent.Client) error {
		_, err := etx.Tx(ctx)
		return err
	})
	require.ErrorIs(t, err, ent.ErrTxStarted)
}

func TestWithTxEdgeWritesShareTx(t *testing.T) {
	ctx := context.Background()
	pool := testutil.NewDB(t)
	client, sqlDB := db.NewEnt(pool)
	t.Cleanup(func() { client.Close() })
	loc := client.Location.Create().SetName("eu-1").SetPveURL("https://pve:8006").SetTokenID("a@pam!b").
		SetTokenSecret([]byte{1}).SetDiskStorage("ceph").SetIsoStorage("local").SaveX(ctx)

	write := func(name string, fail error) error {
		return db.WithTx(ctx, sqlDB, func(tx *sql.Tx, etx *ent.Client) error {
			st, err := etx.ServerType.Create().SetName(name).SetCores(1).SetMemoryMB(512).SetDiskGB(5).AddLocationIDs(loc.ID).Save(ctx)
			if err != nil {
				return err
			}
			if err := etx.ServerType.UpdateOne(st).SetDeprecated(true).ClearLocations().AddLocationIDs(loc.ID).Exec(ctx); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "insert into settings (key, value) values ($1, '1')", name); err != nil {
				return err
			}
			return fail
		})
	}
	boom := errors.New("boom")
	require.ErrorIs(t, write("rolled", boom), boom)
	require.Zero(t, client.ServerType.Query().CountX(ctx))
	require.Zero(t, client.Setting.Query().CountX(ctx))

	require.NoError(t, write("kept", nil))
	st := client.ServerType.Query().WithLocations().OnlyX(ctx)
	require.True(t, st.Deprecated)
	require.Len(t, st.Edges.Locations, 1)
	require.Equal(t, []string{"kept"}, client.Setting.Query().IDsX(ctx))
}

func TestWithTxRollsBackOnGoexitAndPanic(t *testing.T) {
	ctx := context.Background()
	pool := testutil.NewDB(t)
	client, sqlDB := db.NewEnt(pool)
	t.Cleanup(func() { client.Close() })
	require.NoError(t, client.Setting.Create().SetID("k").SetValue("1").Exec(ctx))
	lock := func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "select 1 from settings where key = 'k' for update")
		return err
	}

	locked := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = db.WithTx(ctx, sqlDB, func(tx *sql.Tx, _ *ent.Client) error {
			locked <- lock(ctx, tx)
			runtime.Goexit()
			return nil
		})
	}()
	require.NoError(t, <-locked)
	<-done

	tctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	require.NoError(t, db.WithTx(tctx, sqlDB, func(tx *sql.Tx, _ *ent.Client) error { return lock(tctx, tx) }),
		"a Goexit inside WithTx must release the row lock")

	require.PanicsWithValue(t, "boom", func() {
		_ = db.WithTx(ctx, sqlDB, func(tx *sql.Tx, etx *ent.Client) error {
			if err := lock(ctx, tx); err != nil {
				return err
			}
			etx.Setting.UpdateOneID("k").SetValue("2").ExecX(ctx)
			panic("boom")
		})
	})
	require.Equal(t, "1", client.Setting.GetX(ctx, "k").Value, "panics roll back")
	tctx2, cancel2 := context.WithTimeout(ctx, 3*time.Second)
	defer cancel2()
	require.NoError(t, db.WithTx(tctx2, sqlDB, func(tx *sql.Tx, _ *ent.Client) error { return lock(tctx2, tx) }))
}

func TestMigrateCreatesRiverSchema(t *testing.T) {
	pool := testutil.NewDB(t)
	var n int
	require.NoError(t, pool.QueryRow(context.Background(), "select count(*) from river_job").Scan(&n))
	require.Zero(t, n)
}

func TestWithTxSupportsRawQuery(t *testing.T) {
	ctx := context.Background()
	pool := testutil.NewDB(t)
	client, sqlDB := db.NewEnt(pool)
	t.Cleanup(func() { client.Close() })

	var n int
	err := db.WithTx(ctx, sqlDB, func(_ *sql.Tx, etx *ent.Client) error {
		if _, err := etx.ExecContext(ctx, "insert into settings (key, value) values ('raw', '1')"); err != nil {
			return err
		}
		rows, err := etx.QueryContext(ctx, "select count(*) from settings where key = 'raw'")
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			if err := rows.Scan(&n); err != nil {
				return err
			}
		}
		return rows.Err()
	})
	require.NoError(t, err)
	require.Equal(t, 1, n)
}

func TestBackupPendingUnique(t *testing.T) {
	ctx := context.Background()
	app := testutil.NewApp(t)
	e := testutil.NewServerEnv(app)
	s := e.RunningServer("web")
	add := func(status backup.Status) error {
		return app.Ent.Backup.Create().SetProjectID(e.Project).SetServerID(s.ServerID).SetLocationID(e.Location).
			SetStorage("pbs").SetNode("pve1").SetKind(backup.KindManual).SetStatus(status).SetInstanceGeneration(1).Exec(ctx)
	}
	require.NoError(t, add(backup.StatusQueued))
	require.Error(t, add(backup.StatusQueued), "a second pending backup for the server must fail")
	require.Error(t, add(backup.StatusCreating))
	require.NoError(t, add(backup.StatusAvailable))
	require.NoError(t, add(backup.StatusAvailable))
}

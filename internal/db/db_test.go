package db_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/Hexeption/nebla/internal/db"
	"github.com/Hexeption/nebla/internal/ent"
	"github.com/Hexeption/nebla/internal/ent/setting"
	"github.com/Hexeption/nebla/internal/testutil"
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

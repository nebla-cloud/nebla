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

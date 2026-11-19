package db_test

import (
	"context"
	"testing"

	"github.com/Hexeption/nebla/internal/db"
	"github.com/Hexeption/nebla/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestMigrateIsIdempotent(t *testing.T) {
	pool := testutil.NewDB(t)
	require.NoError(t, db.Migrate(context.Background(), pool))
	var n int
	require.NoError(t, pool.QueryRow(context.Background(), "select count(*) from schema_info").Scan(&n))
}

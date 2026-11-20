package testutil

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewDBIsolatedAndMigrated(t *testing.T) {
	ctx := context.Background()
	a, b := NewDB(t), NewDB(t)
	var nameA, nameB string
	require.NoError(t, a.QueryRow(ctx, "select current_database()").Scan(&nameA))
	require.NoError(t, b.QueryRow(ctx, "select current_database()").Scan(&nameB))
	require.NotEqual(t, nameA, nameB)

	_, err := a.Exec(ctx, "insert into users (email, name, password_hash) values ('a@example.com', 'a', 'x')")
	require.NoError(t, err)
	var n int
	require.NoError(t, b.QueryRow(ctx, "select count(*) from users").Scan(&n))
	require.Zero(t, n)
	require.NoError(t, b.QueryRow(ctx, "select count(*) from river_job").Scan(&n))
	require.Zero(t, n)
}

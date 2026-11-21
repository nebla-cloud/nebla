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

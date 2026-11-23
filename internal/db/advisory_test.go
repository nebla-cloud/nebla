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
	"testing"

	"github.com/nebla-cloud/nebla/internal/db"
	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestTryAdvisoryLock(t *testing.T) {
	ctx := context.Background()
	client, sqlDB := db.NewEnt(testutil.NewDB(t))
	t.Cleanup(func() { client.Close() })
	try := func(ctx context.Context, ns, key string) (func(), bool) {
		t.Helper()
		release, ok, err := db.TryAdvisoryLock(ctx, sqlDB, ns, key)
		require.NoError(t, err)
		return release, ok
	}

	release, ok := try(ctx, "a", "1")
	require.True(t, ok)
	_, ok = try(ctx, "a", "1")
	require.False(t, ok, "held by the first session")
	other, ok := try(ctx, "a", "2")
	require.True(t, ok, "another key is free")
	other()
	other, ok = try(ctx, "b", "1")
	require.True(t, ok, "another namespace is free")
	other()

	release()
	release()
	again, ok := try(ctx, "a", "1")
	require.True(t, ok, "free once released")

	// A release after the taker's context ended still unlocks.
	again()
	cctx, cancel := context.WithCancel(ctx)
	r, ok := try(cctx, "a", "1")
	require.True(t, ok)
	cancel()
	r()
	r, ok = try(ctx, "a", "1")
	require.True(t, ok)
	r()
	require.Equal(t, 0, sqlDB.Stats().InUse, "no connection kept")
}

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

package servers_test

import (
	"context"
	"testing"

	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/stretchr/testify/require"
)

func TestBackfillBackupSlots(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	a := e.RunningServer("a").ServerID
	b := e.RunningServer("b").ServerID
	set := e.RunningServer("c").ServerID
	c := e.App.Ent
	// Servers from before the migration all have slot 0.
	require.NoError(t, c.Server.UpdateOneID(a).SetBackupSlot(0).Exec(ctx))
	require.NoError(t, c.Server.UpdateOneID(b).SetBackupSlot(0).Exec(ctx))
	require.NoError(t, c.Server.UpdateOneID(set).SetBackupSlot(5).Exec(ctx))

	n, err := servers.BackfillBackupSlots(ctx, e.App.SQL)
	require.NoError(t, err)
	require.Equal(t, 2, n)
	require.Equal(t, int(a*887%1440), c.Server.GetX(ctx, a).BackupSlot)
	require.Equal(t, int(b*887%1440), c.Server.GetX(ctx, b).BackupSlot)
	require.Equal(t, 5, c.Server.GetX(ctx, set).BackupSlot)

	// Once per install.
	require.NoError(t, c.Server.UpdateOneID(a).SetBackupSlot(0).Exec(ctx))
	n, err = servers.BackfillBackupSlots(ctx, e.App.SQL)
	require.NoError(t, err)
	require.Zero(t, n)
	require.Zero(t, c.Server.GetX(ctx, a).BackupSlot)
}

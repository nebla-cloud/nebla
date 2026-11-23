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

package backups_test

import (
	"testing"
	"time"

	"github.com/nebla-cloud/nebla/internal/backups"
	"github.com/nebla-cloud/nebla/internal/catalog"
	"github.com/stretchr/testify/require"
)

func at(day, h, m int) time.Time { return time.Date(2026, 11, day, h, m, 0, 0, time.UTC) }

func TestNextDueWindow(t *testing.T) {
	loc := catalog.Location{BackupWindowStart: 120, BackupWindowEnd: 360}
	// Offset is slot * 240 / 1440 minutes into the window.
	require.Equal(t, at(23, 4, 0), backups.NextDue(at(23, 0, 0), loc, 720))
	require.Equal(t, at(23, 3, 0), backups.NextDue(at(23, 0, 0), loc, 360))
	require.Equal(t, at(24, 3, 0), backups.NextDue(at(23, 3, 0), loc, 360))
	require.Equal(t, at(23, 3, 0), backups.LastDue(at(23, 3, 0), loc, 360))
	require.Equal(t, at(22, 3, 0), backups.LastDue(at(23, 2, 59), loc, 360))

	wrap := catalog.Location{BackupWindowStart: 1380, BackupWindowEnd: 60}
	require.Equal(t, at(22, 23, 0), backups.LastDue(at(23, 0, 30), wrap, 0))
	require.Equal(t, at(23, 23, 0), backups.NextDue(at(23, 0, 30), wrap, 0))
	// Slot 1200 of 1440 puts 100 minutes into the 120 minute window: 00:40.
	require.Equal(t, at(23, 0, 40), backups.NextDue(at(23, 0, 30), wrap, 1200))
}

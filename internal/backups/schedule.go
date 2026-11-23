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

package backups

import (
	"time"

	"github.com/nebla-cloud/nebla/internal/catalog"
)

const minutesPerDay = 1440

// dueMinute is the UTC minute of day the server's backup is due: the window start plus slot's share of the window.
func dueMinute(loc catalog.Location, slot int) int {
	length := ((loc.BackupWindowEnd-loc.BackupWindowStart)%minutesPerDay + minutesPerDay) % minutesPerDay
	if length == 0 {
		length = minutesPerDay
	}
	return (loc.BackupWindowStart + slot*length/minutesPerDay) % minutesPerDay
}

// LastDue is the latest due time at or before now.
func LastDue(now time.Time, loc catalog.Location, slot int) time.Time {
	now = now.UTC()
	due := now.Truncate(24 * time.Hour).Add(time.Duration(dueMinute(loc, slot)) * time.Minute)
	if due.After(now) {
		due = due.AddDate(0, 0, -1)
	}
	return due
}

// NextDue is the earliest due time after now.
func NextDue(now time.Time, loc catalog.Location, slot int) time.Time {
	return LastDue(now, loc, slot).AddDate(0, 0, 1)
}

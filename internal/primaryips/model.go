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

// Package primaryips holds the primary IP model.
package primaryips

import (
	"strconv"
	"strings"

	"github.com/nebla-cloud/nebla/internal/ent"
)

const (
	// ResourceType is the actions resource type for primary IPs.
	ResourceType = "primary_ip"
	KindAssign   = "assign_primary_ip"
	KindUnassign = "unassign_primary_ip"
	// MaxAssignFailures is how many failed assigns in a row ResumeStuck retries before it stops.
	MaxAssignFailures = 5
)

// DefaultName is <server>-<family>, or <server>-<family>-<allocID> when taken; it matches the data migration.
func DefaultName(server, family string, allocID int64, taken func(string) bool) string {
	base := strings.TrimRight(left(server, 58), "-") + "-" + family
	if !taken(base) {
		return base
	}
	id := strconv.FormatInt(allocID, 10)
	return left(server, 57-len(id)) + "-" + family + "-" + id
}

func left(s string, n int) string {
	if n < 0 {
		n = 0
	}
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// Status is the member-facing state of a primary IP row.
func Status(row *ent.IPAddress) string {
	if row.ReleasingFromID != nil {
		return "releasing"
	}
	return "active"
}

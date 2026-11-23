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

// Package floatingips holds the floating IP model, its per-server allow-set and guest commands.
package floatingips

import (
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/riverqueue/river"
)

const (
	// ResourceType is the actions resource type for floating IPs.
	ResourceType = "floating_ip"
	KindAssign   = "assign_floating_ip"
	KindUnassign = "unassign_floating_ip"
	KindDelete   = "delete_floating_ip"
	// KindChange is the River job kind that carries out an assign, unassign or delete.
	KindChange = "floating_ip_change"
	// MaxPerServer caps the floating IPs one server may hold.
	MaxPerServer = 10
	// MaxAttempts matches the other lifecycle jobs.
	MaxAttempts = 5
)

// ChangeArgs is the floating_ip_change job for one action.
type ChangeArgs struct {
	ActionID     int64 `json:"action_id"`
	FloatingIPID int64 `json:"floating_ip_id"`
}

func (ChangeArgs) Kind() string { return KindChange }

func (ChangeArgs) InsertOpts() river.InsertOpts { return river.InsertOpts{MaxAttempts: MaxAttempts} }

// Status is the member-facing state of a floating IP row.
func Status(row *ent.IPAddress) string {
	switch {
	case row.Deleting:
		return "deleting"
	case row.ReleasingFromID != nil:
		return "releasing"
	}
	return "active"
}

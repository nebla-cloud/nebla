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

// Package networks holds private network ranges, SDN pool checks and guest commands.
package networks

import "github.com/riverqueue/river"

const (
	// ResourceType is the actions resource type for networks.
	ResourceType = "network"
	KindAttach   = "attach_to_network"
	KindDetach   = "detach_from_network"
	// MaxNICs caps the private NICs on one server (net1 to net3).
	MaxNICs = 3
	// DefaultPrefix is the default vnet name prefix.
	DefaultPrefix = "nb"
	// MaxAttempts matches the other lifecycle jobs that touch Proxmox.
	MaxAttempts = 8
)

// AttachArgs is the attach_to_network job for one action.
type AttachArgs struct {
	ActionID  int64 `json:"action_id"`
	ServerID  int64 `json:"server_id"`
	NetworkID int64 `json:"network_id"`
}

func (AttachArgs) Kind() string { return KindAttach }

func (AttachArgs) InsertOpts() river.InsertOpts { return river.InsertOpts{MaxAttempts: MaxAttempts} }

// DetachArgs is the detach_from_network job for one action.
type DetachArgs struct {
	ActionID  int64 `json:"action_id"`
	ServerID  int64 `json:"server_id"`
	NetworkID int64 `json:"network_id"`
}

func (DetachArgs) Kind() string { return KindDetach }

func (DetachArgs) InsertOpts() river.InsertOpts { return river.InsertOpts{MaxAttempts: MaxAttempts} }

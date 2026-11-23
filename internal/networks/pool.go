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

package networks

import (
	"fmt"

	"github.com/nebla-cloud/nebla/internal/pve"
)

// Candidate is one SDN vnet seen during pool setup, with its slot state.
type Candidate struct {
	VNet, Zone, Network string
	MTU                 int
	State, Reason       string
}

// Check reports whether a vnet can serve as a private network slot, and why not.
func Check(zone pve.SDNZone, v pve.SDNVNet, subnets []string, locationNodes int) (bool, string) {
	switch {
	case len(subnets) > 0:
		return false, "has SDN subnets"
	case v.VLANAware:
		return false, "is VLAN-aware"
	case zone.Type != "vxlan" && zone.Type != "simple":
		return false, fmt.Sprintf("zone type %s isn't supported", zone.Type)
	case zone.Type == "simple" && locationNodes > 1:
		return false, "simple zones work only in single-node locations"
	}
	return true, ""
}

// MTUFor returns the zone's MTU, else 1450 for vxlan and 1500 otherwise.
func MTUFor(z pve.SDNZone) int {
	switch {
	case z.MTU > 0:
		return z.MTU
	case z.Type == "vxlan":
		return 1450
	}
	return 1500
}

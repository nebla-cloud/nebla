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

package networks_test

import (
	"testing"

	"github.com/nebla-cloud/nebla/internal/networks"
	"github.com/nebla-cloud/nebla/internal/pve"
	"github.com/stretchr/testify/require"
)

func TestCheck(t *testing.T) {
	vx := pve.SDNZone{Zone: "z", Type: "vxlan"}
	cases := []struct {
		name    string
		zone    pve.SDNZone
		vnet    pve.SDNVNet
		subnets []string
		nodes   int
		ok      bool
		reason  string
	}{
		{"vxlan ok", vx, pve.SDNVNet{VNet: "nb1"}, nil, 3, true, ""},
		{"simple single node ok", pve.SDNZone{Type: "simple"}, pve.SDNVNet{}, nil, 1, true, ""},
		{"subnets", vx, pve.SDNVNet{}, []string{"10.0.0.0/24"}, 1, false, "has SDN subnets"},
		{"vlan aware", vx, pve.SDNVNet{VLANAware: true}, nil, 1, false, "is VLAN-aware"},
		{"zone type", pve.SDNZone{Type: "evpn"}, pve.SDNVNet{}, nil, 1, false, "zone type evpn isn't supported"},
		{"simple multi node", pve.SDNZone{Type: "simple"}, pve.SDNVNet{}, nil, 2, false, "simple zones work only in single-node locations"},
	}
	for _, c := range cases {
		ok, reason := networks.Check(c.zone, c.vnet, c.subnets, c.nodes)
		require.Equal(t, c.ok, ok, c.name)
		require.Equal(t, c.reason, reason, c.name)
	}
}

func TestMTUFor(t *testing.T) {
	require.Equal(t, 1450, networks.MTUFor(pve.SDNZone{Type: "vxlan"}))
	require.Equal(t, 1500, networks.MTUFor(pve.SDNZone{Type: "simple"}))
	require.Equal(t, 9000, networks.MTUFor(pve.SDNZone{Type: "vxlan", MTU: 9000}))
}

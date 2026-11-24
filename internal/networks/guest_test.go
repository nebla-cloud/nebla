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
	"net/netip"
	"testing"

	"github.com/nebla-cloud/nebla/internal/networks"
	"github.com/stretchr/testify/require"
)

const (
	mac   = "bc:24:11:34:6f:1c"
	file7 = "/etc/netplan/60-nebla-net-7.yaml"
)

const guestWait = `for i in $(seq 30); do IF=$(ip -br link | awk '$3 == "bc:24:11:34:6f:1c" {print $1}'); [ -n "$IF" ] && break; sleep 1; done; [ -n "$IF" ] || { echo "The network isn't attached yet; try again in a moment." >&2; false; }`

var addr = netip.MustParsePrefix("10.0.0.2/16")

func TestGuestUbuntu(t *testing.T) {
	g := networks.Guest("ubuntu", 7, mac, addr, 1450, true)
	require.Equal(t, guestWait+"\nsudo ip link set \"$IF\" mtu 1450\nsudo ip link set \"$IF\" up\nsudo ip addr add 10.0.0.2/16 dev \"$IF\"", g.Apply)
	require.Equal(t, `echo 'network: {version: 2, ethernets: {nebla-net-7: {match: {macaddress: "bc:24:11:34:6f:1c"}, addresses: ["10.0.0.2/16"], mtu: 1450}}}' | sudo tee `+file7+` >/dev/null && sudo chmod 600 `+file7+` && sudo netplan apply`, g.Persist)
	require.Equal(t, "sudo rm -f "+file7+" && sudo netplan apply", g.Remove)
}

func TestGuestDebianMatchesUbuntu(t *testing.T) {
	require.Equal(t, networks.Guest("ubuntu", 7, mac, addr, 1450, true), networks.Guest("debian", 7, mac, addr, 1450, true))
}

func TestGuestDefaultMTUOmitted(t *testing.T) {
	g := networks.Guest("ubuntu", 7, mac, addr, 1500, true)
	require.NotContains(t, g.Apply, "mtu")
	require.NotContains(t, g.Persist, "mtu")
}

func TestGuestNoPersist(t *testing.T) {
	g := networks.Guest("ubuntu", 7, mac, addr, 1450, false)
	require.Empty(t, g.Persist)
	require.Empty(t, g.Remove)
	require.NotEmpty(t, g.Apply)
}

func TestGuestGeneric(t *testing.T) {
	g := networks.Guest("rocky", 7, mac, addr, 1450, true)
	require.Equal(t, "To keep it after a reboot, add 10.0.0.2/16 to the interface with MAC bc:24:11:34:6f:1c in your network configuration.", g.Persist)
	require.Empty(t, g.Remove)
	require.Contains(t, g.Apply, "sudo ip addr add 10.0.0.2/16")
}

func TestRemoval(t *testing.T) {
	require.Equal(t, "sudo rm -f "+file7+" && sudo netplan apply", networks.Removal("ubuntu", 7, true))
	require.Empty(t, networks.Removal("ubuntu", 7, false))
	require.Empty(t, networks.Removal("rocky", 7, true))
}

func TestGuestMACCaseNormalized(t *testing.T) {
	require.Equal(t,
		networks.Guest("ubuntu", 7, mac, addr, 1450, true),
		networks.Guest("ubuntu", 7, "BC:24:11:34:6F:1C", addr, 1450, true))
	require.Equal(t,
		networks.Guest("rocky", 7, mac, addr, 1450, true),
		networks.Guest("rocky", 7, "BC:24:11:34:6F:1C", addr, 1450, true))
}

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

package floatingips_test

import (
	"net/netip"
	"testing"

	"github.com/nebla-cloud/nebla/internal/floatingips"
	"github.com/stretchr/testify/require"
)

const netplanV4 = "/etc/netplan/60-nebla-floating-203-0-113-7.yaml"

func TestGuestUbuntuV4(t *testing.T) {
	g := floatingips.Guest("ubuntu", netip.MustParseAddr("203.0.113.7"))
	require.Equal(t, "sudo ip addr add 203.0.113.7/32 dev eth0\nsudo apt-get install -y iputils-arping\nsudo arping -U -c 3 -I eth0 203.0.113.7", g.Apply)
	require.Equal(t, `echo 'network: {version: 2, ethernets: {eth0: {addresses: ["203.0.113.7/32"]}}}' | sudo tee `+netplanV4+` >/dev/null && sudo chmod 600 `+netplanV4+` && sudo netplan apply`, g.Persist)
}

func TestGuestDebianMatchesUbuntu(t *testing.T) {
	ip := netip.MustParseAddr("203.0.113.7")
	require.Equal(t, floatingips.Guest("ubuntu", ip), floatingips.Guest("debian", ip))
}

func TestGuestGenericV4(t *testing.T) {
	g := floatingips.Guest("rocky", netip.MustParseAddr("203.0.113.7"))
	require.Equal(t, "sudo ip addr add 203.0.113.7/32 dev eth0\n# Install arping from iputils with your package manager.\nsudo arping -U -c 3 -I eth0 203.0.113.7", g.Apply)
	require.Equal(t, "To keep it after a reboot, add 203.0.113.7/32 to eth0 in your network configuration.", g.Persist)
}

func TestGuestUbuntuV6(t *testing.T) {
	g := floatingips.Guest("ubuntu", netip.MustParseAddr("2001:db8::7"))
	file := "/etc/netplan/60-nebla-floating-2001-db8--7.yaml"
	require.Equal(t, "sudo sysctl -w net.ipv6.conf.eth0.ndisc_notify=1\nsudo ip -6 addr add 2001:db8::7/128 dev eth0 preferred_lft 0", g.Apply)
	require.Equal(t, `echo 'network: {version: 2, ethernets: {eth0: {addresses: [{"2001:db8::7/128": {lifetime: 0}}]}}}' | sudo tee `+file+` >/dev/null && sudo chmod 600 `+file+` && sudo netplan apply`, g.Persist)
}

func TestGuestGenericV6(t *testing.T) {
	g := floatingips.Guest("rocky", netip.MustParseAddr("2001:db8::7"))
	require.Equal(t, "To keep it after a reboot, add 2001:db8::7/128 to eth0 in your network configuration.", g.Persist)
}

func TestRemoval(t *testing.T) {
	v4, v6 := netip.MustParseAddr("203.0.113.7"), netip.MustParseAddr("2001:db8::7")
	require.Equal(t, "sudo ip addr del 203.0.113.7/32 dev eth0\nsudo rm -f "+netplanV4+" && sudo netplan apply", floatingips.Removal("ubuntu", v4))
	require.Equal(t, "sudo ip addr del 203.0.113.7/32 dev eth0", floatingips.Removal("rocky", v4))
	require.Equal(t, "sudo ip -6 addr del 2001:db8::7/128 dev eth0\nsudo rm -f /etc/netplan/60-nebla-floating-2001-db8--7.yaml && sudo netplan apply", floatingips.Removal("debian", v6))
}

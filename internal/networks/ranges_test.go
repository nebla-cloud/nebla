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

func TestParseRange(t *testing.T) {
	cases := []struct {
		in, want string
		ok       bool
	}{
		{"10.0.0.0/16", "10.0.0.0/16", true},
		{"10.0.0.5/16", "10.0.0.0/16", true},
		{"172.16.4.0/24", "172.16.4.0/24", true},
		{"192.168.1.0/24", "192.168.1.0/24", true},
		{"8.8.8.0/24", "", false},
		{"10.0.0.0/29", "", false},
		{"10.0.0.0/7", "", false},
		{"fd00::/64", "", false},
		{"172.32.0.0/16", "", false},
		{"172.15.0.0/16", "", false},
		{"192.169.0.0/16", "", false},
		{"11.0.0.0/8", "", false},
		{"nope", "", false},
	}
	for _, c := range cases {
		p, err := networks.ParseRange(c.in)
		if !c.ok {
			require.Error(t, err, c.in)
			continue
		}
		require.NoError(t, err, c.in)
		require.Equal(t, c.want, p.String())
	}
}

func TestDefaultRangeSkipsTaken(t *testing.T) {
	taken := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/16"), netip.MustParsePrefix("10.1.5.0/24")}
	p, ok := networks.DefaultRange(taken)
	require.True(t, ok)
	require.Equal(t, "10.2.0.0/16", p.String())
}

func TestDefaultRangeExhausted(t *testing.T) {
	_, ok := networks.DefaultRange([]netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")})
	require.False(t, ok)
}

func TestOverlaps(t *testing.T) {
	p := netip.MustParsePrefix("10.0.0.0/16")
	require.True(t, networks.Overlaps(p, []netip.Prefix{netip.MustParsePrefix("10.0.5.0/24")}))
	require.True(t, networks.Overlaps(p, []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}))
	require.False(t, networks.Overlaps(p, []netip.Prefix{netip.MustParsePrefix("10.1.0.0/16")}))
	require.False(t, networks.Overlaps(p, nil))
}

func TestNextFreeSkipsReserved(t *testing.T) {
	p := netip.MustParsePrefix("10.0.0.0/24")
	ip, ok := networks.NextFree(p, nil)
	require.True(t, ok)
	require.Equal(t, "10.0.0.2", ip.String())
	ip, ok = networks.NextFree(p, []netip.Addr{netip.MustParseAddr("10.0.0.2")})
	require.True(t, ok)
	require.Equal(t, "10.0.0.3", ip.String())

	small := netip.MustParsePrefix("10.0.0.0/28")
	var used []netip.Addr
	for {
		ip, ok := networks.NextFree(small, used)
		if !ok {
			break
		}
		require.NotEqual(t, "10.0.0.15", ip.String())
		used = append(used, ip)
	}
	require.Len(t, used, 13)
}

func TestUsable(t *testing.T) {
	p := netip.MustParsePrefix("10.0.0.0/24")
	for ip, want := range map[string]bool{
		"10.0.0.0": false, "10.0.0.1": false, "10.0.0.2": true, "10.0.0.254": true,
		"10.0.0.255": false, "10.0.1.2": false,
	} {
		require.Equal(t, want, networks.Usable(p, netip.MustParseAddr(ip)), ip)
	}
}

func TestValidPrefix(t *testing.T) {
	for s, want := range map[string]bool{"nb": true, "a1b2": true, "1ab": false, "abcde": false, "NB": false, "": false} {
		require.Equal(t, want, networks.ValidPrefix(s), s)
	}
}

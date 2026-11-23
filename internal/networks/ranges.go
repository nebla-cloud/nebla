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
	"errors"
	"fmt"
	"net/netip"
	"regexp"
)

var privateBlocks = []netip.Prefix{
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),
}

var prefixRe = regexp.MustCompile(`^[a-z][a-z0-9]{0,3}$`)

// ParseRange parses a private IPv4 range of /8 to /28 and returns it in canonical form.
func ParseRange(s string) (netip.Prefix, error) {
	p, err := netip.ParsePrefix(s)
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("parse range: %w", err)
	}
	if !p.Addr().Is4() {
		return netip.Prefix{}, errors.New("range must be IPv4")
	}
	if p.Bits() < 8 || p.Bits() > 28 {
		return netip.Prefix{}, errors.New("range must be between /8 and /28")
	}
	p = p.Masked()
	for _, b := range privateBlocks {
		if b.Contains(p.Addr()) && p.Bits() >= b.Bits() {
			return p, nil
		}
	}
	return netip.Prefix{}, errors.New("range must be inside 10.0.0.0/8, 172.16.0.0/12 or 192.168.0.0/16")
}

// DefaultRange returns the first 10.N.0.0/16 that overlaps none of taken.
func DefaultRange(taken []netip.Prefix) (netip.Prefix, bool) {
	for n := range 256 {
		p := netip.PrefixFrom(netip.AddrFrom4([4]byte{10, byte(n), 0, 0}), 16)
		if !Overlaps(p, taken) {
			return p, true
		}
	}
	return netip.Prefix{}, false
}

// Overlaps reports whether p overlaps any of others.
func Overlaps(p netip.Prefix, others []netip.Prefix) bool {
	for _, o := range others {
		if p.Overlaps(o) {
			return true
		}
	}
	return false
}

// Usable reports whether ip can be handed out inside p: not the network address, broadcast or first host.
func Usable(p netip.Prefix, ip netip.Addr) bool {
	if !p.Contains(ip) || !ip.Is4() {
		return false
	}
	first := p.Masked().Addr()
	if ip == first || ip == first.Next() {
		return false
	}
	return ip != broadcast(p)
}

func broadcast(p netip.Prefix) netip.Addr {
	a := p.Masked().Addr().As4()
	host := uint32(1)<<(32-p.Bits()) - 1
	v := uint32(a[0])<<24 | uint32(a[1])<<16 | uint32(a[2])<<8 | uint32(a[3])
	v |= host
	return netip.AddrFrom4([4]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)})
}

// NextFree returns the lowest usable address in p that isn't in used.
func NextFree(p netip.Prefix, used []netip.Addr) (netip.Addr, bool) {
	taken := make(map[netip.Addr]bool, len(used))
	for _, u := range used {
		taken[u] = true
	}
	for ip := p.Masked().Addr(); p.Contains(ip); ip = ip.Next() {
		if Usable(p, ip) && !taken[ip] {
			return ip, true
		}
	}
	return netip.Addr{}, false
}

// ValidPrefix reports whether s is a valid vnet name prefix.
func ValidPrefix(s string) bool { return prefixRe.MatchString(s) }

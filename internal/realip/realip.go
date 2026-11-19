// Package realip resolves the client address behind trusted reverse proxies.
package realip

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// ParseCIDRs parses a comma-separated list of CIDRs; a bare address is a single-host prefix.
func ParseCIDRs(s string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for part := range strings.SplitSeq(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if !strings.Contains(part, "/") {
			a, err := netip.ParseAddr(part)
			if err != nil {
				return nil, fmt.Errorf("invalid trusted proxy %q", part)
			}
			a = a.Unmap()
			out = append(out, netip.PrefixFrom(a, a.BitLen()))
			continue
		}
		p, err := netip.ParsePrefix(part)
		if err != nil {
			return nil, fmt.Errorf("invalid trusted proxy %q", part)
		}
		out = append(out, p.Masked())
	}
	return out, nil
}

// ClientIP returns RemoteAddr's host, or, when that is a trusted proxy, the rightmost untrusted X-Forwarded-For hop.
func ClientIP(r *http.Request, trusted []netip.Prefix) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	remote, err := netip.ParseAddr(host)
	if err != nil || !isTrusted(remote.Unmap(), trusted) {
		return host
	}
	hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	cur := host
	for i := len(hops) - 1; i >= 0; i-- {
		h := strings.TrimSpace(hops[i])
		if h == "" {
			continue
		}
		a, err := netip.ParseAddr(h)
		if err != nil {
			break
		}
		a = a.Unmap()
		cur = a.String()
		if !isTrusted(a, trusted) {
			return cur
		}
	}
	return cur
}

func isTrusted(a netip.Addr, trusted []netip.Prefix) bool {
	for _, p := range trusted {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

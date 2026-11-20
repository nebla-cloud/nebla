package realip_test

import (
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/nebla-cloud/nebla/internal/realip"
	"github.com/stretchr/testify/require"
)

func TestClientIP(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("fd00::/8")}
	cases := []struct {
		name    string
		remote  string
		xff     []string
		trusted []netip.Prefix
		want    string
	}{
		{"no trusted proxies ignores xff", "203.0.113.9:1234", []string{"198.51.100.1"}, nil, "203.0.113.9"},
		{"untrusted remote ignores xff", "203.0.113.9:1234", []string{"198.51.100.1"}, trusted, "203.0.113.9"},
		{"trusted remote without xff", "10.0.0.2:1234", nil, trusted, "10.0.0.2"},
		{"trusted remote uses xff", "10.0.0.2:1234", []string{"198.51.100.1"}, trusted, "198.51.100.1"},
		{"skips trusted hops right to left", "10.0.0.2:1234", []string{"198.51.100.1, 10.1.1.1"}, trusted, "198.51.100.1"},
		{"spoofed left entries are ignored", "10.0.0.2:1234", []string{"1.2.3.4, 198.51.100.1, 10.1.1.1"}, trusted, "198.51.100.1"},
		{"multiple header lines", "10.0.0.2:1234", []string{"1.2.3.4", "198.51.100.1"}, trusted, "198.51.100.1"},
		{"all trusted returns leftmost", "10.0.0.2:1234", []string{"10.9.9.9, 10.1.1.1"}, trusted, "10.9.9.9"},
		{"invalid entry stops at last trusted hop", "10.0.0.2:1234", []string{"198.51.100.1, garbage, 10.1.1.1"}, trusted, "10.1.1.1"},
		{"ipv6 remote and client", "[fd00::1]:443", []string{"2001:db8::5"}, trusted, "2001:db8::5"},
		{"mapped ipv4 remote is trusted", "[::ffff:10.0.0.2]:1234", []string{"198.51.100.1"}, trusted, "198.51.100.1"},
		{"remote without port", "10.0.0.2", []string{"198.51.100.1"}, trusted, "198.51.100.1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = tc.remote
			for _, v := range tc.xff {
				r.Header.Add("X-Forwarded-For", v)
			}
			require.Equal(t, tc.want, realip.ClientIP(r, tc.trusted))
		})
	}
}

func TestParseCIDRs(t *testing.T) {
	got, err := realip.ParseCIDRs(" 10.0.0.0/8, 192.168.1.5 ,fd00::/8,")
	require.NoError(t, err)
	require.Equal(t, []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("192.168.1.5/32"),
		netip.MustParsePrefix("fd00::/8")}, got)

	got, err = realip.ParseCIDRs("")
	require.NoError(t, err)
	require.Empty(t, got)

	_, err = realip.ParseCIDRs("10.0.0.0/8,nope")
	require.ErrorContains(t, err, "nope")
}

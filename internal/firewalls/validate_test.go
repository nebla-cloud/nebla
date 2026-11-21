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

package firewalls

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/nebla-cloud/nebla/internal/apierr"
	"github.com/stretchr/testify/require"
)

func tcp(port string, ips ...string) RuleInput {
	return RuleInput{Direction: "in", Protocol: "tcp", Port: ptr(port), IPs: ips}
}

func requireInvalid(t *testing.T, err error, contains string) {
	t.Helper()
	var ae apierr.E
	require.True(t, errors.As(err, &ae), "want apierr, got %v", err)
	require.Equal(t, 422, ae.Status)
	require.Equal(t, "invalid_input", ae.Code)
	require.Contains(t, ae.Msg, contains)
}

func TestValidRules(t *testing.T) {
	many := make([]string, 51)
	for i := range many {
		many[i] = fmt.Sprintf("10.0.%d.0/24", i)
	}
	bad := []struct {
		name string
		in   []RuleInput
		msg  string
	}{
		{"port 0", []RuleInput{tcp("0", "0.0.0.0/0")}, "rules[0]"},
		{"port 65536", []RuleInput{tcp("65536", "0.0.0.0/0")}, "rules[0]"},
		{"reversed range", []RuleInput{tcp("90-80", "0.0.0.0/0")}, "rules[0]"},
		{"junk port", []RuleInput{tcp("ssh", "0.0.0.0/0")}, "rules[0]"},
		{"tcp without port", []RuleInput{{Direction: "in", Protocol: "tcp", IPs: []string{"0.0.0.0/0"}}}, "port"},
		{"icmp with port", []RuleInput{{Direction: "in", Protocol: "icmp", Port: ptr("22"), IPs: []string{"0.0.0.0/0"}}}, "port"},
		{"bad protocol", []RuleInput{{Direction: "in", Protocol: "sctp", IPs: []string{"0.0.0.0/0"}}}, "protocol"},
		{"bad direction", []RuleInput{{Direction: "both", Protocol: "gre", IPs: []string{"0.0.0.0/0"}}}, "direction"},
		{"no ips", []RuleInput{{Direction: "in", Protocol: "gre"}}, "rules[0]"},
		{"51 cidrs", []RuleInput{tcp("22", many...)}, "50"},
		{"garbage cidr second rule", []RuleInput{tcp("22", "0.0.0.0/0"), tcp("80", "10.0.0.0/33")}, "rules[1]"},
		{"bare address", []RuleInput{tcp("22", "10.0.0.1")}, "rules[0]"},
		{"v4-mapped v6", []RuleInput{tcp("22", "::ffff:1.2.3.4/128")}, "rules[0]"},
		{"long description", []RuleInput{{Direction: "in", Protocol: "gre", IPs: []string{"::/0"}, Description: strings.Repeat("x", 256)}}, "255"},
		{"101 rules", func() []RuleInput {
			rs := make([]RuleInput, 101)
			for i := range rs {
				rs[i] = tcp("22", "0.0.0.0/0")
			}
			return rs
		}(), "100"},
	}
	for _, c := range bad {
		t.Run(c.name, func(t *testing.T) {
			_, err := validRules(c.in)
			requireInvalid(t, err, c.msg)
		})
	}

	ok := []RuleInput{
		tcp("22", "0.0.0.0/0", "::/0"),
		tcp("1-65535", "10.0.0.1/8", "2001:db8::1/32"),
		{Direction: "out", Protocol: "udp", Port: ptr("53"), IPs: []string{"1.1.1.1/32"}, Description: strings.Repeat("d", 255)},
		{Direction: "in", Protocol: "icmp", IPs: []string{"0.0.0.0/0"}},
		{Direction: "out", Protocol: "esp", IPs: []string{"192.0.2.0/24"}},
	}
	got, err := validRules(ok)
	require.NoError(t, err)
	require.Equal(t, []string{"10.0.0.0/8", "2001:db8::/32"}, got[1].IPs)
	require.Equal(t, "1-65535", *got[1].Port)
	require.Nil(t, got[3].Port)

	_, err = validRules(make([]RuleInput, 100))
	requireInvalid(t, err, "rules[0]")
	empty, err := validRules(nil)
	require.NoError(t, err)
	require.Empty(t, empty)
}

func TestIPsFor(t *testing.T) {
	ips, err := IPsFor(0, "in", []string{"0.0.0.0/0"}, nil)
	require.NoError(t, err)
	require.Equal(t, []string{"0.0.0.0/0"}, ips)
	ips, err = IPsFor(0, "out", nil, []string{"::/0"})
	require.NoError(t, err)
	require.Equal(t, []string{"::/0"}, ips)
	_, err = IPsFor(2, "in", []string{"0.0.0.0/0"}, []string{"10.0.0.0/8"})
	requireInvalid(t, err, "rules[2]")
	_, err = IPsFor(0, "out", []string{"0.0.0.0/0"}, nil)
	requireInvalid(t, err, "destination_ips")
}

func TestValidNameAndResources(t *testing.T) {
	require.NoError(t, validName("web-fw"))
	for _, n := range []string{"", "Web", "-a", "a_b", strings.Repeat("a", 64)} {
		requireInvalid(t, validName(n), "name")
	}
	requireInvalid(t, validLabels(map[string]string{"bad key": "v"}), "label key")

	id := int64(3)
	sel := " role = web , !old "
	got, err := validResources([]ResourceInput{{Type: "server", ServerID: &id}, {Type: "label_selector", Selector: &sel}, {Type: "server", ServerID: &id}})
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, "role=web,!old", *got[1].Selector)

	empty := ""
	for _, r := range []ResourceInput{{Type: "server"}, {Type: "label_selector"}, {Type: "label_selector", Selector: &empty}, {Type: "vm", ServerID: &id},
		{Type: "server", ServerID: &id, Selector: &sel}} {
		_, err := validResources([]ResourceInput{r})
		requireInvalid(t, err, "")
	}
}

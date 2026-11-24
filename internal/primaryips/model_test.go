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

package primaryips_test

import (
	"strings"
	"testing"

	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/primaryips"
	"github.com/stretchr/testify/require"
)

func TestDefaultName(t *testing.T) {
	none := func(string) bool { return false }
	require.Equal(t, "web-ipv4", primaryips.DefaultName("web", "ipv4", 7, none))
	require.Equal(t, "web-ipv6", primaryips.DefaultName("web", "ipv6", 7, none))

	long := strings.Repeat("a", 57) + "-bcdef"
	got := primaryips.DefaultName(long, "ipv4", 7, none)
	require.Equal(t, strings.Repeat("a", 57)+"-ipv4", got, "the cut trims every trailing dash")
	dashes := strings.Repeat("a", 55) + "---bc"
	require.Equal(t, strings.Repeat("a", 55)+"-ipv4", primaryips.DefaultName(dashes, "ipv4", 7, none))

	full := strings.Repeat("b", 63)
	require.Equal(t, strings.Repeat("b", 58)+"-ipv6", primaryips.DefaultName(full, "ipv6", 7, none))
	require.Len(t, primaryips.DefaultName(full, "ipv6", 7, none), 63)

	taken := func(n string) bool { return n == "web-ipv4" }
	require.Equal(t, "web-ipv4-1234", primaryips.DefaultName("web", "ipv4", 1234, taken))
	all := func(string) bool { return true }
	got = primaryips.DefaultName(full, "ipv4", 1234, all)
	require.Equal(t, strings.Repeat("b", 53)+"-ipv4-1234", got)
	require.Len(t, got, 63)
	cut := strings.Repeat("c", 52) + "-defgh"
	require.Equal(t, strings.Repeat("c", 52)+"--ipv4-1234", primaryips.DefaultName(cut, "ipv4", 1234, all), "the fallback cut is not trimmed")
}

func TestStatus(t *testing.T) {
	from := int64(3)
	require.Equal(t, "active", primaryips.Status(&ent.IPAddress{}))
	require.Equal(t, "releasing", primaryips.Status(&ent.IPAddress{ReleasingFromID: &from}))
}

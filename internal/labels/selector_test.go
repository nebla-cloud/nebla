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

package labels

import (
	"testing"

	"github.com/nebla-cloud/nebla/internal/apierr"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	s, err := Parse("role=web,env!=prod,tier,!old")
	require.NoError(t, err)
	require.Equal(t, Selector{
		{Key: "role", Value: "web", Op: Eq},
		{Key: "env", Value: "prod", Op: Ne},
		{Key: "tier", Op: Has},
		{Key: "old", Op: Lacks},
	}, s)
	require.Equal(t, "role=web,env!=prod,tier,!old", s.String())

	s, err = Parse(" a = b , !c ")
	require.NoError(t, err)
	require.Equal(t, "a=b,!c", s.String())

	_, err = Parse("")
	requireMsg(t, err, "label_selector must not be empty")
	_, err = Parse("  ")
	requireMsg(t, err, "label_selector must not be empty")
	_, err = Parse("role=web,-bad=x")
	requireMsg(t, err, `label_selector: bad key in "-bad=x"`)
	_, err = Parse("a,,b")
	requireMsg(t, err, `label_selector: bad key in ""`)
	_, err = Parse("a=-x")
	requireMsg(t, err, `label_selector: bad value in "a=-x"`)
}

func TestMatches(t *testing.T) {
	s, err := Parse("role=web,env!=prod,tier,!old")
	require.NoError(t, err)
	require.True(t, s.Matches(map[string]string{"role": "web", "env": "dev", "tier": "x"}))
	require.True(t, s.Matches(map[string]string{"role": "web", "tier": "x"}))
	require.False(t, s.Matches(map[string]string{"role": "web", "env": "prod", "tier": "x"}))
	require.False(t, s.Matches(map[string]string{"role": "web", "tier": "x", "old": "y"}))
	require.False(t, s.Matches(nil))
}

func requireMsg(t *testing.T, err error, msg string) {
	t.Helper()
	var e apierr.E
	require.ErrorAs(t, err, &e)
	require.Equal(t, "invalid_input", e.Code)
	require.Equal(t, msg, e.Msg)
}

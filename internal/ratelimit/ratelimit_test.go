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

package ratelimit_test

import (
	"testing"
	"time"

	"github.com/nebla-cloud/nebla/internal/ratelimit"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

func TestAllowBurstThenDeny(t *testing.T) {
	l := ratelimit.New(rate.Every(time.Hour), 3)
	for range 3 {
		require.True(t, l.Allow("a"))
	}
	require.False(t, l.Allow("a"))
	require.True(t, l.Allow("b"))
}

func TestIdleKeysAreSwept(t *testing.T) {
	now := time.Now()
	l := ratelimit.New(rate.Every(time.Second), 1)
	l.SetClock(func() time.Time { return now })
	l.Allow("a")
	require.Equal(t, 1, l.Len())
	now = now.Add(time.Hour)
	l.Allow("b")
	require.Equal(t, 1, l.Len())
}

func TestReserveReportsRemainingAndReset(t *testing.T) {
	now := time.Now()
	l := ratelimit.New(rate.Every(time.Second), 3)
	l.SetClock(func() time.Time { return now })
	r := l.Reserve("a")
	require.True(t, r.Allowed)
	require.Equal(t, 2, r.Remaining)
	require.Equal(t, time.Second, r.Reset)
	l.Reserve("a")
	l.Reserve("a")
	r = l.Reserve("a")
	require.False(t, r.Allowed)
	require.Equal(t, 0, r.Remaining)
	require.Equal(t, 3*time.Second, r.Reset)
	require.Equal(t, time.Second, r.RetryAfter)
}

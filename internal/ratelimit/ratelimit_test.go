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

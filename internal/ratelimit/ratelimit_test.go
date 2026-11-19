package ratelimit_test

import (
	"testing"
	"time"

	"github.com/Hexeption/nebla/internal/ratelimit"
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

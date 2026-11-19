package auth

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHashConcurrencyBounded(t *testing.T) {
	prev := setHashConcurrency(2)
	defer setHashConcurrency(prev)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			_, err := HashPassword(context.Background(), "correct-horse-battery")
			require.NoError(t, err)
		})
	}
	wg.Wait()
	require.Equal(t, int64(2), hashPeak.Load())
}

func TestHashWaitHonoursContext(t *testing.T) {
	prev := setHashConcurrency(1)
	defer setHashConcurrency(prev)
	release, err := acquireHash(context.Background())
	require.NoError(t, err)
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err = HashPassword(ctx, "correct-horse-battery")
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

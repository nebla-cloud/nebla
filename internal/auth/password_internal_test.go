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

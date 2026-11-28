// Copyright (C) 2026 The Nebla contributors
// SPDX-License-Identifier: Apache-2.0

package nebla

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordSleeps replaces retrySleep with a recorder that does not wait.
func recordSleeps(t *testing.T) *[]time.Duration {
	t.Helper()
	var mu sync.Mutex
	var got []time.Duration
	old := retrySleep
	retrySleep = func(ctx context.Context, d time.Duration) error {
		mu.Lock()
		got = append(got, d)
		mu.Unlock()
		return ctx.Err()
	}
	t.Cleanup(func() { retrySleep = old })
	return &got
}

// statusServer answers with statuses in order, then the last one repeated.
func statusServer(t *testing.T, header http.Header, statuses ...int) (*Client, *atomic.Int32, *[][]byte) {
	t.Helper()
	var n atomic.Int32
	var mu sync.Mutex
	var bodies [][]byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 1024)
		k, _ := r.Body.Read(buf)
		mu.Lock()
		bodies = append(bodies, buf[:k])
		mu.Unlock()
		i := int(n.Add(1)) - 1
		for k, v := range header {
			w.Header()[k] = v
		}
		w.WriteHeader(statuses[min(i, len(statuses)-1)])
	}))
	t.Cleanup(srv.Close)
	c := NewClient(WithEndpoint(srv.URL), WithToken("tok"), WithRetry(DefaultRetryPolicy))
	return c, &n, &bodies
}

func TestRetry429AnyMethod(t *testing.T) {
	recordSleeps(t)
	c, n, bodies := statusServer(t, nil, 429, 201)
	require.NoError(t, c.do(t.Context(), "POST", "/servers", nil, map[string]string{"name": "x"}, nil))
	assert.EqualValues(t, 2, n.Load())
	require.Len(t, *bodies, 2)
	assert.Equal(t, (*bodies)[0], (*bodies)[1])
	assert.JSONEq(t, `{"name":"x"}`, string((*bodies)[1]))
}

func TestRetry5xxGetOnly(t *testing.T) {
	recordSleeps(t)
	c, n, _ := statusServer(t, nil, 503, 503, 200)
	require.NoError(t, c.do(t.Context(), "GET", "/servers", nil, nil, nil))
	assert.EqualValues(t, 3, n.Load())

	c, n, _ = statusServer(t, nil, 503, 200)
	err := c.do(t.Context(), "POST", "/servers", nil, nil, nil)
	var e *Error
	require.True(t, errors.As(err, &e))
	assert.Equal(t, 503, e.StatusCode)
	assert.EqualValues(t, 1, n.Load())
}

func TestRetryAfterHonoured(t *testing.T) {
	sleeps := recordSleeps(t)
	c, n, _ := statusServer(t, http.Header{"Retry-After": {"1"}}, 429, 200)
	require.NoError(t, c.do(t.Context(), "GET", "/servers", nil, nil, nil))
	assert.EqualValues(t, 2, n.Load())
	assert.Equal(t, []time.Duration{time.Second}, *sleeps)
}

func TestRetryAfterCapped(t *testing.T) {
	sleeps := recordSleeps(t)
	c, _, _ := statusServer(t, http.Header{"Retry-After": {"3600"}}, 429, 200)
	require.NoError(t, c.do(t.Context(), "GET", "/servers", nil, nil, nil))
	assert.Equal(t, []time.Duration{time.Minute}, *sleeps)
}

func TestRetryBackoffDoubles(t *testing.T) {
	sleeps := recordSleeps(t)
	c, n, _ := statusServer(t, nil, 503)
	err := c.do(t.Context(), "GET", "/servers", nil, nil, nil)
	require.Error(t, err)
	assert.EqualValues(t, 5, n.Load())
	assert.Equal(t, []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second, 4 * time.Second}, *sleeps)
}

func TestRetryGivesUp(t *testing.T) {
	recordSleeps(t)
	c, n, _ := statusServer(t, nil, 503)
	err := c.do(t.Context(), "GET", "/servers", nil, nil, nil)
	var e *Error
	require.True(t, errors.As(err, &e))
	assert.Equal(t, 503, e.StatusCode)
	assert.EqualValues(t, DefaultRetryPolicy.MaxAttempts, n.Load())
}

func TestRetryDisabled(t *testing.T) {
	recordSleeps(t)
	c, n, _ := statusServer(t, nil, 429)
	c = NewClient(WithEndpoint(c.endpoint), WithToken("tok"), WithRetry(RetryPolicy{MaxAttempts: 1}))
	require.Error(t, c.do(t.Context(), "GET", "/servers", nil, nil, nil))
	assert.EqualValues(t, 1, n.Load())
}

func TestRetryTransportErrorGet(t *testing.T) {
	recordSleeps(t)
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) < 3 {
			hj, _ := w.(http.Hijacker)
			conn, _, _ := hj.Hijack()
			_ = conn.Close()
			return
		}
	}))
	defer srv.Close()
	c := NewClient(WithEndpoint(srv.URL), WithToken("tok"))
	require.NoError(t, c.do(t.Context(), "GET", "/servers", nil, nil, nil))
	assert.EqualValues(t, 3, n.Load())
}

func TestRetryStopsOnCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(429)
	}))
	defer srv.Close()
	c := NewClient(WithEndpoint(srv.URL), WithToken("tok"))
	ctx, cancel := context.WithCancel(t.Context())
	time.AfterFunc(20*time.Millisecond, cancel)
	start := time.Now()
	err := c.do(ctx, "GET", "/servers", nil, nil, nil)
	assert.Less(t, time.Since(start), 100*time.Millisecond+20*time.Millisecond)
	assert.ErrorIs(t, err, context.Canceled)
}

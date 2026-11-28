// Copyright (C) 2026 The Nebla contributors
// SPDX-License-Identifier: Apache-2.0

package nebla

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strconv"
	"time"
)

// RetryPolicy controls request retries; MaxAttempts <= 1 disables them.
type RetryPolicy struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
}

// DefaultRetryPolicy is the policy NewClient uses.
var DefaultRetryPolicy = RetryPolicy{MaxAttempts: 5, BaseDelay: 500 * time.Millisecond, MaxDelay: 5 * time.Second}

const retryAfterMax = time.Minute

// retrySleep waits d or until ctx is done; tests replace it.
var retrySleep = func(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// retryTransport retries 429 for any method, and 502/503/504 and transport errors for GET.
type retryTransport struct {
	base   http.RoundTripper
	policy RetryPolicy
}

func (t retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var body []byte
	if req.Body != nil && req.Body != http.NoBody {
		b, err := io.ReadAll(req.Body)
		_ = req.Body.Close()
		if err != nil {
			return nil, err
		}
		body = b
	}
	for attempt := 0; ; attempt++ {
		r := req.Clone(req.Context())
		if body != nil {
			r.Body = io.NopCloser(bytes.NewReader(body))
			r.ContentLength = int64(len(body))
		}
		res, err := t.base.RoundTrip(r)
		if attempt+1 >= t.policy.MaxAttempts || req.Context().Err() != nil || !retryable(req.Method, res, err) {
			return res, err
		}
		d := t.backoff(attempt, res)
		if res != nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10))
			_ = res.Body.Close()
		}
		if err := retrySleep(req.Context(), d); err != nil {
			return nil, err
		}
	}
}

func retryable(method string, res *http.Response, err error) bool {
	if err != nil {
		return method == http.MethodGet
	}
	switch res.StatusCode {
	case http.StatusTooManyRequests:
		return true
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return method == http.MethodGet
	}
	return false
}

func (t retryTransport) backoff(attempt int, res *http.Response) time.Duration {
	if res != nil {
		if s, err := strconv.Atoi(res.Header.Get("Retry-After")); err == nil && s >= 0 {
			return min(time.Duration(s)*time.Second, retryAfterMax)
		}
	}
	d := t.policy.BaseDelay << min(attempt, 30)
	if d <= 0 || d > t.policy.MaxDelay {
		d = t.policy.MaxDelay
	}
	return d
}

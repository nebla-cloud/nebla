// Copyright (C) 2026 The Nebla contributors
// SPDX-License-Identifier: Apache-2.0

package nebla

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestErrorDecoding(t *testing.T) {
	c, f := newFake(t)
	f.handle("GET", "/v1/servers/1", 404, "error_not_found")
	err := c.do(t.Context(), "GET", "/servers/1", nil, nil, nil)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotFound))
	assert.True(t, IsCode(err, CodeLocked, CodeNotFound))
	assert.False(t, IsCode(err, CodeLocked))
	assert.False(t, errors.Is(err, ErrLocked))
	var e *Error
	require.True(t, errors.As(err, &e))
	assert.Equal(t, 404, e.StatusCode)
	assert.Equal(t, "server not found (not_found)", e.Error())
}

func TestNonAPIErrorBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(502)
		_, _ = w.Write([]byte("<html>bad gateway</html>"))
	}))
	defer srv.Close()
	c := NewClient(WithEndpoint(srv.URL), WithToken("tok"), WithRetry(RetryPolicy{MaxAttempts: 1}))
	err := c.do(t.Context(), "GET", "/servers", nil, nil, nil)
	var e *Error
	require.True(t, errors.As(err, &e))
	assert.Equal(t, &Error{StatusCode: 502, Message: "unexpected status 502"}, e)
	assert.Equal(t, "unexpected status 502", e.Error())
	assert.False(t, errors.Is(err, &Error{}))
}

func TestIsCodeNonAPIError(t *testing.T) {
	assert.False(t, IsCode(errors.New("x"), CodeNotFound))
	assert.False(t, IsCode(nil, CodeNotFound))
}

func TestKnownCodesMatchConstants(t *testing.T) {
	src, err := os.ReadFile("errors.go")
	require.NoError(t, err)
	n := len(regexp.MustCompile(`(?m)^\tCode\w+\s+ErrorCode = `).FindAll(src, -1))
	assert.Len(t, KnownCodes(), n)
}

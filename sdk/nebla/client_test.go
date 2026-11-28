// Copyright (C) 2026 The Nebla contributors
// SPDX-License-Identifier: Apache-2.0

package nebla

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequestHeaders(t *testing.T) {
	c, f := newFake(t, WithUserAgent("tf/1"))
	f.handle("POST", "/v1/servers/1", 200, "client_ok")
	var out struct{ ID int }
	require.NoError(t, c.do(t.Context(), "POST", "/servers/1", nil, map[string]string{"a": "b"}, &out))
	r, body := f.last()
	assert.Equal(t, "Bearer tok", r.Header.Get("Authorization"))
	assert.Equal(t, "tf/1 nebla-go/0.4.0", r.Header.Get("User-Agent"))
	assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
	assert.JSONEq(t, `{"a":"b"}`, string(body))
	assert.Equal(t, 1, out.ID)
}

func TestNoBodyNoContentType(t *testing.T) {
	c, f := newFake(t)
	f.handle("GET", "/v1/servers/1", 200, "client_ok")
	require.NoError(t, c.do(t.Context(), "GET", "/servers/1", nil, nil, nil))
	r, _ := f.last()
	assert.Empty(t, r.Header.Get("Content-Type"))
	assert.Equal(t, "nebla-go/0.4.0", r.Header.Get("User-Agent"))
}

func TestEndpointNormalisation(t *testing.T) {
	for _, suffix := range []string{"", "/", "/v1", "/v1/"} {
		t.Run(suffix, func(t *testing.T) {
			var got string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = r.URL.Path
			}))
			defer srv.Close()
			c := NewClient(WithEndpoint(srv.URL+suffix), WithToken("tok"))
			require.NoError(t, c.do(t.Context(), "GET", "/servers/1", nil, nil, nil))
			assert.Equal(t, "/v1/servers/1", got)
		})
	}
}

func TestQuery(t *testing.T) {
	c, f := newFake(t)
	f.handle("GET", "/v1/servers", 200, "client_ok")
	require.NoError(t, c.do(t.Context(), "GET", "/servers", map[string][]string{"page": {"2"}}, nil, nil))
	r, _ := f.last()
	assert.Equal(t, "page=2", r.URL.RawQuery)
}

func TestNonJSONSuccessBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html>hi</html>"))
	}))
	defer srv.Close()
	c := NewClient(WithEndpoint(srv.URL), WithToken("tok"))
	var out struct{}
	err := c.do(t.Context(), "GET", "/servers", nil, nil, &out)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is the endpoint a Nebla panel?")
}

func TestMissingEndpointOrToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("request must not be sent")
	}))
	defer srv.Close()
	err := NewClient(WithToken("tok")).do(t.Context(), "GET", "/servers", nil, nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "endpoint")
	err = NewClient(WithEndpoint(srv.URL)).do(t.Context(), "GET", "/servers", nil, nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "token")
}

func TestPtr(t *testing.T) {
	p := Ptr(3)
	assert.Equal(t, 3, *p)
}

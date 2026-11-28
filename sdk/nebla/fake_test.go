// Copyright (C) 2026 The Nebla contributors
// SPDX-License-Identifier: Apache-2.0

package nebla

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

type fakeRoute struct {
	status  int
	fixture string
	h       http.Handler
}

type fakeReq struct {
	req  *http.Request
	body []byte
}

// fake is an in-process panel serving canned fixtures.
type fake struct {
	t      *testing.T
	mu     sync.Mutex
	routes map[string]fakeRoute
	reqs   []fakeReq
}

// newFake returns a client wired to a fake panel; retries are off by default.
func newFake(t *testing.T, opts ...Option) (*Client, *fake) {
	t.Helper()
	f := &fake{t: t, routes: map[string]fakeRoute{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	base := []Option{WithEndpoint(srv.URL), WithToken("tok"), WithRetry(RetryPolicy{MaxAttempts: 1})}
	return NewClient(append(base, opts...)...), f
}

// handle answers method+path (path includes /v1) with status and testdata/<fixture>.json, or an empty body.
func (f *fake) handle(method, path string, status int, fixture string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.routes[method+" "+path] = fakeRoute{status: status, fixture: fixture}
}

// handleFunc answers method+path (path includes /v1) with h.
func (f *fake) handleFunc(method, path string, h http.HandlerFunc) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.routes[method+" "+path] = fakeRoute{h: h}
}

// last returns the most recent request and its body.
func (f *fake) last() (*http.Request, []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.reqs) == 0 {
		f.t.Fatal("fake: no requests received")
	}
	r := f.reqs[len(f.reqs)-1]
	return r.req, r.body
}

func (f *fake) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.reqs)
}

func (f *fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.reqs = append(f.reqs, fakeReq{r, body})
	rt, ok := f.routes[r.Method+" "+r.URL.Path]
	f.mu.Unlock()
	if !ok {
		f.t.Errorf("fake: unexpected request %s %s", r.Method, r.URL.Path)
		http.Error(w, "unexpected", http.StatusNotImplemented)
		return
	}
	if rt.h != nil {
		rt.h.ServeHTTP(w, r)
		return
	}
	if rt.fixture == "" {
		w.WriteHeader(rt.status)
		return
	}
	data, err := os.ReadFile(filepath.Join("testdata", rt.fixture+".json"))
	if err != nil {
		f.t.Errorf("fake: %v", err)
		http.Error(w, "missing fixture", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(rt.status)
	_, _ = w.Write(data)
}

func bodyString(f *fake) string {
	_, b := f.last()
	return string(b)
}

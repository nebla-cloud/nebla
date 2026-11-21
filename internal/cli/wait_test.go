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

package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/cli/apiclient"
)

func TestWaitSuccess(t *testing.T) {
	h := newHarness(t)
	s := h.Env.CreateServer("web", nil)
	var stderr bytes.Buffer
	got, err := h.api().Wait(context.Background(), &stderr, false, s.ActionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Status != apiclient.ActionStatusSuccess || got[0].Id != s.ActionID {
		t.Fatalf("got=%+v", got)
	}
	want := fmt.Sprintf("Waiting for create_server (%d)\ncreate_server (%d): success\n", s.ActionID, s.ActionID)
	if stderr.String() != want {
		t.Fatalf("stderr=%q want %q", stderr.String(), want)
	}
}

func TestWaitTTY(t *testing.T) {
	h := newHarnessQueueOnly(t)
	s := h.Env.CreateServer("web", nil)
	if err := actions.Succeed(context.Background(), h.Env.App.Ent, s.ActionID); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	if _, err := h.api().Wait(context.Background(), &stderr, true, s.ActionID); err != nil {
		t.Fatal(err)
	}
	if want := fmt.Sprintf("\rWaiting for create_server (%d) ... done\n", s.ActionID); !strings.HasSuffix(stderr.String(), want) {
		t.Fatalf("stderr=%q", stderr.String())
	}
}

func TestWaitError(t *testing.T) {
	h := newHarnessQueueOnly(t)
	s := h.Env.CreateServer("web", nil)
	if err := actions.Fail(context.Background(), h.Env.App.Ent, s.ActionID, actions.CodeFailed, "vm 4242: boom"); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	_, err := h.api().Wait(context.Background(), &stderr, false, s.ActionID)
	want := fmt.Sprintf("action create_server (%d) failed: Couldn't create the server. Try again, or contact your admin. (failed)", s.ActionID)
	if err == nil || err.Error() != want {
		t.Fatalf("err=%v", err)
	}
	if !strings.HasSuffix(stderr.String(), fmt.Sprintf("create_server (%d): error\n", s.ActionID)) {
		t.Fatalf("stderr=%q", stderr.String())
	}
}

type flaky struct {
	n    atomic.Int32
	base http.RoundTripper
}

func (f *flaky) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/v1/actions/") && f.n.Add(1) == 1 {
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	}
	return f.base.RoundTrip(r)
}

func TestWaitRetriesTransient(t *testing.T) {
	h := newHarnessQueueOnly(t)
	s := h.Env.CreateServer("web", nil)
	if err := actions.Succeed(context.Background(), h.Env.App.Ent, s.ActionID); err != nil {
		t.Fatal(err)
	}
	f := &flaky{base: http.DefaultTransport}
	a, err := newAPI(Env{HTTPClient: &http.Client{Transport: f}}, Target{Endpoint: h.URL, Token: h.Token})
	if err != nil {
		t.Fatal(err)
	}
	got, err := a.Wait(context.Background(), io.Discard, false, s.ActionID)
	if err != nil || len(got) != 1 || got[0].Status != apiclient.ActionStatusSuccess {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	if f.n.Load() < 2 {
		t.Fatalf("polls=%d", f.n.Load())
	}
}

func TestWaitInterrupted(t *testing.T) {
	h := newHarnessQueueOnly(t)
	s := h.Env.CreateServer("web", nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := h.api().Wait(ctx, io.Discard, false, s.ActionID)
	if !errors.Is(err, ErrInterrupted) {
		t.Fatalf("err=%v", err)
	}
	var stderr bytes.Buffer
	if code := exitCode(&stderr, err); code != 130 || stderr.String() != fmt.Sprintf("stopped waiting; action %d keeps running\n", s.ActionID) {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
}

func TestWaitTimeout(t *testing.T) {
	h := newHarnessQueueOnly(t)
	s := h.Env.CreateServer("web", nil)
	a := h.api()
	a.Timeout = 20 * pollMax
	_, err := a.Wait(context.Background(), io.Discard, false, s.ActionID)
	if err == nil || errors.Is(err, ErrInterrupted) || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err=%v", err)
	}
}

func TestWaitErrorWithoutCode(t *testing.T) {
	a, _ := stubAPI(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":9,"command":"start_server","status":"error","progress":0,"started":"2026-11-20T00:00:00Z","resources":[],"error":null}`)
	})
	_, err := a.Wait(context.Background(), io.Discard, false, 9)
	if err == nil || err.Error() != "action start_server (9) failed: unknown error" {
		t.Fatalf("err=%v", err)
	}
}

func TestWaitTTYNewlineOnFailure(t *testing.T) {
	calls := 0
	a, _ := stubAPI(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		calls++
		if calls == 1 {
			fmt.Fprint(w, `{"id":9,"command":"start_server","status":"running","progress":60,"started":"2026-11-20T00:00:00Z","resources":[],"error":null}`)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":{"code":"unauthorized","message":"invalid token"}}`)
	})
	var stderr bytes.Buffer
	_, err := a.Wait(context.Background(), &stderr, true, 9)
	if err == nil || !strings.HasSuffix(stderr.String(), "60%\n") {
		t.Fatalf("err=%v stderr=%q", err, stderr.String())
	}
}

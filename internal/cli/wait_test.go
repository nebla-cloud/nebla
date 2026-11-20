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

	"github.com/Hexeption/nebla/internal/actions"
	"github.com/Hexeption/nebla/internal/cli/apiclient"
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
	if err := actions.Fail(context.Background(), h.Env.App.Ent, s.ActionID, "pve_error", "boom"); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	_, err := h.api().Wait(context.Background(), &stderr, false, s.ActionID)
	want := fmt.Sprintf("action create_server (%d) failed: boom (pve_error)", s.ActionID)
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

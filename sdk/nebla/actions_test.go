// Copyright (C) 2026 The Nebla contributors
// SPDX-License-Identifier: Apache-2.0

package nebla

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func actionHandler(statuses ...string) http.HandlerFunc {
	var n atomic.Int32
	return func(w http.ResponseWriter, r *http.Request) {
		i := min(int(n.Add(1))-1, len(statuses)-1)
		id, _ := strconv.Atoi(path.Base(r.URL.Path))
		a := map[string]any{"id": id, "command": "create_server", "status": "running", "progress": i * 50}
		switch statuses[i] {
		case "success":
			a["status"], a["progress"] = "success", 100
		case "error":
			a["status"] = "error"
			a["error"] = map[string]any{"code": "timed_out", "message": "took too long"}
		}
		_ = json.NewEncoder(w).Encode(a)
	}
}

func TestGetAction(t *testing.T) {
	c, f := newFake(t)
	f.handle("GET", "/v1/actions/42", 200, "getAction")
	a, err := c.Actions.Get(context.Background(), 42)
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != 42 || a.Command != "create_server" || a.Status != ActionStatusSuccess || a.Finished == nil || a.Error != nil {
		t.Fatalf("got %+v", a)
	}
}

func TestWaitForSuccess(t *testing.T) {
	c, f := newFake(t)
	f.handleFunc("GET", "/v1/actions/1", actionHandler("running", "running", "success"))
	var seen []int
	run := Action{ID: 1, Status: ActionStatusRunning}
	opts := []WaitOption{PollInterval(time.Millisecond), OnProgress(func(a Action) { seen = append(seen, a.Progress) })}
	if err := c.Actions.WaitFor(context.Background(), []Action{run}, opts...); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 3 || seen[0] != 0 || seen[1] != 50 || seen[2] != 100 {
		t.Fatalf("progress %v", seen)
	}
	f.handleFunc("GET", "/v1/actions/1", actionHandler("running", "success"))
	a, err := c.Actions.Wait(context.Background(), run, PollInterval(time.Millisecond))
	if err != nil || a.Status != ActionStatusSuccess {
		t.Fatalf("got %+v, %v", a, err)
	}
}

func TestWaitForActionError(t *testing.T) {
	c, f := newFake(t)
	f.handleFunc("GET", "/v1/actions/1", actionHandler("running", "error"))
	err := c.Actions.WaitFor(context.Background(), []Action{{ID: 1, Command: "create_server", Status: ActionStatusRunning}}, PollInterval(time.Millisecond))
	var ae *ActionError
	if !errors.As(err, &ae) {
		t.Fatalf("err %v", err)
	}
	if ae.Code != CodeTimedOut || ae.Message != "took too long" || !errors.Is(err, ErrTimedOut) || !IsCode(err, CodeTimedOut) || errors.Is(err, ErrFailed) {
		t.Fatalf("err %v", err)
	}
	if want := "action create_server (1) failed: took too long (timed_out)"; err.Error() != want {
		t.Fatalf("got %q", err.Error())
	}
}

func TestWaitForDeadline(t *testing.T) {
	c, f := newFake(t)
	f.handleFunc("GET", "/v1/actions/7", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":7,"command":"x","status":"running","progress":1}`))
	})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := c.Actions.WaitFor(ctx, []Action{{ID: 7, Status: ActionStatusRunning}}, PollInterval(10*time.Millisecond))
	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "7") {
		t.Fatalf("err %v", err)
	}
}

func TestWaitForFinishedActions(t *testing.T) {
	c, f := newFake(t)
	if err := c.Actions.WaitFor(context.Background(), []Action{{ID: 1, Status: ActionStatusSuccess}}); err != nil {
		t.Fatal(err)
	}
	err := c.Actions.WaitFor(context.Background(), []Action{{ID: 2, Command: "c", Status: ActionStatusError, Error: &ActionErrorDetail{Code: "unreachable", Message: "m"}}})
	var ae *ActionError
	if !errors.As(err, &ae) || !errors.Is(err, ErrUnreachable) {
		t.Fatalf("err %v", err)
	}
	if f.count() != 0 {
		t.Fatalf("expected no requests, got %d", f.count())
	}
}

func TestWaitForOrder(t *testing.T) {
	c, f := newFake(t)
	f.handleFunc("GET", "/v1/actions/1", actionHandler("running", "success"))
	f.handleFunc("GET", "/v1/actions/2", actionHandler("success"))
	var order []string
	opts := []WaitOption{PollInterval(time.Millisecond), OnProgress(func(a Action) { order = append(order, string(rune('0'+a.ID))+string(a.Status[0])) })}
	two := []Action{{ID: 1, Status: ActionStatusRunning}, {ID: 2, Status: ActionStatusRunning}}
	if err := c.Actions.WaitFor(context.Background(), two, opts...); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(order, ","); got != "1r,1s,2s" {
		t.Fatalf("order %s", got)
	}
}

func TestWaitForFirstFailureStops(t *testing.T) {
	c, f := newFake(t)
	f.handleFunc("GET", "/v1/actions/1", actionHandler("error"))
	err := c.Actions.WaitFor(context.Background(), []Action{{ID: 1, Status: ActionStatusRunning}, {ID: 2, Status: ActionStatusRunning}}, PollInterval(time.Millisecond))
	if !IsCode(err, CodeTimedOut) || f.count() != 1 {
		t.Fatalf("err %v, requests %d", err, f.count())
	}
}

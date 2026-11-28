// Copyright (C) 2026 The Nebla contributors
// SPDX-License-Identifier: Apache-2.0

package nebla

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/nebla-cloud/nebla/sdk/nebla/internal/wire"
)

// ActionStatus is the state of an action.
type ActionStatus string

// Action statuses.
const (
	ActionStatusRunning ActionStatus = "running"
	ActionStatusSuccess ActionStatus = "success"
	ActionStatusError   ActionStatus = "error"
)

// ActionResource is a resource an action touches.
type ActionResource struct {
	ID   int64
	Type string
}

// ActionErrorDetail is the failure reported by an action.
type ActionErrorDetail struct {
	Code, Message string
}

// Action is an asynchronous operation.
type Action struct {
	ID        int64
	Command   string
	Status    ActionStatus
	Progress  int
	Resources []ActionResource
	Error     *ActionErrorDetail
	Started   time.Time
	Finished  *time.Time
}

// ActionError is returned when an action finishes with status error.
type ActionError struct {
	Action  Action
	Code    ErrorCode
	Message string
}

func (e *ActionError) Error() string {
	return fmt.Sprintf("action %s (%d) failed: %s (%s)", e.Action.Command, e.Action.ID, e.Message, e.Code)
}

// APICode returns the action's error code.
func (e *ActionError) APICode() ErrorCode { return e.Code }

// Is reports whether target is an *Error with the same non-empty code.
func (e *ActionError) Is(target error) bool {
	t, ok := target.(*Error)
	return ok && t.Code != "" && t.Code == e.Code
}

func newActionError(a Action) *ActionError {
	ae := &ActionError{Action: a, Code: CodeFailed, Message: "action failed"}
	if a.Error != nil {
		if a.Error.Code != "" {
			ae.Code = ErrorCode(a.Error.Code)
		}
		if a.Error.Message != "" {
			ae.Message = a.Error.Message
		}
	}
	return ae
}

func actionFromWire(w wire.Action) Action {
	a := Action{ID: w.ID, Command: w.Command, Status: ActionStatus(w.Status), Progress: w.Progress, Started: w.Started, Finished: w.Finished}
	for _, r := range w.Resources {
		a.Resources = append(a.Resources, ActionResource{ID: r.ID, Type: r.Type})
	}
	if w.Error != nil {
		a.Error = &ActionErrorDetail{Code: w.Error.Code, Message: w.Error.Message}
	}
	return a
}

func actionsFromWire(ws []wire.Action) []Action {
	out := make([]Action, 0, len(ws))
	for _, w := range ws {
		out = append(out, actionFromWire(w))
	}
	return out
}

// actionReply sends a request whose reply is {action}.
func actionReply(ctx context.Context, c *Client, method, path string, body any) (*Action, error) {
	var w struct {
		Action wire.Action `json:"action"`
	}
	if err := c.do(ctx, method, path, nil, body, &w); err != nil {
		return nil, err
	}
	a := actionFromWire(w.Action)
	return &a, nil
}

// actionsReply sends a request whose reply is {actions}.
func actionsReply(ctx context.Context, c *Client, method, path string, body any) ([]Action, error) {
	var w struct {
		Actions []wire.Action `json:"actions"`
	}
	if err := c.do(ctx, method, path, nil, body, &w); err != nil {
		return nil, err
	}
	return actionsFromWire(w.Actions), nil
}

// ActionListOpts filters a list of actions; Status values are ORed.
type ActionListOpts struct {
	ListOpts
	Status []ActionStatus
	Sort   string
}

func (o ActionListOpts) values() url.Values {
	q := o.ListOpts.values()
	for _, st := range o.Status {
		q.Add("status", string(st))
	}
	setIf(q, "sort", o.Sort)
	return q
}

// ActionsService works with actions.
type ActionsService struct{ c *Client }

// List returns one page of the project's actions.
func (s *ActionsService) List(ctx context.Context, opts ActionListOpts) ([]Action, Meta, error) {
	return listPage(ctx, s.c, "/actions", opts.values(), "actions", actionFromWire)
}

// All returns every action matching opts.
func (s *ActionsService) All(ctx context.Context, opts ActionListOpts) ([]Action, error) {
	return listAll(ctx, s.c, "/actions", opts.values(), "actions", actionFromWire)
}

// Get returns one action.
func (s *ActionsService) Get(ctx context.Context, id int64) (*Action, error) {
	var w wire.Action
	if err := s.c.do(ctx, "GET", "/actions/"+strconv.FormatInt(id, 10), nil, nil, &w); err != nil {
		return nil, err
	}
	a := actionFromWire(w)
	return &a, nil
}

type waitConfig struct {
	interval   time.Duration
	onProgress func(Action)
}

// WaitOption configures WaitFor and Wait.
type WaitOption func(*waitConfig)

// PollInterval fixes the polling interval instead of backing off from 1s to 5s.
func PollInterval(d time.Duration) WaitOption { return func(c *waitConfig) { c.interval = d } }

// OnProgress calls fn with every polled state of an action.
func OnProgress(fn func(Action)) WaitOption { return func(c *waitConfig) { c.onProgress = fn } }

// WaitFor waits for each action in order; it returns an *ActionError for the first one that fails.
func (s *ActionsService) WaitFor(ctx context.Context, actions []Action, opts ...WaitOption) error {
	for _, a := range actions {
		if _, err := s.Wait(ctx, a, opts...); err != nil {
			return err
		}
	}
	return nil
}

// Wait waits for one action and returns its final state.
func (s *ActionsService) Wait(ctx context.Context, a Action, opts ...WaitOption) (Action, error) {
	cfg := waitConfig{}
	for _, o := range opts {
		o(&cfg)
	}
	delay := time.Second
	if cfg.interval > 0 {
		delay = cfg.interval
	}
	for {
		switch a.Status {
		case ActionStatusSuccess:
			return a, nil
		case ActionStatusError:
			return a, newActionError(a)
		}
		cur, err := s.Get(ctx, a.ID)
		if err != nil {
			return a, fmt.Errorf("waiting for action %d: %w", a.ID, err)
		}
		a = *cur
		if cfg.onProgress != nil {
			cfg.onProgress(a)
		}
		if a.Status != ActionStatusRunning && a.Status != ActionStatusSuccess && a.Status != ActionStatusError {
			return a, fmt.Errorf("action %d has unknown status %q", a.ID, string(a.Status))
		}
		if a.Status != ActionStatusRunning {
			continue
		}
		t := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			t.Stop()
			return a, fmt.Errorf("waiting for action %d: %w", a.ID, ctx.Err())
		case <-t.C:
		}
		if cfg.interval == 0 {
			delay = min(delay*3/2, 5*time.Second)
		}
	}
}

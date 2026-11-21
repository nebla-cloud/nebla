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

// Package actions records the progress and outcome of asynchronous changes.
package actions

import (
	"context"
	"net/http"
	"time"

	"github.com/nebla-cloud/nebla/internal/apierr"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/action"
	"github.com/nebla-cloud/nebla/internal/ent/predicate"
	"github.com/nebla-cloud/nebla/internal/ent/schema"
)

const (
	StatusRunning = "running"
	StatusSuccess = "success"
	StatusError   = "error"
)

// Resource is one resource an action touches, e.g. {ID: 7, Type: "server"}.
type Resource = schema.ActionResource

// Error is a failed action's outcome; Detail is admin-only technical text.
type Error struct {
	Code    string
	Message string
	Detail  string
}

type Action struct {
	ID        int64
	ProjectID int64
	// ProjectName is set only by the admin reads, GetAny and ListAll.
	ProjectName string
	Command     string
	Status      string
	Progress    int
	Resources   []Resource
	Error       *Error
	Started     time.Time
	Finished    *time.Time
	RiverJobID  *int64
}

var errNotFound = apierr.New(http.StatusNotFound, "not_found", "not found")

func toAction(a *ent.Action) Action {
	out := Action{ID: a.ID, ProjectID: a.ProjectID, Command: a.Command, Status: string(a.Status), Progress: a.Progress,
		Resources: a.Resources, Started: a.Started, Finished: a.Finished, RiverJobID: a.RiverJobID}
	if out.Resources == nil {
		out.Resources = []Resource{}
	}
	if a.ErrorCode != nil {
		code := normalizeCode(*a.ErrorCode)
		out.Error = &Error{Code: code, Message: Message(a.Command, code), Detail: deref(a.ErrorMessage)}
	}
	return out
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// now is truncated to Postgres precision so returned times match stored ones.
func now() time.Time { return time.Now().Truncate(time.Microsecond) }

// Create inserts a running action; c may be the root client or one bound to the caller's tx.
func Create(ctx context.Context, c *ent.Client, projectID int64, command string, resources []Resource) (Action, error) {
	if resources == nil {
		resources = []Resource{}
	}
	a, err := c.Action.Create().SetProjectID(projectID).SetCommand(command).SetResources(resources).SetStarted(now()).Save(ctx)
	if err != nil {
		return Action{}, err
	}
	return toAction(a), nil
}

// AttachJob links an action to the River job that runs it.
func AttachJob(ctx context.Context, c *ent.Client, id, jobID int64) error {
	n, err := c.Action.Update().Where(action.ID(id)).SetRiverJobID(jobID).Save(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		return errNotFound
	}
	return nil
}

// update applies u to a still-running action; a finished action is left alone and nil returned.
func update(ctx context.Context, c *ent.Client, id int64, u *ent.ActionUpdate) error {
	n, err := u.Where(action.ID(id), action.FinishedIsNil()).Save(ctx)
	if err != nil || n > 0 {
		return err
	}
	ok, err := c.Action.Query().Where(action.ID(id)).Exist(ctx)
	if err != nil {
		return err
	}
	if !ok {
		return errNotFound
	}
	return nil
}

// Progress raises progress to pct clamped to 0-100; it never lowers it.
func Progress(ctx context.Context, c *ent.Client, id int64, pct int) error {
	pct = min(max(pct, 0), 100)
	return update(ctx, c, id, c.Action.Update().Where(action.ProgressLT(pct)).SetProgress(pct))
}

func Succeed(ctx context.Context, c *ent.Client, id int64) error {
	return update(ctx, c, id, c.Action.Update().SetStatus(action.StatusSuccess).SetProgress(100).SetFinished(now()))
}

// Fail finishes an action with a Code* kind and admin-only detail.
func Fail(ctx context.Context, c *ent.Client, id int64, code, detail string) error {
	return update(ctx, c, id, c.Action.Update().SetStatus(action.StatusError).SetErrorCode(code).SetErrorMessage(detail).SetFinished(now()))
}

// GetAny returns any project's action, for admins.
func GetAny(ctx context.Context, c *ent.Client, id int64) (Action, error) {
	a, err := c.Action.Query().Where(action.ID(id)).WithProject().Only(ctx)
	if ent.IsNotFound(err) {
		return Action{}, errNotFound
	}
	if err != nil {
		return Action{}, err
	}
	return withName(a), nil
}

func withName(a *ent.Action) Action {
	out := toAction(a)
	if p := a.Edges.Project; p != nil {
		out.ProjectName = p.Name
	}
	return out
}

// Get returns a project's action; other projects' actions are not_found.
func Get(ctx context.Context, c *ent.Client, projectID, id int64) (Action, error) {
	a, err := c.Action.Query().Where(action.ID(id), action.ProjectID(projectID)).Only(ctx)
	if ent.IsNotFound(err) {
		return Action{}, errNotFound
	}
	if err != nil {
		return Action{}, err
	}
	return toAction(a), nil
}

// ListFilter narrows List; empty Statuses means all, and the default order is newest first.
type ListFilter struct {
	Statuses  []string
	Ascending bool
	// Where adds extra predicates, e.g. a resource filter.
	Where []predicate.Action
}

// ValidStatus reports whether s is an action status.
func ValidStatus(s string) bool { return action.StatusValidator(action.Status(s)) == nil }

func List(ctx context.Context, c *ent.Client, projectID int64, f ListFilter, page, perPage int) ([]Action, int, error) {
	return list(ctx, c.Action.Query(), &projectID, f, page, perPage, toAction)
}

// ListAll lists actions across projects, or one project's when projectID is set; for admins.
func ListAll(ctx context.Context, c *ent.Client, projectID *int64, f ListFilter, page, perPage int) ([]Action, int, error) {
	return list(ctx, c.Action.Query().WithProject(), projectID, f, page, perPage, withName)
}

func list(ctx context.Context, q *ent.ActionQuery, projectID *int64, f ListFilter, page, perPage int, conv func(*ent.Action) Action) ([]Action, int, error) {
	q = q.Where(f.Where...)
	if projectID != nil {
		q = q.Where(action.ProjectID(*projectID))
	}
	if len(f.Statuses) > 0 {
		st := make([]action.Status, len(f.Statuses))
		for i, s := range f.Statuses {
			st[i] = action.Status(s)
		}
		q = q.Where(action.StatusIn(st...))
	}
	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	order := ent.Desc(action.FieldID)
	if f.Ascending {
		order = ent.Asc(action.FieldID)
	}
	page, perPage = max(page, 1), max(perPage, 1)
	rows, err := q.Order(order).Offset((page - 1) * perPage).Limit(perPage).All(ctx)
	if err != nil {
		return nil, 0, err
	}
	out := make([]Action, len(rows))
	for i, r := range rows {
		out[i] = conv(r)
	}
	return out, total, nil
}

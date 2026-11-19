// Package actions records the progress and outcome of asynchronous changes.
package actions

import (
	"context"
	"net/http"
	"time"

	"github.com/Hexeption/nebla/internal/apierr"
	"github.com/Hexeption/nebla/internal/ent"
	"github.com/Hexeption/nebla/internal/ent/action"
	"github.com/Hexeption/nebla/internal/ent/predicate"
	"github.com/Hexeption/nebla/internal/ent/schema"
)

const (
	StatusRunning = "running"
	StatusSuccess = "success"
	StatusError   = "error"
)

// Resource is one resource an action touches, e.g. {ID: 7, Type: "server"}.
type Resource = schema.ActionResource

type Error struct {
	Code    string
	Message string
}

type Action struct {
	ID         int64
	ProjectID  int64
	Command    string
	Status     string
	Progress   int
	Resources  []Resource
	Error      *Error
	Started    time.Time
	Finished   *time.Time
	RiverJobID *int64
}

var errNotFound = apierr.New(http.StatusNotFound, "not_found", "not found")

func toAction(a *ent.Action) Action {
	out := Action{ID: a.ID, ProjectID: a.ProjectID, Command: a.Command, Status: string(a.Status), Progress: a.Progress,
		Resources: a.Resources, Started: a.Started, Finished: a.Finished, RiverJobID: a.RiverJobID}
	if out.Resources == nil {
		out.Resources = []Resource{}
	}
	if a.ErrorCode != nil {
		out.Error = &Error{Code: *a.ErrorCode, Message: deref(a.ErrorMessage)}
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

func Fail(ctx context.Context, c *ent.Client, id int64, code, msg string) error {
	return update(ctx, c, id, c.Action.Update().SetStatus(action.StatusError).SetErrorCode(code).SetErrorMessage(msg).SetFinished(now()))
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
	q := c.Action.Query().Where(action.ProjectID(projectID)).Where(f.Where...)
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
		out[i] = toAction(r)
	}
	return out, total, nil
}

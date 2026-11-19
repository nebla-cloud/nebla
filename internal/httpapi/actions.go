package httpapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/Hexeption/nebla/internal/actions"
	"github.com/Hexeption/nebla/internal/apierr"
	"github.com/Hexeption/nebla/internal/apigen"
)

func apiAction(a actions.Action) apigen.Action {
	out := apigen.Action{Id: a.ID, Command: a.Command, Status: apigen.ActionStatus(a.Status), Progress: a.Progress,
		Started: a.Started, Finished: a.Finished}
	out.Resources = make([]struct {
		Id   int64  `json:"id"`
		Type string `json:"type"`
	}, len(a.Resources))
	for i, r := range a.Resources {
		out.Resources[i].Id, out.Resources[i].Type = r.ID, r.Type
	}
	if a.Error != nil {
		out.Error = &struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}{Code: a.Error.Code, Message: a.Error.Message}
	}
	return out
}

func invalidInput(msg string) error {
	return apierr.New(http.StatusUnprocessableEntity, "invalid_input", msg)
}

// actionFilter parses the status (repeatable or comma separated) and sort query parameters.
func actionFilter(status *[]string, sort *string) (actions.ListFilter, error) {
	var f actions.ListFilter
	for _, v := range deref(status) {
		for s := range strings.SplitSeq(v, ",") {
			if s = strings.TrimSpace(s); s == "" {
				continue
			}
			if !actions.ValidStatus(s) {
				return f, invalidInput("status must be running, success or error")
			}
			f.Statuses = append(f.Statuses, s)
		}
	}
	switch deref(sort) {
	case "", "id:desc":
	case "id", "id:asc":
		f.Ascending = true
	default:
		return f, invalidInput("sort must be id, id:asc or id:desc")
	}
	return f, nil
}

func (s *server) GetActions(ctx context.Context, req apigen.GetActionsRequestObject) (apigen.GetActionsResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	f, err := actionFilter(req.Params.Status, req.Params.Sort)
	if err != nil {
		return nil, err
	}
	page, perPage := pageParams(req.Params.Page, req.Params.PerPage)
	as, total, err := actions.List(ctx, s.deps.Ent, sc.ProjectID, f, page, perPage)
	if err != nil {
		return nil, err
	}
	out := apigen.GetActions200JSONResponse{Actions: make([]apigen.Action, len(as)), Meta: meta(page, perPage, total)}
	for i, a := range as {
		out.Actions[i] = apiAction(a)
	}
	return out, nil
}

func (s *server) GetAction(ctx context.Context, req apigen.GetActionRequestObject) (apigen.GetActionResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	a, err := actions.Get(ctx, s.deps.Ent, sc.ProjectID, req.Id)
	if err != nil {
		return nil, err
	}
	return apigen.GetAction200JSONResponse(apiAction(a)), nil
}

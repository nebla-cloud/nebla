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

package httpapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/apierr"
	"github.com/nebla-cloud/nebla/internal/apigen"
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

func apiAdminAction(a actions.Action) apigen.AdminAction {
	p := apiAction(a)
	out := apigen.AdminAction{Id: p.Id, ProjectId: a.ProjectID, ProjectName: a.ProjectName, Command: p.Command, Status: apigen.AdminActionStatus(p.Status),
		Progress: p.Progress, Resources: p.Resources, Started: p.Started, Finished: p.Finished}
	if a.Error != nil {
		out.Error = &struct {
			Code    string `json:"code"`
			Detail  string `json:"detail"`
			Message string `json:"message"`
		}{Code: a.Error.Code, Detail: a.Error.Detail, Message: a.Error.Message}
	}
	return out
}

func (s *server) GetAdminActions(ctx context.Context, req apigen.GetAdminActionsRequestObject) (apigen.GetAdminActionsResponseObject, error) {
	f, err := actionFilter(req.Params.Status, req.Params.Sort)
	if err != nil {
		return nil, err
	}
	page, perPage := pageParams(req.Params.Page, req.Params.PerPage)
	as, total, err := actions.ListAll(ctx, s.deps.Ent, req.Params.ProjectId, f, page, perPage)
	if err != nil {
		return nil, err
	}
	out := apigen.GetAdminActions200JSONResponse{Actions: make([]apigen.AdminAction, len(as)), Meta: meta(page, perPage, total)}
	for i, a := range as {
		out.Actions[i] = apiAdminAction(a)
	}
	return out, nil
}

func (s *server) GetAdminAction(ctx context.Context, req apigen.GetAdminActionRequestObject) (apigen.GetAdminActionResponseObject, error) {
	a, err := actions.GetAny(ctx, s.deps.Ent, req.Id)
	if err != nil {
		return nil, err
	}
	return apigen.GetAdminAction200JSONResponse(apiAdminAction(a)), nil
}

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

	"github.com/nebla-cloud/nebla/internal/apigen"
	"github.com/nebla-cloud/nebla/internal/floatingips"
)

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func apiFloatingIP(f floatingips.FloatingIP) apigen.FloatingIp {
	out := apigen.FloatingIp{Id: f.ID, Name: f.Name, Description: f.Description, Labels: f.Labels, Type: apigen.FloatingIpType(f.Family),
		Ip: f.IP, Location: apigen.LocationRef{Id: f.Location.ID, Name: f.Location.Name, Description: f.Location.Description},
		Server: f.ServerID, Status: apigen.FloatingIpStatus(f.Status), Created: f.Created}
	if g := f.Guest; g != nil {
		out.GuestConfig = &apigen.GuestConfig{Server: nullable(g.Server), Apply: nullable(g.Apply), Persist: nullable(g.Persist),
			PreviousServer: nullable(g.PreviousServer), Remove: nullable(g.Remove)}
	}
	return out
}

func (s *server) GetFloatingIps(ctx context.Context, req apigen.GetFloatingIpsRequestObject) (apigen.GetFloatingIpsResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	p := req.Params
	page, perPage := pageParams(p.Page, p.PerPage)
	f := floatingips.ListFilter{Name: deref(p.Name), LabelSelector: deref(p.LabelSelector), Server: deref(p.Server), Location: deref(p.Location)}
	fs, total, err := s.floatingIPs.List(ctx, sc.ProjectID, f, page, perPage)
	if err != nil {
		return nil, err
	}
	out := apigen.GetFloatingIps200JSONResponse{FloatingIps: make([]apigen.FloatingIp, len(fs)), Meta: meta(page, perPage, total)}
	for i, v := range fs {
		out.FloatingIps[i] = apiFloatingIP(v)
	}
	return out, nil
}

func (s *server) PostFloatingIps(ctx context.Context, req apigen.PostFloatingIpsRequestObject) (apigen.PostFloatingIpsResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	b := req.Body
	in := floatingips.CreateInput{Name: deref(b.Name), Description: deref(b.Description), Family: string(b.Type),
		Location: deref(b.Location), Server: deref(b.Server)}
	if b.Labels != nil {
		in.Labels = *b.Labels
	}
	f, a, err := s.floatingIPs.Create(ctx, sc.ProjectID, in)
	if err != nil {
		return nil, err
	}
	out := apigen.PostFloatingIps201JSONResponse{FloatingIp: apiFloatingIP(f)}
	if a != nil {
		act := apiAction(*a)
		out.Action = &act
	}
	return out, nil
}

func (s *server) GetFloatingIp(ctx context.Context, req apigen.GetFloatingIpRequestObject) (apigen.GetFloatingIpResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	f, err := s.floatingIPs.Get(ctx, sc.ProjectID, req.Id)
	if err != nil {
		return nil, err
	}
	return apigen.GetFloatingIp200JSONResponse(apiFloatingIP(f)), nil
}

func (s *server) PutFloatingIp(ctx context.Context, req apigen.PutFloatingIpRequestObject) (apigen.PutFloatingIpResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	in := floatingips.UpdateInput{Name: req.Body.Name, Description: req.Body.Description}
	if req.Body.Labels != nil {
		l := map[string]string(*req.Body.Labels)
		in.Labels = &l
	}
	f, err := s.floatingIPs.Update(ctx, sc.ProjectID, req.Id, in)
	if err != nil {
		return nil, err
	}
	return apigen.PutFloatingIp200JSONResponse(apiFloatingIP(f)), nil
}

func (s *server) DeleteFloatingIp(ctx context.Context, req apigen.DeleteFloatingIpRequestObject) (apigen.DeleteFloatingIpResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	a, err := s.floatingIPs.Delete(ctx, sc.ProjectID, req.Id)
	if err != nil {
		return nil, err
	}
	return apigen.DeleteFloatingIp200JSONResponse{Action: apiAction(a)}, nil
}

func (s *server) GetFloatingIpActions(ctx context.Context, req apigen.GetFloatingIpActionsRequestObject) (apigen.GetFloatingIpActionsResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	f, err := actionFilter(req.Params.Status, req.Params.Sort)
	if err != nil {
		return nil, err
	}
	page, perPage := pageParams(req.Params.Page, req.Params.PerPage)
	as, total, err := s.floatingIPs.Actions(ctx, sc.ProjectID, req.Id, f, page, perPage)
	if err != nil {
		return nil, err
	}
	out := apigen.GetFloatingIpActions200JSONResponse{Actions: make([]apigen.Action, len(as)), Meta: meta(page, perPage, total)}
	for i, a := range as {
		out.Actions[i] = apiAction(a)
	}
	return out, nil
}

func (s *server) AssignFloatingIp(ctx context.Context, req apigen.AssignFloatingIpRequestObject) (apigen.AssignFloatingIpResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	a, err := s.floatingIPs.Assign(ctx, sc.ProjectID, req.Id, req.Body.Server)
	if err != nil {
		return nil, err
	}
	return apigen.AssignFloatingIp201JSONResponse{ActionCreatedJSONResponse: apigen.ActionCreatedJSONResponse{Action: apiAction(a)}}, nil
}

func (s *server) UnassignFloatingIp(ctx context.Context, req apigen.UnassignFloatingIpRequestObject) (apigen.UnassignFloatingIpResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	a, err := s.floatingIPs.Unassign(ctx, sc.ProjectID, req.Id)
	if err != nil {
		return nil, err
	}
	return apigen.UnassignFloatingIp201JSONResponse{ActionCreatedJSONResponse: apigen.ActionCreatedJSONResponse{Action: apiAction(a)}}, nil
}

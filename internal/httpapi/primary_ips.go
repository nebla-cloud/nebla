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

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/apigen"
	"github.com/nebla-cloud/nebla/internal/primaryips"
)

func apiPrimaryIP(p primaryips.PrimaryIP) apigen.PrimaryIp {
	out := apigen.PrimaryIp{Id: p.ID, Name: p.Name, Description: p.Description, Labels: p.Labels, Type: apigen.PrimaryIpType(p.Type),
		Ip: p.IP, Location: apigen.LocationRef{Id: p.Location.ID, Name: p.Location.Name, Description: p.Location.Description},
		AutoDelete: p.AutoDelete, Status: apigen.PrimaryIpStatus(p.Status), Created: p.Created,
		Protection: apigen.Protection{Delete: p.DeleteProtected}}
	if p.Server != nil {
		out.Server = &struct {
			Id   int64  `json:"id"`
			Name string `json:"name"`
		}{p.Server.ID, p.Server.Name}
	}
	return out
}

func (s *server) GetPrimaryIps(ctx context.Context, req apigen.GetPrimaryIpsRequestObject) (apigen.GetPrimaryIpsResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	p := req.Params
	page, perPage := pageParams(p.Page, p.PerPage)
	f := primaryips.ListFilter{Name: deref(p.Name), LabelSelector: deref(p.LabelSelector), Server: deref(p.Server), Location: deref(p.Location)}
	if p.Type != nil {
		f.Type = string(*p.Type)
	}
	ps, total, err := s.primaryIPs.List(ctx, sc.ProjectID, f, page, perPage)
	if err != nil {
		return nil, err
	}
	out := apigen.GetPrimaryIps200JSONResponse{PrimaryIps: make([]apigen.PrimaryIp, len(ps)), Meta: meta(page, perPage, total)}
	for i, v := range ps {
		out.PrimaryIps[i] = apiPrimaryIP(v)
	}
	return out, nil
}

func (s *server) PostPrimaryIps(ctx context.Context, req apigen.PostPrimaryIpsRequestObject) (apigen.PostPrimaryIpsResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	b := req.Body
	in := primaryips.CreateInput{Type: string(b.Type), Location: b.Location, Name: deref(b.Name), Description: deref(b.Description), AutoDelete: b.AutoDelete}
	if b.Labels != nil {
		in.Labels = *b.Labels
	}
	if b.Protection != nil {
		in.DeleteProtected = deref(b.Protection.Delete)
	}
	p, err := s.primaryIPs.Create(ctx, sc.ProjectID, in)
	if err != nil {
		return nil, err
	}
	return apigen.PostPrimaryIps201JSONResponse{PrimaryIp: apiPrimaryIP(p)}, nil
}

func (s *server) GetPrimaryIp(ctx context.Context, req apigen.GetPrimaryIpRequestObject) (apigen.GetPrimaryIpResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	p, err := s.primaryIPs.Get(ctx, sc.ProjectID, req.Id)
	if err != nil {
		return nil, err
	}
	return apigen.GetPrimaryIp200JSONResponse(apiPrimaryIP(p)), nil
}

func (s *server) PutPrimaryIp(ctx context.Context, req apigen.PutPrimaryIpRequestObject) (apigen.PutPrimaryIpResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	in := primaryips.UpdateInput{Name: req.Body.Name, Description: req.Body.Description, AutoDelete: req.Body.AutoDelete}
	if req.Body.Labels != nil {
		l := map[string]string(*req.Body.Labels)
		in.Labels = &l
	}
	if p := req.Body.Protection; p != nil {
		in.DeleteProtected = p.Delete
	}
	p, err := s.primaryIPs.Update(ctx, sc.ProjectID, req.Id, in)
	if err != nil {
		return nil, err
	}
	return apigen.PutPrimaryIp200JSONResponse(apiPrimaryIP(p)), nil
}

func (s *server) DeletePrimaryIp(ctx context.Context, req apigen.DeletePrimaryIpRequestObject) (apigen.DeletePrimaryIpResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.primaryIPs.Delete(ctx, sc.ProjectID, req.Id); err != nil {
		return nil, err
	}
	return apigen.DeletePrimaryIp204Response{}, nil
}

func (s *server) GetPrimaryIpActions(ctx context.Context, req apigen.GetPrimaryIpActionsRequestObject) (apigen.GetPrimaryIpActionsResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	f, err := actionFilter(req.Params.Status, req.Params.Sort)
	if err != nil {
		return nil, err
	}
	page, perPage := pageParams(req.Params.Page, req.Params.PerPage)
	as, total, err := s.primaryIPs.Actions(ctx, sc.ProjectID, req.Id, f, page, perPage)
	if err != nil {
		return nil, err
	}
	out := apigen.GetPrimaryIpActions200JSONResponse{Actions: make([]apigen.Action, len(as)), Meta: meta(page, perPage, total)}
	for i, a := range as {
		out.Actions[i] = apiAction(a)
	}
	return out, nil
}

func (s *server) AssignPrimaryIp(ctx context.Context, req apigen.AssignPrimaryIpRequestObject) (apigen.AssignPrimaryIpResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	a, warns, err := s.primaryIPs.Assign(ctx, sc.ProjectID, req.Id, req.Body.Server)
	if err != nil {
		return nil, err
	}
	return apigen.AssignPrimaryIp201JSONResponse{PrimaryIpActionCreatedJSONResponse: primaryIPActionCreated(a, warns)}, nil
}

func (s *server) UnassignPrimaryIp(ctx context.Context, req apigen.UnassignPrimaryIpRequestObject) (apigen.UnassignPrimaryIpResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	a, warns, err := s.primaryIPs.Unassign(ctx, sc.ProjectID, req.Id)
	if err != nil {
		return nil, err
	}
	return apigen.UnassignPrimaryIp201JSONResponse{PrimaryIpActionCreatedJSONResponse: primaryIPActionCreated(a, warns)}, nil
}

func primaryIPActionCreated(a actions.Action, warns []string) apigen.PrimaryIpActionCreatedJSONResponse {
	if warns == nil {
		warns = []string{}
	}
	return apigen.PrimaryIpActionCreatedJSONResponse{Action: apiAction(a), Warnings: warns}
}

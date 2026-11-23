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
	"strings"

	"github.com/nebla-cloud/nebla/internal/apigen"
	"github.com/nebla-cloud/nebla/internal/floatingips"
	"github.com/nebla-cloud/nebla/internal/pve"
	"github.com/nebla-cloud/nebla/internal/servers"
)

// apiServer maps s; vols are its attached volume ids and fips its floating IPs, nil for none.
func apiServer(s servers.Server, vols []int64, fips []floatingips.Ref) apigen.Server {
	if vols == nil {
		vols = []int64{}
	}
	out := apigen.Server{Id: s.ID, Name: s.Name, Status: apigen.ServerStatus(s.Status), Location: apigen.LocationRef{Id: s.Location.ID, Name: s.Location.Name, Description: s.Location.Description},
		ServerType: apiServerType(s.Type), Image: apiImage(s.Image), Labels: s.Labels, DiskGb: s.DiskGB, Volumes: vols, Created: s.CreatedAt,
		Firewalls: make([]apigen.ServerFirewall, len(s.Firewalls))}
	out.PublicNet.FloatingIps = make([]struct {
		Id int64  `json:"id"`
		Ip string `json:"ip"`
	}, len(fips))
	for i, f := range fips {
		out.PublicNet.FloatingIps[i].Id, out.PublicNet.FloatingIps[i].Ip = f.ID, f.IP
	}
	for i, f := range s.Firewalls {
		out.Firewalls[i] = apigen.ServerFirewall{Id: f.FirewallID, Name: f.Name, Via: f.Via}
	}
	if s.IPv4.IsValid() {
		out.PublicNet.Ipv4 = &apigen.ServerIp{Ip: s.IPv4.String()}
	}
	if s.IPv6.IsValid() {
		out.PublicNet.Ipv6 = &apigen.ServerIp{Ip: s.IPv6.String()}
	}
	return out
}

// serverExtras returns srv's attached volume ids and floating IPs.
func (s *server) serverExtras(ctx context.Context, srv servers.Server) ([]int64, []floatingips.Ref, error) {
	m, err := s.volumes.ServerVolumes(ctx, []int64{srv.ID})
	if err != nil {
		return nil, nil, err
	}
	f, err := s.floatingIPs.ServerRefs(ctx, []int64{srv.ID})
	return m[srv.ID], f[srv.ID], err
}

func splitList(vs *[]string) []string {
	var out []string
	for _, v := range deref(vs) {
		for p := range strings.SplitSeq(v, ",") {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}

func (s *server) GetServers(ctx context.Context, req apigen.GetServersRequestObject) (apigen.GetServersResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	page, perPage := pageParams(req.Params.Page, req.Params.PerPage)
	f := servers.ListFilter{Name: deref(req.Params.Name), Statuses: splitList(req.Params.Status), LabelSelector: deref(req.Params.LabelSelector)}
	ss, total, err := s.servers.List(ctx, sc.ProjectID, f, page, perPage)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, len(ss))
	for i, v := range ss {
		ids[i] = v.ID
	}
	vols, err := s.volumes.ServerVolumes(ctx, ids)
	if err != nil {
		return nil, err
	}
	fips, err := s.floatingIPs.ServerRefs(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := apigen.GetServers200JSONResponse{Servers: make([]apigen.Server, len(ss)), Meta: meta(page, perPage, total)}
	for i, v := range ss {
		out.Servers[i] = apiServer(v, vols[v.ID], fips[v.ID])
	}
	return out, nil
}

func (s *server) PostServers(ctx context.Context, req apigen.PostServersRequestObject) (apigen.PostServersResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	b := req.Body
	in := servers.CreateInput{Name: b.Name, ServerType: b.ServerType, Image: b.Image, Location: b.Location,
		SSHKeys: deref(b.SshKeys), UserData: deref(b.UserData), IPv4: true, StartAfter: true}
	if b.Labels != nil {
		in.Labels = *b.Labels
	}
	if b.PublicNet != nil {
		if b.PublicNet.Ipv4 != nil {
			in.IPv4 = *b.PublicNet.Ipv4
		}
		in.IPv6 = deref(b.PublicNet.Ipv6)
	}
	for _, f := range deref(b.Firewalls) {
		in.Firewalls = append(in.Firewalls, f.Firewall)
	}
	if b.StartAfterCreate != nil {
		in.StartAfter = *b.StartAfterCreate
	}
	srv, a, err := s.servers.Create(ctx, sc.ProjectID, in)
	if err != nil {
		return nil, err
	}
	return apigen.PostServers201JSONResponse{Server: apiServer(srv, nil, nil), Action: apiAction(a), NextActions: []apigen.Action{}}, nil
}

func (s *server) GetServer(ctx context.Context, req apigen.GetServerRequestObject) (apigen.GetServerResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	srv, err := s.servers.Get(ctx, sc.ProjectID, req.Id)
	if err != nil {
		return nil, err
	}
	vols, fips, err := s.serverExtras(ctx, srv)
	if err != nil {
		return nil, err
	}
	return apigen.GetServer200JSONResponse(apiServer(srv, vols, fips)), nil
}

func (s *server) PutServer(ctx context.Context, req apigen.PutServerRequestObject) (apigen.PutServerResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	in := servers.UpdateInput{Name: req.Body.Name}
	if req.Body.Labels != nil {
		l := map[string]string(*req.Body.Labels)
		in.Labels = &l
	}
	srv, err := s.servers.Update(ctx, sc.ProjectID, req.Id, in)
	if err != nil {
		return nil, err
	}
	vols, fips, err := s.serverExtras(ctx, srv)
	if err != nil {
		return nil, err
	}
	return apigen.PutServer200JSONResponse(apiServer(srv, vols, fips)), nil
}

func (s *server) DeleteServer(ctx context.Context, req apigen.DeleteServerRequestObject) (apigen.DeleteServerResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	a, err := s.servers.Delete(ctx, sc.ProjectID, req.Id)
	if err != nil {
		return nil, err
	}
	return apigen.DeleteServer200JSONResponse{Action: apiAction(a)}, nil
}

func (s *server) GetServerActions(ctx context.Context, req apigen.GetServerActionsRequestObject) (apigen.GetServerActionsResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	f, err := actionFilter(req.Params.Status, req.Params.Sort)
	if err != nil {
		return nil, err
	}
	page, perPage := pageParams(req.Params.Page, req.Params.PerPage)
	as, total, err := s.servers.Actions(ctx, sc.ProjectID, req.Id, f, page, perPage)
	if err != nil {
		return nil, err
	}
	out := apigen.GetServerActions200JSONResponse{Actions: make([]apigen.Action, len(as)), Meta: meta(page, perPage, total)}
	for i, a := range as {
		out.Actions[i] = apiAction(a)
	}
	return out, nil
}

// power runs a power op for the scoped project.
func (s *server) power(ctx context.Context, id int64, op pve.PowerOp) (apigen.ActionCreatedJSONResponse, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return apigen.ActionCreatedJSONResponse{}, err
	}
	a, err := s.servers.Power(ctx, sc.ProjectID, id, op)
	if err != nil {
		return apigen.ActionCreatedJSONResponse{}, err
	}
	return apigen.ActionCreatedJSONResponse{Action: apiAction(a)}, nil
}

func (s *server) PoweronServer(ctx context.Context, req apigen.PoweronServerRequestObject) (apigen.PoweronServerResponseObject, error) {
	out, err := s.power(ctx, req.Id, pve.PowerStart)
	if err != nil {
		return nil, err
	}
	return apigen.PoweronServer201JSONResponse{ActionCreatedJSONResponse: out}, nil
}

func (s *server) PoweroffServer(ctx context.Context, req apigen.PoweroffServerRequestObject) (apigen.PoweroffServerResponseObject, error) {
	out, err := s.power(ctx, req.Id, pve.PowerStop)
	if err != nil {
		return nil, err
	}
	return apigen.PoweroffServer201JSONResponse{ActionCreatedJSONResponse: out}, nil
}

func (s *server) ShutdownServer(ctx context.Context, req apigen.ShutdownServerRequestObject) (apigen.ShutdownServerResponseObject, error) {
	out, err := s.power(ctx, req.Id, pve.PowerShutdown)
	if err != nil {
		return nil, err
	}
	return apigen.ShutdownServer201JSONResponse{ActionCreatedJSONResponse: out}, nil
}

func (s *server) RebootServer(ctx context.Context, req apigen.RebootServerRequestObject) (apigen.RebootServerResponseObject, error) {
	out, err := s.power(ctx, req.Id, pve.PowerReboot)
	if err != nil {
		return nil, err
	}
	return apigen.RebootServer201JSONResponse{ActionCreatedJSONResponse: out}, nil
}

func (s *server) ResetServer(ctx context.Context, req apigen.ResetServerRequestObject) (apigen.ResetServerResponseObject, error) {
	out, err := s.power(ctx, req.Id, pve.PowerReset)
	if err != nil {
		return nil, err
	}
	return apigen.ResetServer201JSONResponse{ActionCreatedJSONResponse: out}, nil
}

func (s *server) RebuildServer(ctx context.Context, req apigen.RebuildServerRequestObject) (apigen.RebuildServerResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	a, err := s.servers.Rebuild(ctx, sc.ProjectID, req.Id, req.Body.Image)
	if err != nil {
		return nil, err
	}
	return apigen.RebuildServer201JSONResponse{ActionCreatedJSONResponse: apigen.ActionCreatedJSONResponse{Action: apiAction(a)}}, nil
}

func (s *server) ChangeServerType(ctx context.Context, req apigen.ChangeServerTypeRequestObject) (apigen.ChangeServerTypeResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	a, err := s.servers.ChangeType(ctx, sc.ProjectID, req.Id, req.Body.ServerType, req.Body.UpgradeDisk)
	if err != nil {
		return nil, err
	}
	return apigen.ChangeServerType201JSONResponse{ActionCreatedJSONResponse: apigen.ActionCreatedJSONResponse{Action: apiAction(a)}}, nil
}

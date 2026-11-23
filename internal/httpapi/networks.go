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
	"github.com/nebla-cloud/nebla/internal/ent"
	entserver "github.com/nebla-cloud/nebla/internal/ent/server"
	"github.com/nebla-cloud/nebla/internal/ent/servernetwork"
	"github.com/nebla-cloud/nebla/internal/networks"
)

// privateNetItem is one entry of Server.private_net.
type privateNetItem = struct {
	GuestConfig *apigen.NetworkGuestConfig `json:"guest_config"`
	Ip          string                     `json:"ip"`
	Name        string                     `json:"name"`
	Network     int64                      `json:"network"`
}

func apiGuestConfig(g *networks.GuestCommands) *apigen.NetworkGuestConfig {
	if g == nil {
		return nil
	}
	return &apigen.NetworkGuestConfig{Apply: nullable(g.Apply), Persist: nullable(g.Persist), Remove: nullable(g.Remove), Note: nullable(g.Note)}
}

func apiNetwork(n networks.Network) apigen.Network {
	out := apigen.Network{Id: n.ID, Name: n.Name, Labels: n.Labels, IpRange: n.IPRange.String(), Created: n.Created,
		Location: apigen.LocationRef{Id: n.Location.ID, Name: n.Location.Name, Description: n.Location.Description},
		Servers: make([]struct {
			Ip     string `json:"ip"`
			Name   string `json:"name"`
			Server int64  `json:"server"`
		}, len(n.Servers))}
	if out.Labels == nil {
		out.Labels = apigen.Labels{}
	}
	for i, m := range n.Servers {
		out.Servers[i].Server, out.Servers[i].Name, out.Servers[i].Ip = m.ServerID, m.Name, m.IP.String()
	}
	return out
}

// privateNets maps each server to its private_net entries; running servers also get the commands to show again.
func (s *server) privateNets(ctx context.Context, ids []int64, running map[int64]bool) (map[int64][]privateNetItem, error) {
	m, err := s.networks.ForServers(ctx, s.deps.Ent, ids)
	if err != nil {
		return nil, err
	}
	out := make(map[int64][]privateNetItem, len(m))
	for id, nets := range m {
		var nics map[string]networks.NIC
		var srv *ent.Server
		flavor := ""
		if running[id] {
			list, err := networks.PrivateNICs(ctx, s.deps.Ent, id)
			if err != nil {
				return nil, err
			}
			nics = make(map[string]networks.NIC, len(list))
			for _, n := range list {
				nics[n.IP.String()] = n
			}
			if srv, err = s.deps.Ent.Server.Get(ctx, id); err != nil {
				return nil, err
			}
			if flavor, err = networks.FlavorOf(ctx, s.deps.Ent, srv); err != nil {
				return nil, err
			}
		}
		items := make([]privateNetItem, len(nets))
		for i, n := range nets {
			items[i] = privateNetItem{Network: n.NetworkID, Name: n.Network, Ip: n.IP.String()}
			if nic, ok := nics[n.IP.String()]; ok && nic.State != string(servernetwork.StateDetaching) {
				items[i].GuestConfig = apiGuestConfig(networks.GuestFor(srv, nic, n.NetworkID, flavor, true))
			}
		}
		out[id] = items
	}
	return out, nil
}

// attachGuest returns the commands for the server's NIC in the given transitional state, or nil.
func (s *server) attachGuest(ctx context.Context, serverID int64, state servernetwork.State, attach bool) (*apigen.NetworkGuestConfig, error) {
	row, err := s.deps.Ent.ServerNetwork.Query().Where(servernetwork.ServerID(serverID), servernetwork.StateEQ(state)).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	nics, err := networks.PrivateNICs(ctx, s.deps.Ent, serverID)
	if err != nil {
		return nil, err
	}
	srv, err := s.deps.Ent.Server.Get(ctx, serverID)
	if err != nil {
		return nil, err
	}
	flavor, err := networks.FlavorOf(ctx, s.deps.Ent, srv)
	if err != nil {
		return nil, err
	}
	for _, n := range nics {
		if n.Index == row.Nic {
			return apiGuestConfig(networks.GuestFor(srv, n, row.NetworkID, flavor, attach)), nil
		}
	}
	return nil, nil
}

func isRunning(status string) bool { return status == string(entserver.StatusRunning) }

func (s *server) GetNetworks(ctx context.Context, req apigen.GetNetworksRequestObject) (apigen.GetNetworksResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	p := req.Params
	page, perPage := pageParams(p.Page, p.PerPage)
	f := networks.ListFilter{Name: deref(p.Name), Location: deref(p.Location), LabelSelector: deref(p.LabelSelector)}
	ns, total, err := s.networks.List(ctx, sc.ProjectID, f, page, perPage)
	if err != nil {
		return nil, err
	}
	out := apigen.GetNetworks200JSONResponse{Networks: make([]apigen.Network, len(ns)), Meta: meta(page, perPage, total)}
	for i, n := range ns {
		out.Networks[i] = apiNetwork(n)
	}
	return out, nil
}

func (s *server) PostNetworks(ctx context.Context, req apigen.PostNetworksRequestObject) (apigen.PostNetworksResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	b := req.Body
	in := networks.CreateInput{Name: b.Name, Location: b.Location, IPRange: deref(b.IpRange)}
	if b.Labels != nil {
		in.Labels = *b.Labels
	}
	n, err := s.networks.Create(ctx, sc.ProjectID, in)
	if err != nil {
		return nil, err
	}
	return apigen.PostNetworks201JSONResponse{Network: apiNetwork(n)}, nil
}

func (s *server) GetNetwork(ctx context.Context, req apigen.GetNetworkRequestObject) (apigen.GetNetworkResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	n, err := s.networks.Get(ctx, sc.ProjectID, req.Id)
	if err != nil {
		return nil, err
	}
	return apigen.GetNetwork200JSONResponse(apiNetwork(n)), nil
}

func (s *server) PutNetwork(ctx context.Context, req apigen.PutNetworkRequestObject) (apigen.PutNetworkResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	in := networks.UpdateInput{Name: req.Body.Name}
	if req.Body.Labels != nil {
		l := map[string]string(*req.Body.Labels)
		in.Labels = &l
	}
	n, err := s.networks.Update(ctx, sc.ProjectID, req.Id, in)
	if err != nil {
		return nil, err
	}
	return apigen.PutNetwork200JSONResponse(apiNetwork(n)), nil
}

func (s *server) DeleteNetwork(ctx context.Context, req apigen.DeleteNetworkRequestObject) (apigen.DeleteNetworkResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.networks.Delete(ctx, sc.ProjectID, req.Id); err != nil {
		return nil, err
	}
	return apigen.DeleteNetwork204Response{}, nil
}

func (s *server) GetNetworkActions(ctx context.Context, req apigen.GetNetworkActionsRequestObject) (apigen.GetNetworkActionsResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	f, err := actionFilter(req.Params.Status, req.Params.Sort)
	if err != nil {
		return nil, err
	}
	page, perPage := pageParams(req.Params.Page, req.Params.PerPage)
	as, total, err := s.networks.Actions(ctx, sc.ProjectID, req.Id, f, page, perPage)
	if err != nil {
		return nil, err
	}
	out := apigen.GetNetworkActions200JSONResponse{Actions: make([]apigen.Action, len(as)), Meta: meta(page, perPage, total)}
	for i, a := range as {
		out.Actions[i] = apiAction(a)
	}
	return out, nil
}

func networkAction(a actions.Action, g *apigen.NetworkGuestConfig) apigen.NetworkActionCreatedJSONResponse {
	return apigen.NetworkActionCreatedJSONResponse{Action: apiAction(a), GuestConfig: g}
}

func (s *server) AttachServerToNetwork(ctx context.Context, req apigen.AttachServerToNetworkRequestObject) (apigen.AttachServerToNetworkResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	a, err := s.networks.Attach(ctx, sc.ProjectID, req.Id, req.Body.Network, deref(req.Body.Ip))
	if err != nil {
		return nil, err
	}
	g, err := s.attachGuest(ctx, req.Id, servernetwork.StateAttaching, true)
	if err != nil {
		return nil, err
	}
	return apigen.AttachServerToNetwork201JSONResponse{NetworkActionCreatedJSONResponse: networkAction(a, g)}, nil
}

func (s *server) DetachServerFromNetwork(ctx context.Context, req apigen.DetachServerFromNetworkRequestObject) (apigen.DetachServerFromNetworkResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	a, err := s.networks.Detach(ctx, sc.ProjectID, req.Id, req.Body.Network)
	if err != nil {
		return nil, err
	}
	g, err := s.attachGuest(ctx, req.Id, servernetwork.StateDetaching, false)
	if err != nil {
		return nil, err
	}
	return apigen.DetachServerFromNetwork201JSONResponse{NetworkActionCreatedJSONResponse: networkAction(a, g)}, nil
}

// apiPool maps a location's private network pool.
func apiPool(p networks.PoolStatus) apigen.PrivateNetworkPool {
	out := apigen.PrivateNetworkPool{State: apigen.PrivateNetworkPoolState(p.State), Zone: p.Zone, Available: p.Available, InUse: p.InUse,
		Cooling: p.Cooling, Unusable: make([]struct {
			Reason string `json:"reason"`
			Vnet   string `json:"vnet"`
		}, len(p.Unusable)), Degraded: make([]struct {
			Network string `json:"network"`
			Reason  string `json:"reason"`
			Vnet    string `json:"vnet"`
		}, len(p.Degraded))}
	for i, c := range p.Unusable {
		out.Unusable[i].Vnet, out.Unusable[i].Reason = c.VNet, c.Reason
	}
	for i, c := range p.Degraded {
		out.Degraded[i].Vnet, out.Degraded[i].Network, out.Degraded[i].Reason = c.VNet, c.Network, c.Reason
	}
	return out
}

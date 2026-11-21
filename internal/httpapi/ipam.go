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
	"github.com/nebla-cloud/nebla/internal/ipam"
)

func apiIPPool(p ipam.Pool) apigen.IpPool {
	return apigen.IpPool{Id: p.ID, LocationId: p.LocationID, Family: apigen.IpPoolFamily(p.Family), Bridge: p.Bridge, VlanTag: p.VLANTag,
		Cidr: p.CIDR, Gateway: p.Gateway, Dns: p.DNS, RangeStart: p.RangeStart, RangeEnd: p.RangeEnd, Allocated: p.Allocated, Created: p.CreatedAt}
}

func ipPoolInput(b *apigen.IpPoolInput) ipam.PoolInput {
	return ipam.PoolInput{LocationID: b.LocationId, Family: string(b.Family), Bridge: b.Bridge, VLANTag: b.VlanTag, CIDR: b.Cidr,
		Gateway: b.Gateway, DNS: b.Dns, RangeStart: b.RangeStart, RangeEnd: b.RangeEnd}
}

func (s *server) GetAdminIpPools(ctx context.Context, req apigen.GetAdminIpPoolsRequestObject) (apigen.GetAdminIpPoolsResponseObject, error) {
	page, perPage := pageParams(req.Params.Page, req.Params.PerPage)
	ps, total, err := s.ipam.List(ctx, deref(req.Params.LocationId), page, perPage)
	if err != nil {
		return nil, err
	}
	out := apigen.GetAdminIpPools200JSONResponse{IpPools: make([]apigen.IpPool, len(ps)), Meta: meta(page, perPage, total)}
	for i, p := range ps {
		out.IpPools[i] = apiIPPool(p)
	}
	return out, nil
}

func (s *server) PostAdminIpPools(ctx context.Context, req apigen.PostAdminIpPoolsRequestObject) (apigen.PostAdminIpPoolsResponseObject, error) {
	p, err := s.ipam.Create(ctx, ipPoolInput(req.Body))
	if err != nil {
		return nil, err
	}
	return apigen.PostAdminIpPools201JSONResponse(apiIPPool(p)), nil
}

func (s *server) GetAdminIpPool(ctx context.Context, req apigen.GetAdminIpPoolRequestObject) (apigen.GetAdminIpPoolResponseObject, error) {
	p, err := s.ipam.Get(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return apigen.GetAdminIpPool200JSONResponse(apiIPPool(p)), nil
}

func (s *server) PutAdminIpPool(ctx context.Context, req apigen.PutAdminIpPoolRequestObject) (apigen.PutAdminIpPoolResponseObject, error) {
	p, err := s.ipam.Update(ctx, req.Id, ipPoolInput(req.Body))
	if err != nil {
		return nil, err
	}
	return apigen.PutAdminIpPool200JSONResponse(apiIPPool(p)), nil
}

func (s *server) DeleteAdminIpPool(ctx context.Context, req apigen.DeleteAdminIpPoolRequestObject) (apigen.DeleteAdminIpPoolResponseObject, error) {
	if err := s.ipam.Delete(ctx, req.Id); err != nil {
		return nil, err
	}
	return apigen.DeleteAdminIpPool204Response{}, nil
}

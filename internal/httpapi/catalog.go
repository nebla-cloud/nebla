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
	"fmt"
	"time"

	"github.com/nebla-cloud/nebla/internal/apigen"
	"github.com/nebla-cloud/nebla/internal/catalog"
	"github.com/nebla-cloud/nebla/internal/ipam"
	"github.com/nebla-cloud/nebla/internal/networks"
)

// pageParams applies the spec defaults (page 1, per_page 25, max 50).
func pageParams(page, perPage *int) (int, int) {
	p, pp := 1, 25
	if page != nil && *page > 0 {
		p = min(*page, 1_000_000)
	}
	if perPage != nil && *perPage > 0 {
		pp = min(*perPage, 50)
	}
	return p, pp
}

func meta(page, perPage, total int) apigen.Meta {
	var m apigen.Meta
	m.Pagination.Page, m.Pagination.PerPage, m.Pagination.TotalEntries = page, perPage, total
	return m
}

func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}

func apiNode(n catalog.Node) apigen.Node {
	return apigen.Node{Id: n.ID, Name: n.Name, Status: n.Status, Enabled: n.Enabled, DiskStorageOverride: n.DiskStorageOverride,
		MaxCpu: n.MaxCPU, MaxMem: n.MaxMem, Cpu: n.CPU, Mem: n.Mem, Updated: n.UpdatedAt}
}

func apiAdminLocation(l catalog.Location, onOtherStorage int, pool networks.PoolStatus) apigen.AdminLocation {
	out := apigen.AdminLocation{Id: l.ID, Name: l.Name, Description: l.Description, DiskStorage: l.DiskStorage, IsoStorage: l.ISOStorage, ImportStorage: l.ImportStorage,
		FirewallEnabled: l.FirewallEnabled, SdnZone: pool.Zone, VnetPrefix: pool.Prefix, PrivateNetworks: apiPool(pool),
		VolumeStorage: l.VolumeStorage, VolumeStorageOk: l.VolumeStorageOK,
		BackupStorage: l.BackupStorage, BackupStorageOk: l.BackupStorageOK, BackupsOnOtherStorage: onOtherStorage,
		BackupWindow: apigen.BackupWindow{Start: hhmm(l.BackupWindowStart), End: hhmm(l.BackupWindowEnd)},
		Pve:          apigen.PveConnection{Url: l.PVEURL, TokenId: l.TokenID, HasSecret: l.HasSecret, CaFingerprint: l.CAFingerprint, InsecureTls: l.InsecureTLS},
		Nodes:        make([]apigen.Node, len(l.Nodes)), Created: l.CreatedAt, Updated: l.UpdatedAt}
	for i, n := range l.Nodes {
		out.Nodes[i] = apiNode(n)
	}
	return out
}

// adminLocation maps l with its count of backups kept on another storage and its private network pool.
func (s *server) adminLocation(ctx context.Context, l catalog.Location) (apigen.AdminLocation, error) {
	n, err := s.catalog.BackupsOnOtherStorage(ctx, l)
	if err != nil {
		return apigen.AdminLocation{}, err
	}
	pool, err := s.networks.Pool(ctx, l.ID)
	if err != nil {
		return apigen.AdminLocation{}, err
	}
	return apiAdminLocation(l, n, pool), nil
}

func apiLocation(l catalog.Location, ipv4, ipv6, volumes, backups, private bool) apigen.Location {
	out := apigen.Location{Id: l.ID, Name: l.Name, Description: l.Description, VolumesAvailable: volumes, BackupsAvailable: backups,
		PrivateNetworksAvailable: private}
	out.Networks.Ipv4, out.Networks.Ipv6 = ipv4, ipv6
	return out
}

// locationWithNetworks maps a location with its live address and volume availability.
func (s *server) locationWithNetworks(ctx context.Context, l catalog.Location) (apigen.Location, error) {
	v4, v6, err := ipam.Available(ctx, s.deps.Ent, l.ID)
	if err != nil {
		return apigen.Location{}, err
	}
	vols, err := s.volumes.Available(ctx, l.ID)
	if err != nil {
		// An unreachable or misconfigured location only hides volumes.
		s.log.WarnContext(ctx, "volumes availability", "location", l.ID, "err", err)
		vols = false
	}
	private, err := s.networks.Available(ctx, l.ID)
	if err != nil {
		// An unreachable pool only hides private networks.
		s.log.WarnContext(ctx, "private networks availability", "location", l.ID, "err", err)
		private = false
	}
	return apiLocation(l, v4, v6, vols, s.catalog.BackupsAvailable(l), private), nil
}

func locationInput(b *apigen.LocationInput) (catalog.LocationInput, error) {
	in := catalog.LocationInput{Name: b.Name, Description: deref(b.Description), DiskStorage: b.DiskStorage, ISOStorage: b.IsoStorage, ImportStorage: deref(b.ImportStorage),
		VolumeStorage: b.VolumeStorage, BackupStorage: b.BackupStorage,
		PVE: catalog.PVEInput{URL: b.Pve.Url, TokenID: b.Pve.TokenId, TokenSecret: b.Pve.TokenSecret,
			CAFingerprint: deref(b.Pve.CaFingerprint), InsecureTLS: deref(b.Pve.InsecureTls)}}
	if w := b.BackupWindow; w != nil {
		start, err := parseHHMM("backup_window.start", w.Start)
		if err != nil {
			return in, err
		}
		end, err := parseHHMM("backup_window.end", w.End)
		if err != nil {
			return in, err
		}
		in.BackupWindowStart, in.BackupWindowEnd = &start, &end
	}
	return in, nil
}

// hhmm formats minutes from 00:00 UTC as HH:MM.
func hhmm(min int) string { return fmt.Sprintf("%02d:%02d", min/60, min%60) }

// parseHHMM reads a strict 24-hour HH:MM into minutes from 00:00.
func parseHHMM(field, v string) (int, error) {
	t, err := time.Parse("15:04", v)
	if err != nil || len(v) != 5 {
		return 0, invalidInput(field + " must be a time as HH:MM")
	}
	return t.Hour()*60 + t.Minute(), nil
}

func apiServerType(t catalog.ServerType) apigen.ServerType {
	return apigen.ServerType{Id: t.ID, Name: t.Name, Description: t.Description, Cores: t.Cores, MemoryMb: t.MemoryMB,
		DiskGb: t.DiskGB, Locations: t.Locations, Deprecated: t.Deprecated}
}

func serverTypeInput(b *apigen.ServerTypeInput) catalog.ServerTypeInput {
	return catalog.ServerTypeInput{Name: b.Name, Description: deref(b.Description), Cores: b.Cores, MemoryMB: b.MemoryMb,
		DiskGB: b.DiskGb, Locations: deref(b.Locations), Deprecated: deref(b.Deprecated)}
}

func apiAdminImage(i catalog.Image) apigen.AdminImage {
	out := apigen.AdminImage{Id: i.ID, Name: i.Name, Description: i.Description, OsFlavor: i.OSFlavor, OsVersion: i.OSVersion,
		Templates: make([]apigen.ImageTemplate, len(i.Templates)), Created: i.CreatedAt}
	if i.CatalogKey != "" {
		out.CatalogKey = new(i.CatalogKey)
	}
	for j, t := range i.Templates {
		out.Templates[j] = apigen.ImageTemplate{LocationId: t.LocationID, Vmid: t.VMID}
		if t.Node != "" {
			out.Templates[j].Node = new(t.Node)
		}
	}
	return out
}

func apiImage(i catalog.Image) apigen.Image {
	return apigen.Image{Id: i.ID, Name: i.Name, Description: i.Description, OsFlavor: i.OSFlavor, OsVersion: i.OSVersion, Locations: i.Locations()}
}

func imageInput(b *apigen.ImageInput) catalog.ImageInput {
	in := catalog.ImageInput{Name: b.Name, Description: deref(b.Description), OSFlavor: b.OsFlavor, OSVersion: b.OsVersion,
		Templates: make([]catalog.TemplateInput, len(b.Templates))}
	for i, t := range b.Templates {
		in.Templates[i] = catalog.TemplateInput{LocationID: t.LocationId, Node: deref(t.Node), VMID: t.Vmid}
	}
	return in
}

func (s *server) GetAdminLocations(ctx context.Context, req apigen.GetAdminLocationsRequestObject) (apigen.GetAdminLocationsResponseObject, error) {
	page, perPage := pageParams(req.Params.Page, req.Params.PerPage)
	ls, total, err := s.catalog.ListLocations(ctx, page, perPage)
	if err != nil {
		return nil, err
	}
	out := apigen.GetAdminLocations200JSONResponse{Locations: make([]apigen.AdminLocation, len(ls)), Meta: meta(page, perPage, total)}
	for i, l := range ls {
		if out.Locations[i], err = s.adminLocation(ctx, l); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *server) PostAdminLocations(ctx context.Context, req apigen.PostAdminLocationsRequestObject) (apigen.PostAdminLocationsResponseObject, error) {
	in, err := locationInput(req.Body)
	if err != nil {
		return nil, err
	}
	l, err := s.catalog.CreateLocation(ctx, in)
	if err != nil {
		return nil, err
	}
	out, err := s.adminLocation(ctx, l)
	if err != nil {
		return nil, err
	}
	return apigen.PostAdminLocations201JSONResponse(out), nil
}

func (s *server) GetAdminLocation(ctx context.Context, req apigen.GetAdminLocationRequestObject) (apigen.GetAdminLocationResponseObject, error) {
	l, err := s.catalog.GetLocation(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	out, err := s.adminLocation(ctx, l)
	if err != nil {
		return nil, err
	}
	return apigen.GetAdminLocation200JSONResponse(out), nil
}

func (s *server) PutAdminLocation(ctx context.Context, req apigen.PutAdminLocationRequestObject) (apigen.PutAdminLocationResponseObject, error) {
	in, err := locationInput(req.Body)
	if err != nil {
		return nil, err
	}
	l, err := s.catalog.UpdateLocation(ctx, req.Id, in)
	if err != nil {
		return nil, err
	}
	if b := req.Body; b.SdnZone != nil || b.VnetPrefix != nil {
		cur, err := s.networks.Pool(ctx, req.Id)
		if err != nil {
			return nil, err
		}
		zone, prefix := cur.Zone, cur.Prefix
		if b.SdnZone != nil {
			zone = b.SdnZone
		}
		if b.VnetPrefix != nil {
			prefix = *b.VnetPrefix
		}
		if _, err := s.networks.SetLocationSDN(ctx, req.Id, zone, prefix); err != nil {
			return nil, err
		}
	}
	out, err := s.adminLocation(ctx, l)
	if err != nil {
		return nil, err
	}
	return apigen.PutAdminLocation200JSONResponse(out), nil
}

func (s *server) DeleteAdminLocation(ctx context.Context, req apigen.DeleteAdminLocationRequestObject) (apigen.DeleteAdminLocationResponseObject, error) {
	if err := s.catalog.DeleteLocation(ctx, req.Id); err != nil {
		return nil, err
	}
	return apigen.DeleteAdminLocation204Response{}, nil
}

func (s *server) PostAdminLocationSyncNodes(ctx context.Context, req apigen.PostAdminLocationSyncNodesRequestObject) (apigen.PostAdminLocationSyncNodesResponseObject, error) {
	l, err := s.catalog.SyncNodes(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	out, err := s.adminLocation(ctx, l)
	if err != nil {
		return nil, err
	}
	return apigen.PostAdminLocationSyncNodes200JSONResponse(out), nil
}

func (s *server) PutAdminLocationNode(ctx context.Context, req apigen.PutAdminLocationNodeRequestObject) (apigen.PutAdminLocationNodeResponseObject, error) {
	n, err := s.catalog.UpdateNode(ctx, req.Id, req.NodeId, catalog.NodeUpdate{Enabled: req.Body.Enabled, DiskStorageOverride: req.Body.DiskStorageOverride})
	if err != nil {
		return nil, err
	}
	return apigen.PutAdminLocationNode200JSONResponse(apiNode(n)), nil
}

func (s *server) serverTypeList(ctx context.Context, page, perPage *int) (apigen.ServerTypeList, error) {
	p, pp := pageParams(page, perPage)
	ts, total, err := s.catalog.ListServerTypes(ctx, p, pp)
	if err != nil {
		return apigen.ServerTypeList{}, err
	}
	out := apigen.ServerTypeList{ServerTypes: make([]apigen.ServerType, len(ts)), Meta: meta(p, pp, total)}
	for i, t := range ts {
		out.ServerTypes[i] = apiServerType(t)
	}
	return out, nil
}

func (s *server) GetAdminServerTypes(ctx context.Context, req apigen.GetAdminServerTypesRequestObject) (apigen.GetAdminServerTypesResponseObject, error) {
	out, err := s.serverTypeList(ctx, req.Params.Page, req.Params.PerPage)
	if err != nil {
		return nil, err
	}
	return apigen.GetAdminServerTypes200JSONResponse(out), nil
}

func (s *server) PostAdminServerTypes(ctx context.Context, req apigen.PostAdminServerTypesRequestObject) (apigen.PostAdminServerTypesResponseObject, error) {
	t, err := s.catalog.CreateServerType(ctx, serverTypeInput(req.Body))
	if err != nil {
		return nil, err
	}
	return apigen.PostAdminServerTypes201JSONResponse(apiServerType(t)), nil
}

func (s *server) GetAdminServerType(ctx context.Context, req apigen.GetAdminServerTypeRequestObject) (apigen.GetAdminServerTypeResponseObject, error) {
	t, err := s.catalog.GetServerType(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return apigen.GetAdminServerType200JSONResponse(apiServerType(t)), nil
}

func (s *server) PutAdminServerType(ctx context.Context, req apigen.PutAdminServerTypeRequestObject) (apigen.PutAdminServerTypeResponseObject, error) {
	t, err := s.catalog.UpdateServerType(ctx, req.Id, serverTypeInput(req.Body))
	if err != nil {
		return nil, err
	}
	return apigen.PutAdminServerType200JSONResponse(apiServerType(t)), nil
}

func (s *server) DeleteAdminServerType(ctx context.Context, req apigen.DeleteAdminServerTypeRequestObject) (apigen.DeleteAdminServerTypeResponseObject, error) {
	if err := s.catalog.DeleteServerType(ctx, req.Id); err != nil {
		return nil, err
	}
	return apigen.DeleteAdminServerType204Response{}, nil
}

func (s *server) GetAdminImages(ctx context.Context, req apigen.GetAdminImagesRequestObject) (apigen.GetAdminImagesResponseObject, error) {
	page, perPage := pageParams(req.Params.Page, req.Params.PerPage)
	is, total, err := s.catalog.ListImages(ctx, 0, page, perPage)
	if err != nil {
		return nil, err
	}
	out := apigen.GetAdminImages200JSONResponse{Images: make([]apigen.AdminImage, len(is)), Meta: meta(page, perPage, total)}
	for i, img := range is {
		out.Images[i] = apiAdminImage(img)
	}
	return out, nil
}

func (s *server) PostAdminImages(ctx context.Context, req apigen.PostAdminImagesRequestObject) (apigen.PostAdminImagesResponseObject, error) {
	img, err := s.catalog.CreateImage(ctx, imageInput(req.Body))
	if err != nil {
		return nil, err
	}
	return apigen.PostAdminImages201JSONResponse(apiAdminImage(img)), nil
}

func (s *server) GetAdminImage(ctx context.Context, req apigen.GetAdminImageRequestObject) (apigen.GetAdminImageResponseObject, error) {
	img, err := s.catalog.GetImage(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return apigen.GetAdminImage200JSONResponse(apiAdminImage(img)), nil
}

func (s *server) PutAdminImage(ctx context.Context, req apigen.PutAdminImageRequestObject) (apigen.PutAdminImageResponseObject, error) {
	img, err := s.catalog.UpdateImage(ctx, req.Id, imageInput(req.Body))
	if err != nil {
		return nil, err
	}
	return apigen.PutAdminImage200JSONResponse(apiAdminImage(img)), nil
}

func (s *server) DeleteAdminImage(ctx context.Context, req apigen.DeleteAdminImageRequestObject) (apigen.DeleteAdminImageResponseObject, error) {
	if err := s.catalog.DeleteImage(ctx, req.Id); err != nil {
		return nil, err
	}
	return apigen.DeleteAdminImage204Response{}, nil
}

func (s *server) GetLocations(ctx context.Context, req apigen.GetLocationsRequestObject) (apigen.GetLocationsResponseObject, error) {
	if _, err := mustScope(ctx); err != nil {
		return nil, err
	}
	page, perPage := pageParams(req.Params.Page, req.Params.PerPage)
	ls, total, err := s.catalog.ListLocations(ctx, page, perPage)
	if err != nil {
		return nil, err
	}
	out := apigen.GetLocations200JSONResponse{Locations: make([]apigen.Location, len(ls)), Meta: meta(page, perPage, total)}
	for i, l := range ls {
		if out.Locations[i], err = s.locationWithNetworks(ctx, l); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *server) GetLocation(ctx context.Context, req apigen.GetLocationRequestObject) (apigen.GetLocationResponseObject, error) {
	if _, err := mustScope(ctx); err != nil {
		return nil, err
	}
	l, err := s.catalog.GetLocation(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	out, err := s.locationWithNetworks(ctx, l)
	if err != nil {
		return nil, err
	}
	return apigen.GetLocation200JSONResponse(out), nil
}

func (s *server) GetServerTypes(ctx context.Context, req apigen.GetServerTypesRequestObject) (apigen.GetServerTypesResponseObject, error) {
	if _, err := mustScope(ctx); err != nil {
		return nil, err
	}
	out, err := s.serverTypeList(ctx, req.Params.Page, req.Params.PerPage)
	if err != nil {
		return nil, err
	}
	return apigen.GetServerTypes200JSONResponse(out), nil
}

func (s *server) GetServerType(ctx context.Context, req apigen.GetServerTypeRequestObject) (apigen.GetServerTypeResponseObject, error) {
	if _, err := mustScope(ctx); err != nil {
		return nil, err
	}
	t, err := s.catalog.GetServerType(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return apigen.GetServerType200JSONResponse(apiServerType(t)), nil
}

func (s *server) GetImages(ctx context.Context, req apigen.GetImagesRequestObject) (apigen.GetImagesResponseObject, error) {
	if _, err := mustScope(ctx); err != nil {
		return nil, err
	}
	var locID int64
	if ref := deref(req.Params.Location); ref != "" {
		l, err := s.catalog.LocationByRef(ctx, ref)
		if err != nil {
			return nil, err
		}
		locID = l.ID
	}
	page, perPage := pageParams(req.Params.Page, req.Params.PerPage)
	is, total, err := s.catalog.ListImages(ctx, locID, page, perPage)
	if err != nil {
		return nil, err
	}
	out := apigen.GetImages200JSONResponse{Images: make([]apigen.Image, len(is)), Meta: meta(page, perPage, total)}
	for i, img := range is {
		out.Images[i] = apiImage(img)
	}
	return out, nil
}

func (s *server) GetImage(ctx context.Context, req apigen.GetImageRequestObject) (apigen.GetImageResponseObject, error) {
	if _, err := mustScope(ctx); err != nil {
		return nil, err
	}
	img, err := s.catalog.GetImage(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return apigen.GetImage200JSONResponse(apiImage(img)), nil
}

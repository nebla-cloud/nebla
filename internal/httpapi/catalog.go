package httpapi

import (
	"context"

	"github.com/Hexeption/nebla/internal/apigen"
	"github.com/Hexeption/nebla/internal/catalog"
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

func apiAdminLocation(l catalog.Location) apigen.AdminLocation {
	out := apigen.AdminLocation{Id: l.ID, Name: l.Name, Description: l.Description, DiskStorage: l.DiskStorage, IsoStorage: l.ISOStorage, ImportStorage: l.ImportStorage,
		Pve:   apigen.PveConnection{Url: l.PVEURL, TokenId: l.TokenID, HasSecret: l.HasSecret, CaFingerprint: l.CAFingerprint, InsecureTls: l.InsecureTLS},
		Nodes: make([]apigen.Node, len(l.Nodes)), Created: l.CreatedAt, Updated: l.UpdatedAt}
	for i, n := range l.Nodes {
		out.Nodes[i] = apiNode(n)
	}
	return out
}

func apiLocation(l catalog.Location) apigen.Location {
	return apigen.Location{Id: l.ID, Name: l.Name, Description: l.Description}
}

func locationInput(b *apigen.LocationInput) catalog.LocationInput {
	return catalog.LocationInput{Name: b.Name, Description: deref(b.Description), DiskStorage: b.DiskStorage, ISOStorage: b.IsoStorage, ImportStorage: deref(b.ImportStorage),
		PVE: catalog.PVEInput{URL: b.Pve.Url, TokenID: b.Pve.TokenId, TokenSecret: b.Pve.TokenSecret,
			CAFingerprint: deref(b.Pve.CaFingerprint), InsecureTLS: deref(b.Pve.InsecureTls)}}
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
		out.Locations[i] = apiAdminLocation(l)
	}
	return out, nil
}

func (s *server) PostAdminLocations(ctx context.Context, req apigen.PostAdminLocationsRequestObject) (apigen.PostAdminLocationsResponseObject, error) {
	l, err := s.catalog.CreateLocation(ctx, locationInput(req.Body))
	if err != nil {
		return nil, err
	}
	return apigen.PostAdminLocations201JSONResponse(apiAdminLocation(l)), nil
}

func (s *server) GetAdminLocation(ctx context.Context, req apigen.GetAdminLocationRequestObject) (apigen.GetAdminLocationResponseObject, error) {
	l, err := s.catalog.GetLocation(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return apigen.GetAdminLocation200JSONResponse(apiAdminLocation(l)), nil
}

func (s *server) PutAdminLocation(ctx context.Context, req apigen.PutAdminLocationRequestObject) (apigen.PutAdminLocationResponseObject, error) {
	l, err := s.catalog.UpdateLocation(ctx, req.Id, locationInput(req.Body))
	if err != nil {
		return nil, err
	}
	return apigen.PutAdminLocation200JSONResponse(apiAdminLocation(l)), nil
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
	return apigen.PostAdminLocationSyncNodes200JSONResponse(apiAdminLocation(l)), nil
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
		out.Locations[i] = apiLocation(l)
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
	return apigen.GetLocation200JSONResponse(apiLocation(l)), nil
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

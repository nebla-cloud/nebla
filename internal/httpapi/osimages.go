package httpapi

import (
	"context"

	"github.com/nebla-cloud/nebla/internal/apigen"
	"github.com/nebla-cloud/nebla/internal/osimages"
)

func apiCatalogLocation(l osimages.LocationState) apigen.CatalogLocationState {
	out := apigen.CatalogLocationState{LocationId: l.LocationID, Available: l.Available, Reason: l.Reason,
		State: l.State, Nodes: make([]apigen.CatalogNode, len(l.Nodes))}
	for i, n := range l.Nodes {
		out.Nodes[i] = apigen.CatalogNode{Node: n.Node, Status: n.Status, Vmid: n.VMID, File: n.File,
			BuiltAt: n.BuiltAt, Error: n.Error}
	}
	return out
}

func (s *server) GetAdminCatalog(ctx context.Context, _ apigen.GetAdminCatalogRequestObject) (apigen.GetAdminCatalogResponseObject, error) {
	es, err := s.osimages.List(ctx)
	if err != nil {
		return nil, err
	}
	out := apigen.GetAdminCatalog200JSONResponse{Catalog: make([]apigen.CatalogEntry, len(es))}
	for i, e := range es {
		ce := apigen.CatalogEntry{Key: e.Key, Description: e.Description, OsFlavor: e.OSFlavor, OsVersion: e.OSVersion,
			Retired: e.Retired, Locations: make([]apigen.CatalogLocationState, len(e.Locations))}
		for j, l := range e.Locations {
			ce.Locations[j] = apiCatalogLocation(l)
		}
		out.Catalog[i] = ce
	}
	return out, nil
}

func (s *server) PostAdminCatalogEnable(ctx context.Context, req apigen.PostAdminCatalogEnableRequestObject) (apigen.PostAdminCatalogEnableResponseObject, error) {
	l, err := s.osimages.Enable(ctx, req.Key, req.LocationId)
	if err != nil {
		return nil, err
	}
	return apigen.PostAdminCatalogEnable200JSONResponse{Location: apiCatalogLocation(l)}, nil
}

func (s *server) PostAdminCatalogRefresh(ctx context.Context, req apigen.PostAdminCatalogRefreshRequestObject) (apigen.PostAdminCatalogRefreshResponseObject, error) {
	l, err := s.osimages.Refresh(ctx, req.Key, req.LocationId)
	if err != nil {
		return nil, err
	}
	return apigen.PostAdminCatalogRefresh200JSONResponse{Location: apiCatalogLocation(l)}, nil
}

func (s *server) PostAdminCatalogDisable(ctx context.Context, req apigen.PostAdminCatalogDisableRequestObject) (apigen.PostAdminCatalogDisableResponseObject, error) {
	l, err := s.osimages.Disable(ctx, req.Key, req.LocationId)
	if err != nil {
		return nil, err
	}
	return apigen.PostAdminCatalogDisable200JSONResponse{Location: apiCatalogLocation(l)}, nil
}

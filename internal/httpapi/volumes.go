package httpapi

import (
	"context"

	"github.com/nebla-cloud/nebla/internal/apigen"
	"github.com/nebla-cloud/nebla/internal/volumes"
)

func apiVolume(v volumes.Volume) apigen.Volume {
	return apigen.Volume{Id: v.ID, Name: v.Name, Labels: v.Labels, Size: v.SizeGB, Server: v.ServerID,
		Location: apigen.LocationRef{Id: v.LocationID, Name: v.LocationName, Description: v.LocationDescription},
		Status:   apigen.VolumeStatus(v.Status), LinuxDevice: v.LinuxDevice, Created: v.Created}
}

func (s *server) GetVolumes(ctx context.Context, req apigen.GetVolumesRequestObject) (apigen.GetVolumesResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	p := req.Params
	page, perPage := pageParams(p.Page, p.PerPage)
	f := volumes.ListFilter{Name: deref(p.Name), LabelSelector: deref(p.LabelSelector), Status: deref(p.Status),
		Server: deref(p.Server), Location: deref(p.Location)}
	vs, total, err := s.volumes.List(ctx, sc.ProjectID, f, page, perPage)
	if err != nil {
		return nil, err
	}
	out := apigen.GetVolumes200JSONResponse{Volumes: make([]apigen.Volume, len(vs)), Meta: meta(page, perPage, total)}
	for i, v := range vs {
		out.Volumes[i] = apiVolume(v)
	}
	return out, nil
}

func (s *server) PostVolumes(ctx context.Context, req apigen.PostVolumesRequestObject) (apigen.PostVolumesResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	b := req.Body
	in := volumes.CreateInput{Name: b.Name, SizeGB: b.Size, Location: deref(b.Location), Server: deref(b.Server)}
	if b.Labels != nil {
		in.Labels = *b.Labels
	}
	v, a, err := s.volumes.Create(ctx, sc.ProjectID, in)
	if err != nil {
		return nil, err
	}
	return apigen.PostVolumes201JSONResponse{Volume: apiVolume(v), Action: apiAction(a)}, nil
}

func (s *server) GetVolume(ctx context.Context, req apigen.GetVolumeRequestObject) (apigen.GetVolumeResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	v, err := s.volumes.Get(ctx, sc.ProjectID, req.Id)
	if err != nil {
		return nil, err
	}
	return apigen.GetVolume200JSONResponse(apiVolume(v)), nil
}

func (s *server) PutVolume(ctx context.Context, req apigen.PutVolumeRequestObject) (apigen.PutVolumeResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	in := volumes.UpdateInput{Name: req.Body.Name}
	if req.Body.Labels != nil {
		l := map[string]string(*req.Body.Labels)
		in.Labels = &l
	}
	v, err := s.volumes.Update(ctx, sc.ProjectID, req.Id, in)
	if err != nil {
		return nil, err
	}
	return apigen.PutVolume200JSONResponse(apiVolume(v)), nil
}

func (s *server) DeleteVolume(ctx context.Context, req apigen.DeleteVolumeRequestObject) (apigen.DeleteVolumeResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	a, err := s.volumes.Delete(ctx, sc.ProjectID, req.Id)
	if err != nil {
		return nil, err
	}
	return apigen.DeleteVolume200JSONResponse{Action: apiAction(a)}, nil
}

func (s *server) GetVolumeActions(ctx context.Context, req apigen.GetVolumeActionsRequestObject) (apigen.GetVolumeActionsResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	f, err := actionFilter(req.Params.Status, req.Params.Sort)
	if err != nil {
		return nil, err
	}
	page, perPage := pageParams(req.Params.Page, req.Params.PerPage)
	as, total, err := s.volumes.Actions(ctx, sc.ProjectID, req.Id, f, page, perPage)
	if err != nil {
		return nil, err
	}
	out := apigen.GetVolumeActions200JSONResponse{Actions: make([]apigen.Action, len(as)), Meta: meta(page, perPage, total)}
	for i, a := range as {
		out.Actions[i] = apiAction(a)
	}
	return out, nil
}

func (s *server) AttachVolume(ctx context.Context, req apigen.AttachVolumeRequestObject) (apigen.AttachVolumeResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	a, err := s.volumes.Attach(ctx, sc.ProjectID, req.Id, req.Body.Server)
	if err != nil {
		return nil, err
	}
	return apigen.AttachVolume201JSONResponse{ActionCreatedJSONResponse: apigen.ActionCreatedJSONResponse{Action: apiAction(a)}}, nil
}

func (s *server) DetachVolume(ctx context.Context, req apigen.DetachVolumeRequestObject) (apigen.DetachVolumeResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	a, err := s.volumes.Detach(ctx, sc.ProjectID, req.Id)
	if err != nil {
		return nil, err
	}
	return apigen.DetachVolume201JSONResponse{ActionCreatedJSONResponse: apigen.ActionCreatedJSONResponse{Action: apiAction(a)}}, nil
}

func (s *server) ResizeVolume(ctx context.Context, req apigen.ResizeVolumeRequestObject) (apigen.ResizeVolumeResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	a, err := s.volumes.Resize(ctx, sc.ProjectID, req.Id, req.Body.Size)
	if err != nil {
		return nil, err
	}
	return apigen.ResizeVolume201JSONResponse{ActionCreatedJSONResponse: apigen.ActionCreatedJSONResponse{Action: apiAction(a)}}, nil
}

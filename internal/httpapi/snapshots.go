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
	"github.com/nebla-cloud/nebla/internal/snapshots"
)

// snapshotMapper maps snapshots, looking each location up once.
type snapshotMapper struct {
	s    *server
	locs map[int64]apigen.LocationRef
}

func (s *server) snapshotMapper() *snapshotMapper {
	return &snapshotMapper{s: s, locs: map[int64]apigen.LocationRef{}}
}

func (m *snapshotMapper) apiSnapshot(ctx context.Context, v snapshots.Snapshot) (apigen.Snapshot, error) {
	ref, ok := m.locs[v.LocationID]
	if !ok {
		loc, err := m.s.catalog.GetLocation(ctx, v.LocationID)
		if err != nil {
			return apigen.Snapshot{}, err
		}
		ref = apigen.LocationRef{Id: loc.ID, Name: loc.Name, Description: loc.Description}
		m.locs[v.LocationID] = ref
	}
	out := apigen.Snapshot{Id: v.ID, Name: v.Name, Description: v.Description, Labels: v.Labels, Size: v.SizeGB,
		Location: ref,
		Server:   v.ServerID, ServerName: v.ServerName, OsFlavor: v.OSFlavor, OsVersion: v.OSVersion,
		Status: apigen.SnapshotStatus(v.Status), RestoreKeepsIdentity: v.RestoreKeepsIdentity, Created: v.CreatedAt}
	if out.Labels == nil {
		out.Labels = map[string]string{}
	}
	if v.ImageID != nil {
		out.Image = &struct {
			Id   int64  `json:"id"`
			Name string `json:"name"`
		}{Id: *v.ImageID, Name: v.ImageName}
	}
	return out, nil
}

func (s *server) GetSnapshots(ctx context.Context, req apigen.GetSnapshotsRequestObject) (apigen.GetSnapshotsResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	p := req.Params
	page, perPage := pageParams(p.Page, p.PerPage)
	f := snapshots.ListFilter{Name: deref(p.Name), LabelSelector: deref(p.LabelSelector), Status: deref(p.Status),
		Server: deref(p.Server), Location: deref(p.Location)}
	vs, total, err := s.snapshots.List(ctx, sc.ProjectID, f, page, perPage)
	if err != nil {
		return nil, err
	}
	out := apigen.GetSnapshots200JSONResponse{Snapshots: make([]apigen.Snapshot, len(vs)), Meta: meta(page, perPage, total)}
	m := s.snapshotMapper()
	for i, v := range vs {
		if out.Snapshots[i], err = m.apiSnapshot(ctx, v); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *server) PostSnapshots(ctx context.Context, req apigen.PostSnapshotsRequestObject) (apigen.PostSnapshotsResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	b := req.Body
	in := snapshots.CreateInput{Server: b.Server, Name: deref(b.Name), Description: deref(b.Description), ForceStop: deref(b.ForceStop)}
	if b.Labels != nil {
		in.Labels = *b.Labels
	}
	v, a, err := s.snapshots.Create(ctx, sc.ProjectID, in)
	if err != nil {
		return nil, err
	}
	snap, err := s.snapshotMapper().apiSnapshot(ctx, v)
	if err != nil {
		return nil, err
	}
	return apigen.PostSnapshots201JSONResponse{Snapshot: snap, Action: apiAction(a)}, nil
}

func (s *server) GetSnapshot(ctx context.Context, req apigen.GetSnapshotRequestObject) (apigen.GetSnapshotResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	v, err := s.snapshots.Get(ctx, sc.ProjectID, req.Id)
	if err != nil {
		return nil, err
	}
	out, err := s.snapshotMapper().apiSnapshot(ctx, v)
	return apigen.GetSnapshot200JSONResponse(out), err
}

func (s *server) PutSnapshot(ctx context.Context, req apigen.PutSnapshotRequestObject) (apigen.PutSnapshotResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	in := snapshots.UpdateInput{Name: req.Body.Name, Description: req.Body.Description}
	if req.Body.Labels != nil {
		l := map[string]string(*req.Body.Labels)
		in.Labels = &l
	}
	v, err := s.snapshots.Update(ctx, sc.ProjectID, req.Id, in)
	if err != nil {
		return nil, err
	}
	out, err := s.snapshotMapper().apiSnapshot(ctx, v)
	return apigen.PutSnapshot200JSONResponse(out), err
}

func (s *server) DeleteSnapshot(ctx context.Context, req apigen.DeleteSnapshotRequestObject) (apigen.DeleteSnapshotResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	a, err := s.snapshots.Delete(ctx, sc.ProjectID, req.Id)
	if err != nil {
		return nil, err
	}
	return apigen.DeleteSnapshot200JSONResponse{Action: apiAction(a)}, nil
}

func (s *server) GetSnapshotActions(ctx context.Context, req apigen.GetSnapshotActionsRequestObject) (apigen.GetSnapshotActionsResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	f, err := actionFilter(req.Params.Status, req.Params.Sort)
	if err != nil {
		return nil, err
	}
	page, perPage := pageParams(req.Params.Page, req.Params.PerPage)
	as, total, err := s.snapshots.Actions(ctx, sc.ProjectID, req.Id, f, page, perPage)
	if err != nil {
		return nil, err
	}
	out := apigen.GetSnapshotActions200JSONResponse{Actions: make([]apigen.Action, len(as)), Meta: meta(page, perPage, total)}
	for i, a := range as {
		out.Actions[i] = apiAction(a)
	}
	return out, nil
}

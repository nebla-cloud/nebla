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
	"github.com/nebla-cloud/nebla/internal/backups"
	"github.com/nebla-cloud/nebla/internal/servers"
)

func apiBackup(b backups.Backup) apigen.Backup {
	return apigen.Backup{Id: b.ID, Server: b.ServerID, Kind: apigen.BackupKind(b.Kind), Status: apigen.BackupStatus(b.Status),
		Size: b.SizeBytes, Created: b.Created, Completed: b.Completed, RestoreResetsIdentity: b.RestoreResetsIdentity}
}

// backupSettings maps the request's backups object; nil leaves the setting alone.
func backupSettings(b *apigen.BackupSettings) *servers.BackupSettings {
	if b == nil {
		return nil
	}
	return &servers.BackupSettings{Enabled: b.Enabled, Keep: b.Keep}
}

func (s *server) GetServerBackups(ctx context.Context, req apigen.GetServerBackupsRequestObject) (apigen.GetServerBackupsResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	bs, err := s.backups.List(ctx, sc.ProjectID, req.Id, backups.ListFilter{Status: string(deref(req.Params.Status)), Kind: string(deref(req.Params.Kind))})
	if err != nil {
		return nil, err
	}
	out := apigen.GetServerBackups200JSONResponse{Backups: make([]apigen.Backup, len(bs))}
	for i, b := range bs {
		out.Backups[i] = apiBackup(b)
	}
	return out, nil
}

func (s *server) PostServerBackups(ctx context.Context, req apigen.PostServerBackupsRequestObject) (apigen.PostServerBackupsResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	b, created, err := s.backups.BackUpNow(ctx, sc.ProjectID, req.Id)
	if err != nil {
		return nil, err
	}
	if !created {
		return apigen.PostServerBackups200JSONResponse(apiBackup(b)), nil
	}
	return apigen.PostServerBackups202JSONResponse(apiBackup(b)), nil
}

func (s *server) GetServerBackup(ctx context.Context, req apigen.GetServerBackupRequestObject) (apigen.GetServerBackupResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	b, err := s.backups.Get(ctx, sc.ProjectID, req.Id, req.Backup)
	if err != nil {
		return nil, err
	}
	return apigen.GetServerBackup200JSONResponse(apiBackup(b)), nil
}

func (s *server) DeleteServerBackup(ctx context.Context, req apigen.DeleteServerBackupRequestObject) (apigen.DeleteServerBackupResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	a, err := s.backups.Delete(ctx, sc.ProjectID, req.Id, req.Backup)
	if err != nil {
		return nil, err
	}
	return apigen.DeleteServerBackup200JSONResponse{Action: apiAction(a)}, nil
}

func (s *server) RestoreServerBackup(ctx context.Context, req apigen.RestoreServerBackupRequestObject) (apigen.RestoreServerBackupResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	a, err := s.backups.Restore(ctx, sc.ProjectID, req.Id, req.Body.Backup)
	if err != nil {
		return nil, err
	}
	return apigen.RestoreServerBackup201JSONResponse{ActionCreatedJSONResponse: apigen.ActionCreatedJSONResponse{Action: apiAction(a)}}, nil
}

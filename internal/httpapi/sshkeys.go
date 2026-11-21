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
	"github.com/nebla-cloud/nebla/internal/sshkeys"
)

func apiSSHKey(k sshkeys.Key) apigen.SshKey {
	return apigen.SshKey{Id: k.ID, Name: k.Name, PublicKey: k.PublicKey, Fingerprint: k.Fingerprint, Created: k.CreatedAt}
}

func (s *server) GetSshKeys(ctx context.Context, req apigen.GetSshKeysRequestObject) (apigen.GetSshKeysResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	page, perPage := pageParams(req.Params.Page, req.Params.PerPage)
	ks, total, err := s.sshkeys.List(ctx, sc.ProjectID, page, perPage)
	if err != nil {
		return nil, err
	}
	out := apigen.GetSshKeys200JSONResponse{SshKeys: make([]apigen.SshKey, len(ks)), Meta: meta(page, perPage, total)}
	for i, k := range ks {
		out.SshKeys[i] = apiSSHKey(k)
	}
	return out, nil
}

func (s *server) PostSshKeys(ctx context.Context, req apigen.PostSshKeysRequestObject) (apigen.PostSshKeysResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	k, err := s.sshkeys.Create(ctx, sc.ProjectID, req.Body.Name, req.Body.PublicKey)
	if err != nil {
		return nil, err
	}
	return apigen.PostSshKeys201JSONResponse(apiSSHKey(k)), nil
}

func (s *server) GetSshKey(ctx context.Context, req apigen.GetSshKeyRequestObject) (apigen.GetSshKeyResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	k, err := s.sshkeys.Get(ctx, sc.ProjectID, req.Id)
	if err != nil {
		return nil, err
	}
	return apigen.GetSshKey200JSONResponse(apiSSHKey(k)), nil
}

func (s *server) PutSshKey(ctx context.Context, req apigen.PutSshKeyRequestObject) (apigen.PutSshKeyResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	k, err := s.sshkeys.Rename(ctx, sc.ProjectID, req.Id, req.Body.Name)
	if err != nil {
		return nil, err
	}
	return apigen.PutSshKey200JSONResponse(apiSSHKey(k)), nil
}

func (s *server) DeleteSshKey(ctx context.Context, req apigen.DeleteSshKeyRequestObject) (apigen.DeleteSshKeyResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.sshkeys.Delete(ctx, sc.ProjectID, req.Id); err != nil {
		return nil, err
	}
	return apigen.DeleteSshKey204Response{}, nil
}

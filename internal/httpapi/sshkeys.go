package httpapi

import (
	"context"

	"github.com/Hexeption/nebla/internal/apigen"
	"github.com/Hexeption/nebla/internal/sshkeys"
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

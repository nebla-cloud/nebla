package httpapi

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/nebla-cloud/nebla/internal/apierr"
	"github.com/nebla-cloud/nebla/internal/apigen"
	"github.com/nebla-cloud/nebla/internal/auth"
)

var errPasskeysOff = apierr.New(http.StatusNotFound, "not_found", "not found")

func apiPasskey(p auth.Passkey) apigen.Passkey {
	return apigen.Passkey{Id: p.ID, Name: p.Name, CreatedAt: p.CreatedAt, LastUsedAt: p.LastUsedAt}
}

func (s *server) rpName(ctx context.Context) string {
	return s.deps.Ext.Branding.Get(ctx).ProductName
}

func options(raw json.RawMessage) (map[string]any, error) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func (s *server) GetMePasskeys(ctx context.Context, _ apigen.GetMePasskeysRequestObject) (apigen.GetMePasskeysResponseObject, error) {
	u, _ := UserFrom(ctx)
	list, err := s.passkeys.List(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	out := apigen.PasskeyList{Passkeys: []apigen.Passkey{}}
	for _, p := range list {
		out.Passkeys = append(out.Passkeys, apiPasskey(p))
	}
	return apigen.GetMePasskeys200JSONResponse(out), nil
}

func (s *server) PostMePasskeysBegin(ctx context.Context, req apigen.PostMePasskeysBeginRequestObject) (apigen.PostMePasskeysBeginResponseObject, error) {
	if !s.passkeys.Enabled() {
		return nil, errPasskeysOff
	}
	u, _ := UserFrom(ctx)
	if err := s.limitPassword(ctx, u.Email); err != nil {
		return nil, err
	}
	code := ""
	if req.Body.TotpCode != nil {
		code = *req.Body.TotpCode
	}
	id, raw, err := s.passkeys.BeginRegistration(ctx, u.ID, req.Body.Password, code, s.rpName(ctx))
	if err != nil {
		return nil, err
	}
	opts, err := options(raw)
	if err != nil {
		return nil, err
	}
	return apigen.PostMePasskeysBegin200JSONResponse{ChallengeId: id, Options: opts}, nil
}

func (s *server) PostMePasskeysFinish(ctx context.Context, req apigen.PostMePasskeysFinishRequestObject) (apigen.PostMePasskeysFinishResponseObject, error) {
	u, _ := UserFrom(ctx)
	cred, err := json.Marshal(req.Body.Credential)
	if err != nil {
		return nil, err
	}
	p, err := s.passkeys.FinishRegistration(ctx, u.ID, req.Body.ChallengeId, cred)
	if err != nil {
		return nil, err
	}
	return apigen.PostMePasskeysFinish201JSONResponse(apiPasskey(p)), nil
}

func (s *server) PutMePasskey(ctx context.Context, req apigen.PutMePasskeyRequestObject) (apigen.PutMePasskeyResponseObject, error) {
	u, _ := UserFrom(ctx)
	p, err := s.passkeys.Rename(ctx, u.ID, req.Id, req.Body.Name)
	if err != nil {
		return nil, err
	}
	return apigen.PutMePasskey200JSONResponse(apiPasskey(p)), nil
}

func (s *server) DeleteMePasskey(ctx context.Context, req apigen.DeleteMePasskeyRequestObject) (apigen.DeleteMePasskeyResponseObject, error) {
	u, _ := UserFrom(ctx)
	if err := s.passkeys.Remove(ctx, u.ID, req.Id, sessionToken(ctx)); err != nil {
		return nil, err
	}
	return apigen.DeleteMePasskey204Response{}, nil
}

func (s *server) PostAuthPasskeyBegin(ctx context.Context, _ apigen.PostAuthPasskeyBeginRequestObject) (apigen.PostAuthPasskeyBeginResponseObject, error) {
	id, raw, err := s.passkeys.BeginLogin(ctx, s.rpName(ctx))
	if err != nil {
		return nil, err
	}
	opts, err := options(raw)
	if err != nil {
		return nil, err
	}
	return apigen.PostAuthPasskeyBegin200JSONResponse{ChallengeId: id, Options: opts}, nil
}

func (s *server) PostAuthPasskeyFinish(ctx context.Context, req apigen.PostAuthPasskeyFinishRequestObject) (apigen.PostAuthPasskeyFinishResponseObject, error) {
	cred, err := json.Marshal(req.Body.Credential)
	if err != nil {
		return nil, err
	}
	u, tok, err := s.passkeys.FinishLogin(ctx, req.Body.ChallengeId, cred)
	if err != nil {
		return nil, err
	}
	return apigen.PostAuthPasskeyFinish200JSONResponse{
		Body:    apiUser(u),
		Headers: apigen.PostAuthPasskeyFinish200ResponseHeaders{SetCookie: new(s.sessionCookie(tok).String())},
	}, nil
}

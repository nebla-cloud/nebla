package httpapi

import (
	"context"
	"net/http"

	"github.com/Hexeption/nebla/internal/apierr"
	"github.com/Hexeption/nebla/internal/apigen"
	"github.com/Hexeption/nebla/internal/auth"
	"github.com/Hexeption/nebla/internal/realip"
)

func apiUser(u auth.User) apigen.User {
	return apigen.User{Id: u.ID, Email: u.Email, Name: u.Name, IsAdmin: u.IsAdmin, TotpEnabled: u.TOTPEnabled}
}

func (s *server) sessionCookie(token string) *http.Cookie {
	return &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   int(auth.SessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   s.deps.Config.HTTPS(),
		SameSite: http.SameSiteLaxMode,
	}
}

func (s *server) PostSetup(ctx context.Context, req apigen.PostSetupRequestObject) (apigen.PostSetupResponseObject, error) {
	u, err := s.auth.Setup(ctx, req.Body.Email, req.Body.Password, req.Body.Name)
	if err != nil {
		return nil, err
	}
	return apigen.PostSetup201JSONResponse(apiUser(u)), nil
}

func (s *server) PostSignup(ctx context.Context, req apigen.PostSignupRequestObject) (apigen.PostSignupResponseObject, error) {
	if !s.deps.Ext.SelfSignupEnabled(ctx) {
		return nil, apierr.New(http.StatusNotFound, "not_found", "not found")
	}
	u, err := s.auth.CreateUser(ctx, req.Body.Email, req.Body.Password, req.Body.Name, false)
	if err != nil {
		return nil, err
	}
	tok, err := s.auth.NewSession(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	return apigen.PostSignup201JSONResponse{
		Body:    apiUser(u),
		Headers: apigen.PostSignup201ResponseHeaders{SetCookie: new(s.sessionCookie(tok).String())},
	}, nil
}

func (s *server) PostAuthLogin(ctx context.Context, req apigen.PostAuthLoginRequestObject) (apigen.PostAuthLoginResponseObject, error) {
	ip := ""
	if r := requestFrom(ctx); r != nil {
		ip = realip.ClientIP(r, s.deps.Config.TrustedProxies)
	}
	if !s.loginLimit.Allow(auth.NormalizeEmail(req.Body.Email) + "|" + ip) {
		return nil, apierr.New(http.StatusTooManyRequests, "rate_limited", "too many login attempts, try again later")
	}
	code := ""
	if req.Body.TotpCode != nil {
		code = *req.Body.TotpCode
	}
	tok, err := s.auth.Login(ctx, req.Body.Email, req.Body.Password, code)
	if err != nil {
		return nil, err
	}
	u, err := s.auth.UserFromSession(ctx, tok)
	if err != nil {
		return nil, err
	}
	return apigen.PostAuthLogin200JSONResponse{
		Body:    apiUser(u),
		Headers: apigen.PostAuthLogin200ResponseHeaders{SetCookie: new(s.sessionCookie(tok).String())},
	}, nil
}

func (s *server) PostAuthLogout(ctx context.Context, _ apigen.PostAuthLogoutRequestObject) (apigen.PostAuthLogoutResponseObject, error) {
	if r := requestFrom(ctx); r != nil {
		if c, err := r.Cookie(sessionCookie); err == nil {
			if err := s.auth.Logout(ctx, c.Value); err != nil {
				return nil, err
			}
		}
	}
	clear := s.sessionCookie("")
	clear.MaxAge = -1
	return apigen.PostAuthLogout204Response{Headers: apigen.PostAuthLogout204ResponseHeaders{SetCookie: new(clear.String())}}, nil
}

func (s *server) GetMe(ctx context.Context, _ apigen.GetMeRequestObject) (apigen.GetMeResponseObject, error) {
	u, _ := UserFrom(ctx)
	return apigen.GetMe200JSONResponse(apiUser(u)), nil
}

func (s *server) PostAuthTotpEnroll(ctx context.Context, _ apigen.PostAuthTotpEnrollRequestObject) (apigen.PostAuthTotpEnrollResponseObject, error) {
	u, _ := UserFrom(ctx)
	secret, url, err := s.auth.EnrollTOTP(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	out := apigen.PostAuthTotpEnroll200JSONResponse{}
	out.Secret, out.OtpauthUrl = secret, url
	return out, nil
}

func (s *server) PostAuthTotpVerify(ctx context.Context, req apigen.PostAuthTotpVerifyRequestObject) (apigen.PostAuthTotpVerifyResponseObject, error) {
	u, _ := UserFrom(ctx)
	if err := s.auth.VerifyTOTP(ctx, u.ID, req.Body.Code); err != nil {
		return nil, err
	}
	return apigen.PostAuthTotpVerify204Response{}, nil
}

func (s *server) PostAuthTotpDisable(ctx context.Context, req apigen.PostAuthTotpDisableRequestObject) (apigen.PostAuthTotpDisableResponseObject, error) {
	u, _ := UserFrom(ctx)
	if err := s.auth.DisableTOTP(ctx, u.ID, req.Body.Code); err != nil {
		return nil, err
	}
	return apigen.PostAuthTotpDisable204Response{}, nil
}

func (s *server) GetAdminSettings(ctx context.Context, _ apigen.GetAdminSettingsRequestObject) (apigen.GetAdminSettingsResponseObject, error) {
	return apigen.GetAdminSettings200JSONResponse{SelfSignup: s.settings.Bool(ctx, "self_signup", false)}, nil
}

func (s *server) PutAdminSettings(ctx context.Context, req apigen.PutAdminSettingsRequestObject) (apigen.PutAdminSettingsResponseObject, error) {
	if err := s.settings.SetBool(ctx, "self_signup", req.Body.SelfSignup); err != nil {
		return nil, err
	}
	return apigen.PutAdminSettings200JSONResponse{SelfSignup: req.Body.SelfSignup}, nil
}

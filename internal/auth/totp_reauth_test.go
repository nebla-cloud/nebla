package auth_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/require"
)

func TestEnrollRequiresPassword(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	u, err := s.Setup(ctx, "victim@example.com", pw, "V")
	require.NoError(t, err)
	_, _, err = s.EnrollTOTP(ctx, u.ID, "wrong-password")
	requireCode(t, err, http.StatusUnauthorized, "invalid_credentials")
	_, err = s.Login(ctx, "victim@example.com", pw, "")
	require.NoError(t, err)
}

func TestTotpChangeRevokesOtherSessions(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	u, err := s.Setup(ctx, "a@example.com", pw, "A")
	require.NoError(t, err)
	tok1, err := s.Login(ctx, "a@example.com", pw, "")
	require.NoError(t, err)
	tok2, err := s.Login(ctx, "a@example.com", pw, "")
	require.NoError(t, err)

	secret, _, err := s.EnrollTOTP(ctx, u.ID, pw)
	require.NoError(t, err)
	code, _ := totp.GenerateCode(secret, time.Now())
	require.NoError(t, s.VerifyTOTP(ctx, u.ID, code, tok1))

	_, err = s.UserFromSession(ctx, tok1)
	require.NoError(t, err)
	_, err = s.UserFromSession(ctx, tok2)
	requireCode(t, err, http.StatusUnauthorized, "unauthorized")

	// disable keeps the calling session and revokes the rest
	code, _ = totp.GenerateCode(secret, time.Now())
	tok3, err := s.Login(ctx, "a@example.com", pw, code)
	require.NoError(t, err)
	require.NoError(t, s.DisableTOTP(ctx, u.ID, code, pw, tok3))
	_, err = s.UserFromSession(ctx, tok3)
	require.NoError(t, err)
	_, err = s.UserFromSession(ctx, tok1)
	requireCode(t, err, http.StatusUnauthorized, "unauthorized")
}

func TestDisableRequiresPassword(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	u, err := s.Setup(ctx, "a@example.com", pw, "A")
	require.NoError(t, err)
	secret, _, err := s.EnrollTOTP(ctx, u.ID, pw)
	require.NoError(t, err)
	code, _ := totp.GenerateCode(secret, time.Now())
	require.NoError(t, s.VerifyTOTP(ctx, u.ID, code, ""))

	err = s.DisableTOTP(ctx, u.ID, code, "wrong-password", "")
	requireCode(t, err, http.StatusUnauthorized, "invalid_credentials")
	_, err = s.Login(ctx, "a@example.com", pw, "")
	requireCode(t, err, http.StatusUnauthorized, "totp_required")
}

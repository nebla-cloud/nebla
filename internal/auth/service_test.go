package auth_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/Hexeption/nebla/internal/apierr"
	"github.com/Hexeption/nebla/internal/auth"
	"github.com/Hexeption/nebla/internal/ext"
	"github.com/Hexeption/nebla/internal/testutil"
	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/require"
)

const pw = "correct-horse-battery"

func newSvc(t *testing.T) *auth.Service {
	return auth.New(testutil.NewDB(t), ext.Defaults(nil).Registration)
}

func requireCode(t *testing.T, err error, status int, code string) {
	t.Helper()
	var e apierr.E
	require.True(t, errors.As(err, &e), "got %v", err)
	require.Equal(t, status, e.Status)
	require.Equal(t, code, e.Code)
}

func TestPasswordHashRoundTrip(t *testing.T) {
	h, err := auth.HashPassword(pw)
	require.NoError(t, err)
	require.Contains(t, h, "$argon2id$v=19$m=65536,t=3,p=2$")
	require.True(t, auth.VerifyPassword(h, pw))
	require.False(t, auth.VerifyPassword(h, "nope"))
	require.False(t, auth.VerifyPassword("garbage", pw))
}

func TestSetupOnlyOnce(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	u, err := s.Setup(ctx, "Admin@Example.com", pw, "Admin")
	require.NoError(t, err)
	require.True(t, u.IsAdmin)
	require.Equal(t, "admin@example.com", u.Email)
	_, err = s.Setup(ctx, "b@example.com", pw, "B")
	requireCode(t, err, http.StatusConflict, "already_setup")
}

func TestShortPasswordRejected(t *testing.T) {
	_, err := newSvc(t).Setup(context.Background(), "a@example.com", "short", "A")
	requireCode(t, err, http.StatusUnprocessableEntity, "invalid_input")
}

func TestLoginAndSession(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	_, err := s.Setup(ctx, "a@example.com", pw, "A")
	require.NoError(t, err)
	_, err = s.Login(ctx, "a@example.com", "wrong-password", "")
	requireCode(t, err, http.StatusUnauthorized, "invalid_credentials")
	_, err = s.Login(ctx, "nobody@example.com", pw, "")
	requireCode(t, err, http.StatusUnauthorized, "invalid_credentials")
	tok, err := s.Login(ctx, "A@example.com", pw, "")
	require.NoError(t, err)
	u, err := s.UserFromSession(ctx, tok)
	require.NoError(t, err)
	require.Equal(t, "a@example.com", u.Email)
	require.NoError(t, s.Logout(ctx, tok))
	_, err = s.UserFromSession(ctx, tok)
	requireCode(t, err, http.StatusUnauthorized, "unauthorized")
}

func TestLoginRequiresTOTPWhenEnabled(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	u, err := s.Setup(ctx, "a@example.com", pw, "A")
	require.NoError(t, err)
	secret, url, err := s.EnrollTOTP(ctx, u.ID)
	require.NoError(t, err)
	require.Contains(t, url, "otpauth://totp/Nebla:")
	code, err := totp.GenerateCode(secret, time.Now())
	require.NoError(t, err)
	requireCode(t, s.VerifyTOTP(ctx, u.ID, "000000"), http.StatusUnauthorized, "invalid_code")
	require.NoError(t, s.VerifyTOTP(ctx, u.ID, code))

	_, err = s.Login(ctx, "a@example.com", pw, "")
	requireCode(t, err, http.StatusUnauthorized, "totp_required")
	_, err = s.Login(ctx, "a@example.com", pw, "000000")
	requireCode(t, err, http.StatusUnauthorized, "invalid_credentials")
	_, err = s.Login(ctx, "a@example.com", "wrong-password", "")
	requireCode(t, err, http.StatusUnauthorized, "invalid_credentials")
	code, _ = totp.GenerateCode(secret, time.Now())
	_, err = s.Login(ctx, "a@example.com", pw, code)
	require.NoError(t, err)

	requireCode(t, s.DisableTOTP(ctx, u.ID, "000000"), http.StatusUnauthorized, "invalid_code")
	require.NoError(t, s.DisableTOTP(ctx, u.ID, code))
	_, err = s.Login(ctx, "a@example.com", pw, "")
	require.NoError(t, err)
}

// Package auth handles accounts, passwords, sessions and TOTP.
package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Hexeption/nebla/internal/apierr"
	"github.com/Hexeption/nebla/internal/db/gen"
	"github.com/Hexeption/nebla/internal/ext"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type User struct {
	ID          int64
	Email       string
	Name        string
	IsAdmin     bool
	TOTPEnabled bool
}

func toUser(u gen.User) User {
	return User{ID: u.ID, Email: u.Email, Name: u.Name, IsAdmin: u.IsAdmin, TOTPEnabled: u.TotpEnabled}
}

type Service struct {
	pool *pgxpool.Pool
	q    *gen.Queries
	reg  ext.Registration
	// dummyHash keeps unknown-email logins as slow as real ones.
	dummyHash string
}

func New(pool *pgxpool.Pool, reg ext.Registration) *Service {
	h, _ := HashPassword(context.Background(), "nebla-dummy-password")
	return &Service{pool: pool, q: gen.New(pool), reg: reg, dummyHash: h}
}

var (
	errInvalidCreds = apierr.New(http.StatusUnauthorized, "invalid_credentials", "invalid email or password")
	errUnauthorized = apierr.New(http.StatusUnauthorized, "unauthorized", "authentication required")
	errInvalidCode  = apierr.New(http.StatusUnauthorized, "invalid_code", "invalid verification code")
)

// NormalizeEmail trims and lowercases an email for lookups and rate-limit keys.
func NormalizeEmail(e string) string { return strings.ToLower(strings.TrimSpace(e)) }

func validate(email, pw, name string) (string, error) {
	email = NormalizeEmail(email)
	if !strings.Contains(email, "@") || len(email) < 3 {
		return "", apierr.New(http.StatusUnprocessableEntity, "invalid_input", "a valid email is required")
	}
	if len(pw) < MinPasswordLen {
		return "", apierr.New(http.StatusUnprocessableEntity, "invalid_input", "password must be at least 10 characters")
	}
	if strings.TrimSpace(name) == "" {
		return "", apierr.New(http.StatusUnprocessableEntity, "invalid_input", "name is required")
	}
	return email, nil
}

// Setup creates the first admin; it fails with already_setup once any user exists.
func (s *Service) Setup(ctx context.Context, email, pw, name string) (User, error) {
	email, err := validate(email, pw, name)
	if err != nil {
		return User{}, err
	}
	hash, err := HashPassword(ctx, pw)
	if err != nil {
		return User{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "select pg_advisory_xact_lock(4001)"); err != nil {
		return User{}, err
	}
	qt := s.q.WithTx(tx)
	n, err := qt.CountUsers(ctx)
	if err != nil {
		return User{}, err
	}
	if n > 0 {
		return User{}, apierr.New(http.StatusConflict, "already_setup", "setup has already been completed")
	}
	row, err := qt.CreateUser(ctx, gen.CreateUserParams{Email: email, Name: name, PasswordHash: hash, IsAdmin: true})
	if err != nil {
		return User{}, mapCreateErr(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return User{}, err
	}
	if err := s.reg.OnUserCreated(ctx, row.ID); err != nil {
		return User{}, err
	}
	return toUser(row), nil
}

// CreateUser creates an account and fires the registration hook.
func (s *Service) CreateUser(ctx context.Context, email, pw, name string, admin bool) (User, error) {
	email, err := validate(email, pw, name)
	if err != nil {
		return User{}, err
	}
	hash, err := HashPassword(ctx, pw)
	if err != nil {
		return User{}, err
	}
	row, err := s.q.CreateUser(ctx, gen.CreateUserParams{Email: email, Name: name, PasswordHash: hash, IsAdmin: admin})
	if err != nil {
		return User{}, mapCreateErr(err)
	}
	if err := s.reg.OnUserCreated(ctx, row.ID); err != nil {
		return User{}, err
	}
	return toUser(row), nil
}

func mapCreateErr(err error) error {
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == "23505" {
		return apierr.New(http.StatusConflict, "email_taken", "email is already registered")
	}
	return err
}

// Login verifies credentials (and the TOTP code when enabled) and returns a session token.
func (s *Service) Login(ctx context.Context, email, pw, code string) (string, error) {
	u, err := s.q.GetUserByEmail(ctx, NormalizeEmail(email))
	if errors.Is(err, pgx.ErrNoRows) {
		VerifyPassword(ctx, s.dummyHash, pw)
		return "", errInvalidCreds
	}
	if err != nil {
		return "", err
	}
	if !VerifyPassword(ctx, u.PasswordHash, pw) {
		return "", errInvalidCreds
	}
	if u.TotpEnabled {
		if code == "" {
			return "", apierr.New(http.StatusUnauthorized, "totp_required", "a TOTP code is required")
		}
		if !u.TotpSecret.Valid || !validTOTP(u.TotpSecret.String, code) {
			return "", errInvalidCreds
		}
	}
	return s.newSession(ctx, u.ID)
}

// NewSession starts a session for an existing user.
func (s *Service) NewSession(ctx context.Context, userID int64) (string, error) {
	return s.newSession(ctx, userID)
}

func (s *Service) newSession(ctx context.Context, userID int64) (string, error) {
	tok, hash, err := newToken()
	if err != nil {
		return "", err
	}
	err = s.q.CreateSession(ctx, gen.CreateSessionParams{
		TokenHash: hash, UserID: userID, ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(SessionTTL), Valid: true},
	})
	return tok, err
}

func (s *Service) Logout(ctx context.Context, token string) error {
	return s.q.DeleteSession(ctx, hashToken(token))
}

func (s *Service) UserFromSession(ctx context.Context, token string) (User, error) {
	if token == "" {
		return User{}, errUnauthorized
	}
	u, err := s.q.GetSessionUser(ctx, hashToken(token))
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, errUnauthorized
	}
	if err != nil {
		return User{}, err
	}
	return toUser(u), nil
}

// EnrollTOTP stores a pending secret and returns it with its otpauth URL.
func (s *Service) EnrollTOTP(ctx context.Context, userID int64) (secret, url string, err error) {
	u, err := s.q.GetUser(ctx, userID)
	if err != nil {
		return "", "", err
	}
	if u.TotpEnabled {
		return "", "", apierr.New(http.StatusConflict, "totp_already_enabled", "TOTP is already enabled")
	}
	key, err := generateTOTP(u.Email)
	if err != nil {
		return "", "", err
	}
	if err := s.q.SetTOTPPending(ctx, gen.SetTOTPPendingParams{ID: userID, TotpPendingSecret: pgtype.Text{String: key.Secret(), Valid: true}}); err != nil {
		return "", "", err
	}
	return key.Secret(), key.URL(), nil
}

// VerifyTOTP activates the pending secret when code is valid.
func (s *Service) VerifyTOTP(ctx context.Context, userID int64, code string) error {
	u, err := s.q.GetUser(ctx, userID)
	if err != nil {
		return err
	}
	if !u.TotpPendingSecret.Valid {
		return apierr.New(http.StatusUnprocessableEntity, "totp_not_enrolled", "start enrollment first")
	}
	if !validTOTP(u.TotpPendingSecret.String, code) {
		return errInvalidCode
	}
	return s.q.EnableTOTP(ctx, userID)
}

func (s *Service) DisableTOTP(ctx context.Context, userID int64, code string) error {
	u, err := s.q.GetUser(ctx, userID)
	if err != nil {
		return err
	}
	if !u.TotpEnabled || !u.TotpSecret.Valid || !validTOTP(u.TotpSecret.String, code) {
		return errInvalidCode
	}
	return s.q.DisableTOTP(ctx, userID)
}

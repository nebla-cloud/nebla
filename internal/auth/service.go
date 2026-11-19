// Package auth handles accounts, passwords, sessions and TOTP.
package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/Hexeption/nebla/internal/apierr"
	"github.com/Hexeption/nebla/internal/ent"
	"github.com/Hexeption/nebla/internal/ent/session"
	"github.com/Hexeption/nebla/internal/ent/user"
	"github.com/Hexeption/nebla/internal/ext"
	"github.com/jackc/pgx/v5/pgconn"
)

type User struct {
	ID          int64
	Email       string
	Name        string
	IsAdmin     bool
	TOTPEnabled bool
}

func toUser(u *ent.User) User {
	return User{ID: u.ID, Email: u.Email, Name: u.Name, IsAdmin: u.IsAdmin, TOTPEnabled: u.TotpEnabled}
}

type Service struct {
	db  *ent.Client
	reg ext.Registration
	// dummyHash keeps unknown-email logins as slow as real ones.
	dummyHash string
}

func New(db *ent.Client, reg ext.Registration) *Service {
	h, _ := HashPassword(context.Background(), "nebla-dummy-password")
	return &Service{db: db, reg: reg, dummyHash: h}
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
	tx, err := s.db.Tx(ctx)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "select pg_advisory_xact_lock(4001)"); err != nil {
		return User{}, err
	}
	n, err := tx.User.Query().Count(ctx)
	if err != nil {
		return User{}, err
	}
	if n > 0 {
		return User{}, apierr.New(http.StatusConflict, "already_setup", "setup has already been completed")
	}
	row, err := tx.User.Create().SetEmail(email).SetName(name).SetPasswordHash(hash).SetIsAdmin(true).Save(ctx)
	if err != nil {
		return User{}, mapCreateErr(err)
	}
	if err := tx.Commit(); err != nil {
		return User{}, err
	}
	if err := s.reg.OnUserCreated(ctx, row.ID); err != nil {
		return User{}, err
	}
	return toUser(row), nil
}

// CreateUser creates an account and fires the registration hook.
func (s *Service) CreateUser(ctx context.Context, email, pw, name string, admin bool) (User, error) {
	u, err := s.CreateUserTx(ctx, s.db, email, pw, name, admin)
	if err != nil {
		return User{}, err
	}
	if err := s.Registered(ctx, u.ID); err != nil {
		return User{}, err
	}
	return u, nil
}

// CreateUserTx inserts a user through db (usually a tx); call Registered after commit.
func (s *Service) CreateUserTx(ctx context.Context, db *ent.Client, email, pw, name string, admin bool) (User, error) {
	email, err := validate(email, pw, name)
	if err != nil {
		return User{}, err
	}
	hash, err := HashPassword(ctx, pw)
	if err != nil {
		return User{}, err
	}
	row, err := db.User.Create().SetEmail(email).SetName(name).SetPasswordHash(hash).SetIsAdmin(admin).Save(ctx)
	if err != nil {
		return User{}, mapCreateErr(err)
	}
	return toUser(row), nil
}

// Registered fires the registration hook for a newly committed user.
func (s *Service) Registered(ctx context.Context, userID int64) error {
	return s.reg.OnUserCreated(ctx, userID)
}

// ListUsers returns one page of users ordered by id, plus the total count.
func (s *Service) ListUsers(ctx context.Context, page, perPage int) ([]User, int, error) {
	total, err := s.db.User.Query().Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	rows, err := s.db.User.Query().Order(user.ByID()).Offset((page - 1) * perPage).Limit(perPage).All(ctx)
	if err != nil {
		return nil, 0, err
	}
	out := make([]User, len(rows))
	for i, r := range rows {
		out[i] = toUser(r)
	}
	return out, total, nil
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
	u, err := s.db.User.Query().Where(user.Email(NormalizeEmail(email))).Only(ctx)
	if ent.IsNotFound(err) {
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
		if u.TotpSecret == nil || !validTOTP(*u.TotpSecret, code) {
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
	err = s.db.Session.Create().SetID(hash).SetUserID(userID).SetExpiresAt(time.Now().Add(SessionTTL)).Exec(ctx)
	return tok, err
}

func (s *Service) Logout(ctx context.Context, token string) error {
	_, err := s.db.Session.Delete().Where(session.ID(hashToken(token))).Exec(ctx)
	return err
}

func (s *Service) UserFromSession(ctx context.Context, token string) (User, error) {
	if token == "" {
		return User{}, errUnauthorized
	}
	u, err := s.db.Session.Query().Where(session.ID(hashToken(token)), sessionLive).QueryUser().Only(ctx)
	if ent.IsNotFound(err) {
		return User{}, errUnauthorized
	}
	if err != nil {
		return User{}, err
	}
	return toUser(u), nil
}

// sessionLive checks expiry against the database clock.
func sessionLive(s *entsql.Selector) {
	s.Where(entsql.ExprP(s.C(session.FieldExpiresAt) + " > now()"))
}

// EnrollTOTP stores a pending secret and returns it with its otpauth URL.
func (s *Service) EnrollTOTP(ctx context.Context, userID int64) (secret, url string, err error) {
	u, err := s.db.User.Get(ctx, userID)
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
	if err := s.db.User.UpdateOneID(userID).SetTotpPendingSecret(key.Secret()).Exec(ctx); err != nil {
		return "", "", err
	}
	return key.Secret(), key.URL(), nil
}

// VerifyTOTP activates the pending secret when code is valid.
func (s *Service) VerifyTOTP(ctx context.Context, userID int64, code string) error {
	u, err := s.db.User.Get(ctx, userID)
	if err != nil {
		return err
	}
	if u.TotpPendingSecret == nil {
		return apierr.New(http.StatusUnprocessableEntity, "totp_not_enrolled", "start enrollment first")
	}
	if !validTOTP(*u.TotpPendingSecret, code) {
		return errInvalidCode
	}
	return s.db.User.UpdateOneID(userID).
		Modify(func(u *entsql.UpdateBuilder) { u.Set(user.FieldTotpSecret, entsql.Expr(user.FieldTotpPendingSecret)) }).
		ClearTotpPendingSecret().SetTotpEnabled(true).Exec(ctx)
}

func (s *Service) DisableTOTP(ctx context.Context, userID int64, code string) error {
	u, err := s.db.User.Get(ctx, userID)
	if err != nil {
		return err
	}
	if !u.TotpEnabled || u.TotpSecret == nil || !validTOTP(*u.TotpSecret, code) {
		return errInvalidCode
	}
	return s.db.User.UpdateOneID(userID).ClearTotpSecret().ClearTotpPendingSecret().SetTotpEnabled(false).Exec(ctx)
}

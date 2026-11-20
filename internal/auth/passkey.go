package auth

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/nebla-cloud/nebla/internal/apierr"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/user"
	"github.com/nebla-cloud/nebla/internal/ent/webauthnchallenge"
	"github.com/nebla-cloud/nebla/internal/ent/webauthncredential"
)

const (
	MaxPasskeys  = 10
	ChallengeTTL = 5 * time.Minute
)

var (
	errNotFound       = apierr.New(http.StatusNotFound, "not_found", "not found")
	errPasskeyLimit   = apierr.New(http.StatusConflict, "passkey_limit", "you can have at most 10 passkeys")
	errPasskeyExists  = apierr.New(http.StatusConflict, "passkey_exists", "that passkey is already added")
	errPasskeyExpired = apierr.New(http.StatusUnprocessableEntity, "passkey_invalid", "that request expired, try again")
	errPasskeyInvalid = apierr.New(http.StatusUnprocessableEntity, "passkey_invalid", "that passkey couldn't be verified, try again")
	errPasskeyFailed  = apierr.New(http.StatusUnauthorized, "passkey_failed", "that passkey didn't work")
)

type Passkey struct {
	ID         int64
	Name       string
	CreatedAt  time.Time
	LastUsedAt *time.Time
}

func toPasskey(c *ent.WebauthnCredential) Passkey {
	return Passkey{ID: c.ID, Name: c.Name, CreatedAt: c.CreatedAt, LastUsedAt: c.LastUsedAt}
}

// Passkeys runs WebAuthn registration and login; it is disabled when origin is "".
type Passkeys struct {
	db     *ent.Client
	svc    *Service
	origin string
	rpID   string
}

func NewPasskeys(db *ent.Client, svc *Service, origin string) (*Passkeys, error) {
	p := &Passkeys{db: db, svc: svc, origin: origin}
	if origin == "" {
		return p, nil
	}
	u, err := url.Parse(origin)
	if err != nil {
		return nil, err
	}
	p.rpID = u.Hostname()
	if _, err := p.webauthn(p.rpID); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *Passkeys) Enabled() bool { return p.origin != "" }

func (p *Passkeys) webauthn(rpName string) (*webauthn.WebAuthn, error) {
	if rpName == "" {
		rpName = p.rpID
	}
	return webauthn.New(&webauthn.Config{RPID: p.rpID, RPDisplayName: rpName, RPOrigins: []string{p.origin}})
}

// waUser adapts an ent user to webauthn.User.
type waUser struct {
	u     *ent.User
	creds []webauthn.Credential
}

func (w *waUser) WebAuthnID() []byte                         { return handleOf(w.u) }
func (w *waUser) WebAuthnName() string                       { return w.u.Email }
func (w *waUser) WebAuthnDisplayName() string                { return w.u.Name }
func (w *waUser) WebAuthnCredentials() []webauthn.Credential { return w.creds }

// handleOf returns the user's handle; ent scans NULL bytes as a pointer to an empty slice.
func handleOf(u *ent.User) []byte {
	if u.WebauthnHandle == nil {
		return nil
	}
	return *u.WebauthnHandle
}

func (p *Passkeys) List(ctx context.Context, userID int64) ([]Passkey, error) {
	if !p.Enabled() {
		return nil, errNotFound
	}
	rows, err := p.db.WebauthnCredential.Query().Where(webauthncredential.UserID(userID)).
		Order(webauthncredential.ByID()).All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Passkey, len(rows))
	for i, r := range rows {
		out[i] = toPasskey(r)
	}
	return out, nil
}

func (p *Passkeys) BeginRegistration(ctx context.Context, userID int64, password, totpCode, rpName string) (string, json.RawMessage, error) {
	if !p.Enabled() {
		return "", nil, errNotFound
	}
	u, err := p.db.User.Get(ctx, userID)
	if err != nil {
		return "", nil, err
	}
	if !VerifyPassword(ctx, u.PasswordHash, password) {
		return "", nil, errInvalidCreds
	}
	if u.TotpEnabled {
		if totpCode == "" {
			return "", nil, apierr.New(http.StatusUnauthorized, "totp_required", "a TOTP code is required")
		}
		if u.TotpSecret == nil || !validTOTP(*u.TotpSecret, totpCode) {
			return "", nil, errInvalidCode
		}
	}
	creds, err := p.db.WebauthnCredential.Query().Where(webauthncredential.UserID(userID)).All(ctx)
	if err != nil {
		return "", nil, err
	}
	if len(creds) >= MaxPasskeys {
		return "", nil, errPasskeyLimit
	}
	if len(handleOf(u)) == 0 {
		handle := make([]byte, 32)
		if _, err := rand.Read(handle); err != nil {
			return "", nil, err
		}
		err := p.db.User.Update().Where(user.ID(userID), user.WebauthnHandleIsNil()).SetWebauthnHandle(handle).Exec(ctx)
		if err != nil {
			return "", nil, err
		}
		if u, err = p.db.User.Get(ctx, userID); err != nil {
			return "", nil, err
		}
	}
	existing := make(webauthn.Credentials, len(creds))
	for i, c := range creds {
		existing[i] = c.Data
	}
	wa, err := p.webauthn(rpName)
	if err != nil {
		return "", nil, err
	}
	creation, session, err := wa.BeginRegistration(&waUser{u: u},
		webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
			RequireResidentKey: protocol.ResidentKeyRequired(),
			ResidentKey:        protocol.ResidentKeyRequirementRequired,
			UserVerification:   protocol.VerificationRequired,
		}),
		webauthn.WithConveyancePreference(protocol.PreferNoAttestation),
		webauthn.WithExclusions(existing.CredentialDescriptors()))
	if err != nil {
		return "", nil, err
	}
	id, err := p.saveChallenge(ctx, webauthnchallenge.PurposeRegister, &userID, *session)
	if err != nil {
		return "", nil, err
	}
	opts, err := json.Marshal(creation.Response)
	return id, opts, err
}

func (p *Passkeys) FinishRegistration(ctx context.Context, userID int64, challengeID string, credential json.RawMessage) (Passkey, error) {
	if !p.Enabled() {
		return Passkey{}, errNotFound
	}
	session, err := p.takeChallenge(ctx, challengeID, webauthnchallenge.PurposeRegister, &userID)
	if err != nil {
		return Passkey{}, err
	}
	if session == nil {
		return Passkey{}, errPasskeyExpired
	}
	u, err := p.db.User.Get(ctx, userID)
	if err != nil {
		return Passkey{}, err
	}
	parsed, err := protocol.ParseCredentialCreationResponseBytes(credential)
	if err != nil {
		return Passkey{}, errPasskeyInvalid
	}
	// The display name isn't checked at finish.
	wa, err := p.webauthn("")
	if err != nil {
		return Passkey{}, err
	}
	cred, err := wa.CreateCredential(&waUser{u: u}, *session, parsed)
	if err != nil {
		return Passkey{}, errPasskeyInvalid
	}
	var row *ent.WebauthnCredential
	err = p.svc.inTx(ctx, func(tx *ent.Client) error {
		if _, err := tx.User.Query().Where(user.ID(userID)).ForUpdate().Only(ctx); err != nil {
			return err
		}
		n, err := tx.WebauthnCredential.Query().Where(webauthncredential.UserID(userID)).Count(ctx)
		if err != nil {
			return err
		}
		if n >= MaxPasskeys {
			return errPasskeyLimit
		}
		row, err = tx.WebauthnCredential.Create().SetUserID(userID).SetCredentialID(cred.ID).
			SetName(providerName(cred.Authenticator.AAGUID)).SetAaguid(cred.Authenticator.AAGUID).SetData(*cred).Save(ctx)
		var pe *pgconn.PgError
		if errors.As(err, &pe) && pe.Code == "23505" {
			return errPasskeyExists
		}
		return err
	})
	if err != nil {
		return Passkey{}, err
	}
	return toPasskey(row), nil
}

func (p *Passkeys) Rename(ctx context.Context, userID, id int64, name string) (Passkey, error) {
	if !p.Enabled() {
		return Passkey{}, errNotFound
	}
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 100 {
		return Passkey{}, apierr.New(http.StatusUnprocessableEntity, "invalid_input", "name must be 1-100 characters")
	}
	row, err := p.db.WebauthnCredential.UpdateOneID(id).Where(webauthncredential.UserID(userID)).SetName(name).Save(ctx)
	if ent.IsNotFound(err) {
		return Passkey{}, errNotFound
	}
	if err != nil {
		return Passkey{}, err
	}
	return toPasskey(row), nil
}

// Remove deletes the user's passkey and revokes their other sessions in one transaction.
func (p *Passkeys) Remove(ctx context.Context, userID, id int64, keepToken string) error {
	if !p.Enabled() {
		return errNotFound
	}
	return p.svc.inTx(ctx, func(tx *ent.Client) error {
		n, err := tx.WebauthnCredential.Delete().
			Where(webauthncredential.ID(id), webauthncredential.UserID(userID)).Exec(ctx)
		if err != nil {
			return err
		}
		if n == 0 {
			return errNotFound
		}
		return revokeOtherSessions(ctx, tx, userID, keepToken)
	})
}

func (p *Passkeys) BeginLogin(ctx context.Context, rpName string) (string, json.RawMessage, error) {
	if !p.Enabled() {
		return "", nil, errNotFound
	}
	if _, err := p.DeleteExpiredChallenges(ctx, 100); err != nil {
		return "", nil, err
	}
	wa, err := p.webauthn(rpName)
	if err != nil {
		return "", nil, err
	}
	assertion, session, err := wa.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		return "", nil, err
	}
	id, err := p.saveChallenge(ctx, webauthnchallenge.PurposeLogin, nil, *session)
	if err != nil {
		return "", nil, err
	}
	opts, err := json.Marshal(assertion.Response)
	return id, opts, err
}

// FinishLogin verifies a discoverable assertion and starts a session; TOTP is not asked.
func (p *Passkeys) FinishLogin(ctx context.Context, challengeID string, credential json.RawMessage) (User, string, error) {
	if !p.Enabled() {
		return User{}, "", errNotFound
	}
	session, err := p.takeChallenge(ctx, challengeID, webauthnchallenge.PurposeLogin, nil)
	if err != nil {
		return User{}, "", err
	}
	if session == nil {
		return User{}, "", errPasskeyFailed
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(credential)
	if err != nil {
		return User{}, "", errPasskeyFailed
	}
	var (
		owner *ent.User
		row   *ent.WebauthnCredential
		dbErr error
	)
	handler := func(rawID, handle []byte) (webauthn.User, error) {
		u, err := p.db.User.Query().Where(user.WebauthnHandle(handle)).Only(ctx)
		if err == nil {
			row, err = p.db.WebauthnCredential.Query().
				Where(webauthncredential.CredentialID(rawID), webauthncredential.UserID(u.ID)).Only(ctx)
		}
		if err != nil {
			if !ent.IsNotFound(err) {
				dbErr = err
			}
			return nil, err
		}
		owner = u
		return &waUser{u: u, creds: []webauthn.Credential{row.Data}}, nil
	}
	// The display name isn't checked at finish.
	wa, err := p.webauthn("")
	if err != nil {
		return User{}, "", err
	}
	_, cred, err := wa.ValidatePasskeyLogin(handler, *session, parsed)
	if dbErr != nil {
		return User{}, "", dbErr
	}
	if err != nil || cred.Authenticator.CloneWarning {
		return User{}, "", errPasskeyFailed
	}
	if err := p.db.WebauthnCredential.UpdateOneID(row.ID).SetData(*cred).SetLastUsedAt(time.Now()).Exec(ctx); err != nil {
		return User{}, "", err
	}
	tok, err := p.svc.NewSession(ctx, owner.ID)
	if err != nil {
		return User{}, "", err
	}
	return toUser(owner), tok, nil
}

// DeleteExpiredChallenges removes up to limit expired challenges (all when limit is 0); it runs even when disabled.
func (p *Passkeys) DeleteExpiredChallenges(ctx context.Context, limit int) (int, error) {
	q := `DELETE FROM webauthn_challenges WHERE expires_at <= now()`
	args := []any{}
	if limit > 0 {
		q = `DELETE FROM webauthn_challenges WHERE token_hash IN
			(SELECT token_hash FROM webauthn_challenges WHERE expires_at <= now() LIMIT $1)`
		args = append(args, limit)
	}
	res, err := p.db.ExecContext(ctx, q, args...)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

func (p *Passkeys) saveChallenge(ctx context.Context, purpose webauthnchallenge.Purpose, userID *int64, session webauthn.SessionData) (string, error) {
	tok, hash, err := newToken()
	if err != nil {
		return "", err
	}
	err = p.db.WebauthnChallenge.Create().SetID(hash).SetPurpose(purpose).SetNillableUserID(userID).
		SetSessionData(session).SetExpiresAt(time.Now().Add(ChallengeTTL)).Exec(ctx)
	return tok, err
}

// takeChallenge deletes a live challenge in one statement and returns its session, or nil when none matched.
func (p *Passkeys) takeChallenge(ctx context.Context, id string, purpose webauthnchallenge.Purpose, userID *int64) (*webauthn.SessionData, error) {
	q := `DELETE FROM webauthn_challenges WHERE token_hash = $1 AND purpose = $2 AND expires_at > now()`
	args := []any{hashToken(id), string(purpose)}
	if userID != nil {
		q += ` AND user_id = $3`
		args = append(args, *userID)
	}
	rows, err := p.db.QueryContext(ctx, q+` RETURNING session_data`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, rows.Err()
	}
	var raw []byte
	if err := rows.Scan(&raw); err != nil {
		return nil, err
	}
	var s webauthn.SessionData
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, err
	}
	return &s, rows.Close()
}

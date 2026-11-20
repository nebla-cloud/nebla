package auth_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/nebla-cloud/nebla/internal/auth"
	"github.com/nebla-cloud/nebla/internal/db"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/user"
	"github.com/nebla-cloud/nebla/internal/ent/webauthnchallenge"
	"github.com/nebla-cloud/nebla/internal/ent/webauthncredential"
	"github.com/nebla-cloud/nebla/internal/ext"
	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/pquerna/otp/totp"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/require"
)

const (
	pkOrigin = "http://localhost:8080"
	rpName   = "nebla"
)

// iCloud Keychain's AAGUID, fbfc3007-154e-4ecc-8c0b-6e020557d7bd.
var icloudAAGUID = [16]byte{0xfb, 0xfc, 0x30, 0x07, 0x15, 0x4e, 0x4e, 0xcc, 0x8c, 0x0b, 0x6e, 0x02, 0x05, 0x57, 0xd7, 0xbd}

type pkEnv struct {
	db  *ent.Client
	svc *auth.Service
	pk  *auth.Passkeys
}

func newPKEnv(t *testing.T, origin string) pkEnv {
	t.Helper()
	db := testutil.NewEnt(t)
	svc := auth.New(db, ext.Defaults(nil).Registration)
	pk, err := auth.NewPasskeys(db, svc, origin)
	require.NoError(t, err)
	return pkEnv{db: db, svc: svc, pk: pk}
}

func (e pkEnv) user(t *testing.T, email string) auth.User {
	t.Helper()
	u, err := e.svc.CreateUser(context.Background(), email, pw, "U", false)
	require.NoError(t, err)
	return u
}

// register adds a passkey for userID from a.
func (e pkEnv) register(t *testing.T, userID int64, a *testutil.Authenticator) auth.Passkey {
	t.Helper()
	ctx := context.Background()
	id, opts, err := e.pk.BeginRegistration(ctx, userID, pw, "", rpName)
	require.NoError(t, err)
	p, err := e.pk.FinishRegistration(ctx, userID, id, a.Register(opts))
	require.NoError(t, err)
	return p
}

func (e pkEnv) challengeCount(t *testing.T) int {
	t.Helper()
	n, err := e.db.WebauthnChallenge.Query().Count(context.Background())
	require.NoError(t, err)
	return n
}

func (e pkEnv) expireChallenges(t *testing.T) {
	t.Helper()
	require.NoError(t, e.db.WebauthnChallenge.Update().SetExpiresAt(time.Now().Add(-time.Minute)).Exec(context.Background()))
}

func challengeHash(id string) []byte {
	h := sha256.Sum256([]byte(id))
	return h[:]
}

func TestPasskeysDisabled(t *testing.T) {
	e := newPKEnv(t, "")
	ctx := context.Background()
	require.False(t, e.pk.Enabled())
	u := e.user(t, "a@example.com")
	_, err := e.pk.List(ctx, u.ID)
	requireCode(t, err, http.StatusNotFound, "not_found")
	_, _, err = e.pk.BeginRegistration(ctx, u.ID, pw, "", rpName)
	requireCode(t, err, http.StatusNotFound, "not_found")
	_, err = e.pk.FinishRegistration(ctx, u.ID, "x", json.RawMessage(`{}`))
	requireCode(t, err, http.StatusNotFound, "not_found")
	_, err = e.pk.Rename(ctx, u.ID, 1, "x")
	requireCode(t, err, http.StatusNotFound, "not_found")
	err = e.pk.Remove(ctx, u.ID, 1, "")
	requireCode(t, err, http.StatusNotFound, "not_found")
	_, _, err = e.pk.BeginLogin(ctx, rpName)
	requireCode(t, err, http.StatusNotFound, "not_found")
	_, _, err = e.pk.FinishLogin(ctx, "x", json.RawMessage(`{}`))
	requireCode(t, err, http.StatusNotFound, "not_found")
	_, err = e.pk.DeleteExpiredChallenges(ctx, 0)
	require.NoError(t, err)
}

func TestBeginRegistrationPassword(t *testing.T) {
	e := newPKEnv(t, pkOrigin)
	ctx := context.Background()
	require.True(t, e.pk.Enabled())
	u := e.user(t, "a@example.com")
	_, _, err := e.pk.BeginRegistration(ctx, u.ID, "wrong-password", "", rpName)
	requireCode(t, err, http.StatusUnauthorized, "invalid_credentials")
	require.Zero(t, e.challengeCount(t))

	id, opts, err := e.pk.BeginRegistration(ctx, u.ID, pw, "", rpName)
	require.NoError(t, err)
	require.NotEmpty(t, id)
	var o struct {
		RP  struct{ ID, Name string }
		Sel struct {
			ResidentKey      string `json:"residentKey"`
			UserVerification string `json:"userVerification"`
		} `json:"authenticatorSelection"`
		Attestation string `json:"attestation"`
	}
	require.NoError(t, json.Unmarshal(opts, &o))
	require.Equal(t, "localhost", o.RP.ID)
	require.Equal(t, rpName, o.RP.Name)
	require.Equal(t, "required", o.Sel.ResidentKey)
	require.Equal(t, "required", o.Sel.UserVerification)
	require.Equal(t, "none", o.Attestation)

	row, err := e.db.User.Get(ctx, u.ID)
	require.NoError(t, err)
	require.NotNil(t, row.WebauthnHandle)
	require.Len(t, *row.WebauthnHandle, 32)
	first := *row.WebauthnHandle
	_, _, err = e.pk.BeginRegistration(ctx, u.ID, pw, "", rpName)
	require.NoError(t, err)
	row, err = e.db.User.Get(ctx, u.ID)
	require.NoError(t, err)
	require.Equal(t, first, *row.WebauthnHandle)
}

func TestBeginRegistrationNeedsTOTP(t *testing.T) {
	e := newPKEnv(t, pkOrigin)
	ctx := context.Background()
	u := e.user(t, "a@example.com")
	secret, _, err := e.svc.EnrollTOTP(ctx, u.ID, pw)
	require.NoError(t, err)
	code, _ := totp.GenerateCode(secret, time.Now())
	require.NoError(t, e.svc.VerifyTOTP(ctx, u.ID, code, ""))

	_, _, err = e.pk.BeginRegistration(ctx, u.ID, pw, "", rpName)
	requireCode(t, err, http.StatusUnauthorized, "totp_required")
	_, _, err = e.pk.BeginRegistration(ctx, u.ID, pw, "000000", rpName)
	requireCode(t, err, http.StatusUnauthorized, "invalid_code")
	require.Zero(t, e.challengeCount(t))
	code, _ = totp.GenerateCode(secret, time.Now())
	_, _, err = e.pk.BeginRegistration(ctx, u.ID, pw, code, rpName)
	require.NoError(t, err)
	require.Equal(t, 1, e.challengeCount(t))
}

func TestFinishRegistrationNamesProvider(t *testing.T) {
	e := newPKEnv(t, pkOrigin)
	ctx := context.Background()
	u := e.user(t, "a@example.com")
	p := e.register(t, u.ID, testutil.NewAuthenticator(pkOrigin, icloudAAGUID))
	require.Equal(t, "iCloud Keychain", p.Name)
	require.Nil(t, p.LastUsedAt)
	require.Zero(t, e.challengeCount(t))

	p2 := e.register(t, u.ID, testutil.NewAuthenticator(pkOrigin, [16]byte{}))
	require.Equal(t, "Passkey", p2.Name)

	list, err := e.pk.List(ctx, u.ID)
	require.NoError(t, err)
	require.Len(t, list, 2)
	require.Equal(t, p.ID, list[0].ID)
	require.Equal(t, "iCloud Keychain", list[0].Name)
	require.Nil(t, list[0].LastUsedAt)
	require.False(t, list[0].CreatedAt.IsZero())
}

func TestFinishRegistrationOtherUsersChallenge(t *testing.T) {
	e := newPKEnv(t, pkOrigin)
	ctx := context.Background()
	a := e.user(t, "a@example.com")
	b := e.user(t, "b@example.com")
	id, opts, err := e.pk.BeginRegistration(ctx, a.ID, pw, "", rpName)
	require.NoError(t, err)
	_, err = e.pk.FinishRegistration(ctx, b.ID, id, testutil.NewAuthenticator(pkOrigin, icloudAAGUID).Register(opts))
	requireCode(t, err, http.StatusUnprocessableEntity, "passkey_invalid")
	require.Equal(t, 1, e.challengeCount(t))
}

func TestFinishRegistrationReusedOrExpired(t *testing.T) {
	e := newPKEnv(t, pkOrigin)
	ctx := context.Background()
	u := e.user(t, "a@example.com")
	id, opts, err := e.pk.BeginRegistration(ctx, u.ID, pw, "", rpName)
	require.NoError(t, err)
	resp := testutil.NewAuthenticator(pkOrigin, icloudAAGUID).Register(opts)
	_, err = e.pk.FinishRegistration(ctx, u.ID, id, resp)
	require.NoError(t, err)
	_, err = e.pk.FinishRegistration(ctx, u.ID, id, resp)
	requireCode(t, err, http.StatusUnprocessableEntity, "passkey_invalid")

	id, opts, err = e.pk.BeginRegistration(ctx, u.ID, pw, "", rpName)
	require.NoError(t, err)
	e.expireChallenges(t)
	_, err = e.pk.FinishRegistration(ctx, u.ID, id, testutil.NewAuthenticator(pkOrigin, icloudAAGUID).Register(opts))
	requireCode(t, err, http.StatusUnprocessableEntity, "passkey_invalid")
}

func TestFinishRegistrationBadResponse(t *testing.T) {
	e := newPKEnv(t, pkOrigin)
	ctx := context.Background()
	u := e.user(t, "a@example.com")
	id, _, err := e.pk.BeginRegistration(ctx, u.ID, pw, "", rpName)
	require.NoError(t, err)
	_, err = e.pk.FinishRegistration(ctx, u.ID, id, json.RawMessage(`{"id":"x"}`))
	requireCode(t, err, http.StatusUnprocessableEntity, "passkey_invalid")

	// A response signed for another origin fails verification.
	id, opts, err := e.pk.BeginRegistration(ctx, u.ID, pw, "", rpName)
	require.NoError(t, err)
	_, err = e.pk.FinishRegistration(ctx, u.ID, id, testutil.NewAuthenticator("http://localhost:9999", icloudAAGUID).Register(opts))
	requireCode(t, err, http.StatusUnprocessableEntity, "passkey_invalid")
}

func TestFinishRegistrationDuplicate(t *testing.T) {
	e := newPKEnv(t, pkOrigin)
	ctx := context.Background()
	u := e.user(t, "a@example.com")
	a := testutil.NewAuthenticator(pkOrigin, icloudAAGUID)
	e.register(t, u.ID, a)
	id, opts, err := e.pk.BeginRegistration(ctx, u.ID, pw, "", rpName)
	require.NoError(t, err)
	var o struct {
		Exclude []struct{ ID string } `json:"excludeCredentials"`
	}
	require.NoError(t, json.Unmarshal(opts, &o))
	require.Len(t, o.Exclude, 1)
	_, err = e.pk.FinishRegistration(ctx, u.ID, id, a.Register(opts))
	requireCode(t, err, http.StatusConflict, "passkey_exists")
}

func insertCredentials(t *testing.T, db *ent.Client, userID int64, n int) {
	t.Helper()
	for i := range n {
		err := db.WebauthnCredential.Create().SetUserID(userID).SetCredentialID([]byte{byte(userID), byte(i)}).
			SetName("Passkey").SetData(webauthn.Credential{ID: []byte{byte(userID), byte(i)}}).Exec(context.Background())
		require.NoError(t, err)
	}
}

func TestPasskeyLimit(t *testing.T) {
	e := newPKEnv(t, pkOrigin)
	ctx := context.Background()
	u := e.user(t, "a@example.com")
	insertCredentials(t, e.db, u.ID, auth.MaxPasskeys-1)
	id, opts, err := e.pk.BeginRegistration(ctx, u.ID, pw, "", rpName)
	require.NoError(t, err)
	require.NoError(t, e.db.WebauthnCredential.Create().SetUserID(u.ID).SetCredentialID([]byte("tenth")).
		SetName("Passkey").SetData(webauthn.Credential{}).Exec(ctx))
	// The cap is checked again at finish.
	_, err = e.pk.FinishRegistration(ctx, u.ID, id, testutil.NewAuthenticator(pkOrigin, icloudAAGUID).Register(opts))
	requireCode(t, err, http.StatusConflict, "passkey_limit")
	_, _, err = e.pk.BeginRegistration(ctx, u.ID, pw, "", rpName)
	requireCode(t, err, http.StatusConflict, "passkey_limit")
}

func TestRenamePasskey(t *testing.T) {
	e := newPKEnv(t, pkOrigin)
	ctx := context.Background()
	a := e.user(t, "a@example.com")
	b := e.user(t, "b@example.com")
	p := e.register(t, a.ID, testutil.NewAuthenticator(pkOrigin, icloudAAGUID))
	got, err := e.pk.Rename(ctx, a.ID, p.ID, "  Work laptop  ")
	require.NoError(t, err)
	require.Equal(t, "Work laptop", got.Name)
	require.Equal(t, p.ID, got.ID)
	_, err = e.pk.Rename(ctx, a.ID, p.ID, "   ")
	requireCode(t, err, http.StatusUnprocessableEntity, "invalid_input")
	_, err = e.pk.Rename(ctx, a.ID, p.ID, strings.Repeat("x", 101))
	requireCode(t, err, http.StatusUnprocessableEntity, "invalid_input")
	_, err = e.pk.Rename(ctx, a.ID, p.ID, strings.Repeat("x", 100))
	require.NoError(t, err)
	_, err = e.pk.Rename(ctx, b.ID, p.ID, "mine")
	requireCode(t, err, http.StatusNotFound, "not_found")
}

func TestRemovePasskeyRevokesOthers(t *testing.T) {
	e := newPKEnv(t, pkOrigin)
	ctx := context.Background()
	a := e.user(t, "a@example.com")
	b := e.user(t, "b@example.com")
	p := e.register(t, a.ID, testutil.NewAuthenticator(pkOrigin, icloudAAGUID))
	keep, err := e.svc.NewSession(ctx, a.ID)
	require.NoError(t, err)
	other, err := e.svc.NewSession(ctx, a.ID)
	require.NoError(t, err)
	bTok, err := e.svc.NewSession(ctx, b.ID)
	require.NoError(t, err)

	err = e.pk.Remove(ctx, b.ID, p.ID, bTok)
	requireCode(t, err, http.StatusNotFound, "not_found")
	_, err = e.svc.UserFromSession(ctx, other)
	require.NoError(t, err)

	require.NoError(t, e.pk.Remove(ctx, a.ID, p.ID, keep))
	_, err = e.svc.UserFromSession(ctx, keep)
	require.NoError(t, err)
	_, err = e.svc.UserFromSession(ctx, other)
	requireCode(t, err, http.StatusUnauthorized, "unauthorized")
	_, err = e.svc.UserFromSession(ctx, bTok)
	require.NoError(t, err)
	n, err := e.db.WebauthnCredential.Query().Count(ctx)
	require.NoError(t, err)
	require.Zero(t, n)
	err = e.pk.Remove(ctx, a.ID, p.ID, keep)
	requireCode(t, err, http.StatusNotFound, "not_found")
}

func TestPasskeyLogin(t *testing.T) {
	e := newPKEnv(t, pkOrigin)
	ctx := context.Background()
	u := e.user(t, "a@example.com")
	a := testutil.NewAuthenticator(pkOrigin, icloudAAGUID)
	p := e.register(t, u.ID, a)
	secret, _, err := e.svc.EnrollTOTP(ctx, u.ID, pw)
	require.NoError(t, err)
	code, _ := totp.GenerateCode(secret, time.Now())
	require.NoError(t, e.svc.VerifyTOTP(ctx, u.ID, code, ""))

	id, opts, err := e.pk.BeginLogin(ctx, rpName)
	require.NoError(t, err)
	var o struct {
		RPID             string            `json:"rpId"`
		Allow            []json.RawMessage `json:"allowCredentials"`
		UserVerification string            `json:"userVerification"`
	}
	require.NoError(t, json.Unmarshal(opts, &o))
	require.Equal(t, "localhost", o.RPID)
	require.Empty(t, o.Allow)
	require.Equal(t, "required", o.UserVerification)

	got, tok, err := e.pk.FinishLogin(ctx, id, a.Assert(opts))
	require.NoError(t, err)
	require.Equal(t, u.ID, got.ID)
	require.True(t, got.TOTPEnabled)
	me, err := e.svc.UserFromSession(ctx, tok)
	require.NoError(t, err)
	require.Equal(t, u.ID, me.ID)
	require.Zero(t, e.challengeCount(t))

	row, err := e.db.WebauthnCredential.Get(ctx, p.ID)
	require.NoError(t, err)
	require.NotNil(t, row.LastUsedAt)
	require.Equal(t, uint32(2), row.Data.Authenticator.SignCount)
	list, err := e.pk.List(ctx, u.ID)
	require.NoError(t, err)
	require.NotNil(t, list[0].LastUsedAt)
}

func TestPasskeyLoginReplayFails(t *testing.T) {
	e := newPKEnv(t, pkOrigin)
	ctx := context.Background()
	u := e.user(t, "a@example.com")
	a := testutil.NewAuthenticator(pkOrigin, icloudAAGUID)
	e.register(t, u.ID, a)
	id, opts, err := e.pk.BeginLogin(ctx, rpName)
	require.NoError(t, err)
	resp := a.Assert(opts)
	_, _, err = e.pk.FinishLogin(ctx, id, resp)
	require.NoError(t, err)
	_, _, err = e.pk.FinishLogin(ctx, id, resp)
	requireCode(t, err, http.StatusUnauthorized, "passkey_failed")
}

func swapUserHandle(t *testing.T, resp json.RawMessage, handle []byte) json.RawMessage {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal(resp, &m))
	m["response"].(map[string]any)["userHandle"] = base64.RawURLEncoding.EncodeToString(handle)
	out, err := json.Marshal(m)
	require.NoError(t, err)
	return out
}

func TestPasskeyLoginFailures(t *testing.T) {
	e := newPKEnv(t, pkOrigin)
	ctx := context.Background()
	u := e.user(t, "a@example.com")
	other := e.user(t, "b@example.com")
	a := testutil.NewAuthenticator(pkOrigin, icloudAAGUID)
	e.register(t, u.ID, a)
	e.register(t, other.ID, testutil.NewAuthenticator(pkOrigin, icloudAAGUID))
	failed := func(id string, resp json.RawMessage) {
		t.Helper()
		_, _, err := e.pk.FinishLogin(ctx, id, resp)
		requireCode(t, err, http.StatusUnauthorized, "passkey_failed")
	}

	// An authenticator that began registering but never finished.
	fresh := testutil.NewAuthenticator(pkOrigin, icloudAAGUID)
	_, regOpts, err := e.pk.BeginRegistration(ctx, u.ID, pw, "", rpName)
	require.NoError(t, err)
	fresh.Register(regOpts)
	id, opts, err := e.pk.BeginLogin(ctx, rpName)
	require.NoError(t, err)
	failed(id, fresh.Assert(opts))

	otherRow, err := e.db.User.Query().Where(user.ID(other.ID)).Only(ctx)
	require.NoError(t, err)
	id, opts, err = e.pk.BeginLogin(ctx, rpName)
	require.NoError(t, err)
	failed(id, swapUserHandle(t, a.Assert(opts), *otherRow.WebauthnHandle))

	id, opts, err = e.pk.BeginLogin(ctx, rpName)
	require.NoError(t, err)
	e.expireChallenges(t)
	failed(id, a.Assert(opts))

	regID, regOpts, err := e.pk.BeginRegistration(ctx, u.ID, pw, "", rpName)
	require.NoError(t, err)
	failed(regID, a.Assert(regOpts))
	n, err := e.db.WebauthnChallenge.Query().Where(webauthnchallenge.ID(challengeHash(regID))).Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n)

	failed("not-a-challenge", a.Assert(opts))

	// The valid path still works after all that.
	id, opts, err = e.pk.BeginLogin(ctx, rpName)
	require.NoError(t, err)
	_, _, err = e.pk.FinishLogin(ctx, id, a.Assert(opts))
	require.NoError(t, err)
}

func TestPasskeyLoginCloneWarning(t *testing.T) {
	e := newPKEnv(t, pkOrigin)
	ctx := context.Background()
	u := e.user(t, "a@example.com")
	a := testutil.NewAuthenticator(pkOrigin, icloudAAGUID)
	p := e.register(t, u.ID, a)
	row, err := e.db.WebauthnCredential.Get(ctx, p.ID)
	require.NoError(t, err)
	data := row.Data
	data.Authenticator.SignCount = 1000
	require.NoError(t, e.db.WebauthnCredential.UpdateOneID(p.ID).SetData(data).Exec(ctx))
	id, opts, err := e.pk.BeginLogin(ctx, rpName)
	require.NoError(t, err)
	_, _, err = e.pk.FinishLogin(ctx, id, a.Assert(opts))
	requireCode(t, err, http.StatusUnauthorized, "passkey_failed")
}

func TestDeleteExpiredChallenges(t *testing.T) {
	e := newPKEnv(t, pkOrigin)
	ctx := context.Background()
	for range 3 {
		_, _, err := e.pk.BeginLogin(ctx, rpName)
		require.NoError(t, err)
	}
	e.expireChallenges(t)
	// Inserted directly, since BeginLogin itself sweeps expired rows.
	for i := range 2 {
		err := e.db.WebauthnChallenge.Create().SetID([]byte{byte(i)}).SetPurpose(webauthnchallenge.PurposeLogin).
			SetSessionData(webauthn.SessionData{}).SetExpiresAt(time.Now().Add(time.Minute)).Exec(ctx)
		require.NoError(t, err)
	}
	n, err := e.pk.DeleteExpiredChallenges(ctx, 0)
	require.NoError(t, err)
	require.Equal(t, 3, n)
	require.Equal(t, 2, e.challengeCount(t))

	e.expireChallenges(t)
	n, err = e.pk.DeleteExpiredChallenges(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.Equal(t, 1, e.challengeCount(t))
}

func TestBeginLoginSweepsExpired(t *testing.T) {
	e := newPKEnv(t, pkOrigin)
	ctx := context.Background()
	_, _, err := e.pk.BeginLogin(ctx, rpName)
	require.NoError(t, err)
	e.expireChallenges(t)
	_, _, err = e.pk.BeginLogin(ctx, rpName)
	require.NoError(t, err)
	n, err := e.db.WebauthnChallenge.Query().Where(webauthnchallenge.ExpiresAtLT(time.Now())).Count(ctx)
	require.NoError(t, err)
	require.Zero(t, n)
}

func TestUserDeleteCascadesPasskeys(t *testing.T) {
	e := newPKEnv(t, pkOrigin)
	ctx := context.Background()
	u := e.user(t, "a@example.com")
	e.register(t, u.ID, testutil.NewAuthenticator(pkOrigin, icloudAAGUID))
	_, _, err := e.pk.BeginRegistration(ctx, u.ID, pw, "", rpName)
	require.NoError(t, err)
	require.NoError(t, e.db.User.DeleteOneID(u.ID).Exec(ctx))
	n, err := e.db.WebauthnCredential.Query().Where(webauthncredential.UserID(u.ID)).Count(ctx)
	require.NoError(t, err)
	require.Zero(t, n)
	require.Zero(t, e.challengeCount(t))
}

func TestChallengeCleanupJob(t *testing.T) {
	ctx := context.Background()
	pool := testutil.NewDB(t)
	client, sqlDB := db.NewEnt(pool)
	t.Cleanup(func() { client.Close() })
	pk, err := auth.NewPasskeys(client, auth.New(client, ext.Defaults(nil).Registration), "")
	require.NoError(t, err)
	for i := range 2 {
		err := client.WebauthnChallenge.Create().SetID([]byte{byte(i)}).SetPurpose(webauthnchallenge.PurposeLogin).
			SetSessionData(webauthn.SessionData{}).SetExpiresAt(time.Now().Add(time.Duration(i*2-1) * time.Minute)).Exec(ctx)
		require.NoError(t, err)
	}
	workers := river.NewWorkers()
	auth.RegisterPasskeyJobs(workers, pk)
	j := testutil.StartJobs(t, pool, sqlDB, workers)
	tx, err := sqlDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	res, err := j.InsertTx(ctx, tx, auth.ChallengeCleanupArgs{}, nil)
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
	require.Equal(t, "maintenance", res.Job.Queue)
	row := testutil.WaitJob(t, j, res.Job.ID)
	require.Equal(t, rivertype.JobStateCompleted, row.State)
	left, err := client.WebauthnChallenge.Query().All(ctx)
	require.NoError(t, err)
	require.Len(t, left, 1)
	require.Equal(t, []byte{1}, left[0].ID)
}

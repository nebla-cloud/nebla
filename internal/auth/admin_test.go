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

package auth_test

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/nebla-cloud/nebla/internal/auth"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/user"
	"github.com/nebla-cloud/nebla/internal/ent/webauthnchallenge"
	"github.com/nebla-cloud/nebla/internal/ent/webauthncredential"
	"github.com/nebla-cloud/nebla/internal/ext"
	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/require"
)

type adminEnv struct {
	db  *ent.Client
	svc *auth.Service
}

func newAdminEnv(t *testing.T) adminEnv {
	t.Helper()
	db := testutil.NewEnt(t)
	return adminEnv{db: db, svc: auth.New(db, ext.Defaults(nil).Registration)}
}

func (e adminEnv) user(t *testing.T, email string, admin bool) auth.User {
	t.Helper()
	u, err := e.svc.CreateUser(context.Background(), email, pw, "U", admin)
	require.NoError(t, err)
	return u
}

func (e adminEnv) session(t *testing.T, userID int64) string {
	t.Helper()
	tok, err := e.svc.NewSession(context.Background(), userID)
	require.NoError(t, err)
	return tok
}

func (e adminEnv) live(t *testing.T, tok string) bool {
	t.Helper()
	_, err := e.svc.UserFromSession(context.Background(), tok)
	return err == nil
}

// addPasskeyState gives the user one credential and one pending challenge.
func (e adminEnv) addPasskeyState(t *testing.T, userID int64) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, e.db.WebauthnCredential.Create().SetUserID(userID).SetCredentialID([]byte{byte(userID), 0xAA}).
		SetName("Passkey").SetData(webauthn.Credential{ID: []byte{byte(userID), 0xAA}}).Exec(ctx))
	require.NoError(t, e.db.WebauthnChallenge.Create().SetID([]byte{byte(userID), 0xBB}).
		SetPurpose(webauthnchallenge.PurposeRegister).SetUserID(userID).SetSessionData(webauthn.SessionData{}).
		SetExpiresAt(time.Now().Add(time.Minute)).Exec(ctx))
}

func (e adminEnv) passkeyState(t *testing.T, userID int64) (creds, challenges int) {
	t.Helper()
	ctx := context.Background()
	creds, err := e.db.WebauthnCredential.Query().Where(webauthncredential.UserID(userID)).Count(ctx)
	require.NoError(t, err)
	challenges, err = e.db.WebauthnChallenge.Query().Where(webauthnchallenge.UserID(userID)).Count(ctx)
	require.NoError(t, err)
	return creds, challenges
}

func TestAdminUpdateUser(t *testing.T) {
	e := newAdminEnv(t)
	ctx := context.Background()
	admin := e.user(t, "admin@example.com", true)
	u := e.user(t, "u@example.com", false)
	e.user(t, "taken@example.com", false)

	got, err := e.svc.AdminUpdateUser(ctx, admin.ID, u.ID, "New Name", "  New@Example.com ", false, "")
	require.NoError(t, err)
	require.Equal(t, "New Name", got.Name)
	require.Equal(t, "new@example.com", got.Email)
	require.False(t, got.IsAdmin)

	_, err = e.svc.AdminUpdateUser(ctx, admin.ID, u.ID, "N", "TAKEN@example.com", false, "")
	requireCode(t, err, http.StatusConflict, "email_taken")
	require.EqualError(t, err, "email_taken: That email is already registered.")
	_, err = e.svc.AdminUpdateUser(ctx, admin.ID, u.ID, "N", "not-an-email", false, "")
	requireCode(t, err, http.StatusUnprocessableEntity, "invalid_input")
	_, err = e.svc.AdminUpdateUser(ctx, admin.ID, u.ID, "  ", "n@example.com", false, "")
	requireCode(t, err, http.StatusUnprocessableEntity, "invalid_input")
	_, err = e.svc.AdminUpdateUser(ctx, admin.ID, 99999, "N", "n@example.com", false, "")
	requireCode(t, err, http.StatusNotFound, "not_found")

	_, err = e.svc.AdminUpdateUser(ctx, admin.ID, admin.ID, "A", "admin@example.com", false, "")
	requireCode(t, err, http.StatusUnprocessableEntity, "self_admin_action")
	self, err := e.svc.AdminUpdateUser(ctx, admin.ID, admin.ID, "Me", "admin@example.com", true, "")
	require.NoError(t, err, "own name and email edits are allowed")
	require.Equal(t, "Me", self.Name)

	got, err = e.svc.AdminUpdateUser(ctx, admin.ID, u.ID, "N", "new@example.com", true, "")
	require.NoError(t, err)
	require.True(t, got.IsAdmin)
	other := e.user(t, "other@example.com", false)
	_, err = e.svc.AdminUpdateUser(ctx, u.ID, admin.ID, "Me", "admin@example.com", false, "")
	require.NoError(t, err, "two admins, demoting one is fine")
	_, err = e.svc.AdminUpdateUser(ctx, other.ID, u.ID, "N", "new@example.com", false, "")
	requireCode(t, err, http.StatusConflict, "last_admin")
}

func TestAdminUpdateUserEmailRevokes(t *testing.T) {
	e := newAdminEnv(t)
	ctx := context.Background()
	admin := e.user(t, "admin@example.com", true)
	u := e.user(t, "u@example.com", false)
	uTok := e.session(t, u.ID)

	_, err := e.svc.AdminUpdateUser(ctx, admin.ID, u.ID, "Renamed", "u@example.com", false, "")
	require.NoError(t, err)
	require.True(t, e.live(t, uTok), "a name change keeps sessions")
	_, err = e.svc.AdminUpdateUser(ctx, admin.ID, u.ID, "Renamed", "u@example.com", true, "")
	require.NoError(t, err)
	require.True(t, e.live(t, uTok), "an admin change keeps sessions")

	_, err = e.svc.AdminUpdateUser(ctx, admin.ID, u.ID, "Renamed", "U2@example.com", true, "")
	require.NoError(t, err)
	require.False(t, e.live(t, uTok))

	keep := e.session(t, admin.ID)
	other := e.session(t, admin.ID)
	_, err = e.svc.AdminUpdateUser(ctx, admin.ID, admin.ID, "A", "admin2@example.com", true, keep)
	require.NoError(t, err)
	require.True(t, e.live(t, keep))
	require.False(t, e.live(t, other))
}

func TestAdminDemoteRace(t *testing.T) {
	e := newAdminEnv(t)
	ctx := context.Background()
	a := e.user(t, "a@example.com", true)
	b := e.user(t, "b@example.com", true)
	for range 5 {
		var wg sync.WaitGroup
		errs := make([]error, 2)
		wg.Go(func() { _, errs[0] = e.svc.AdminUpdateUser(ctx, a.ID, b.ID, "U", "b@example.com", false, "") })
		wg.Go(func() { _, errs[1] = e.svc.AdminUpdateUser(ctx, b.ID, a.ID, "U", "a@example.com", false, "") })
		wg.Wait()
		failed := 0
		for _, err := range errs {
			if err != nil {
				requireCode(t, err, http.StatusConflict, "last_admin")
				failed++
			}
		}
		require.Equal(t, 1, failed)
		n, err := e.db.User.Query().Where(user.IsAdmin(true)).Count(ctx)
		require.NoError(t, err)
		require.Equal(t, 1, n)
		require.NoError(t, e.db.User.Update().SetIsAdmin(true).Exec(ctx))
	}
}

func TestAdminSetPassword(t *testing.T) {
	e := newAdminEnv(t)
	ctx := context.Background()
	admin := e.user(t, "admin@example.com", true)
	u := e.user(t, "u@example.com", false)
	keepers := e.user(t, "k@example.com", false)
	e.addPasskeyState(t, u.ID)
	e.addPasskeyState(t, keepers.ID)
	tok := e.session(t, u.ID)

	requireCode(t, e.svc.AdminSetPassword(ctx, admin.ID, u.ID, "short", true), http.StatusUnprocessableEntity, "invalid_input")
	requireCode(t, e.svc.AdminSetPassword(ctx, admin.ID, admin.ID, "another-long-password", true), http.StatusUnprocessableEntity, "self_admin_action")
	requireCode(t, e.svc.AdminSetPassword(ctx, admin.ID, 99999, "another-long-password", true), http.StatusNotFound, "not_found")
	require.True(t, e.live(t, tok))

	require.NoError(t, e.svc.AdminSetPassword(ctx, admin.ID, u.ID, "another-long-password", true))
	require.False(t, e.live(t, tok))
	_, err := e.svc.Login(ctx, "u@example.com", "another-long-password", "")
	require.NoError(t, err)
	creds, chs := e.passkeyState(t, u.ID)
	require.Zero(t, creds)
	require.Zero(t, chs)

	require.NoError(t, e.svc.AdminSetPassword(ctx, admin.ID, keepers.ID, "another-long-password", false))
	creds, chs = e.passkeyState(t, keepers.ID)
	require.Equal(t, 1, creds)
	require.Equal(t, 1, chs)
}

func TestAdminResetTOTP(t *testing.T) {
	e := newAdminEnv(t)
	ctx := context.Background()
	admin := e.user(t, "admin@example.com", true)
	u := e.user(t, "u@example.com", false)
	secret, _, err := e.svc.EnrollTOTP(ctx, u.ID, pw)
	require.NoError(t, err)
	code, err := totp.GenerateCode(secret, time.Now())
	require.NoError(t, err)
	require.NoError(t, e.svc.VerifyTOTP(ctx, u.ID, code, ""))
	_, err = e.svc.Login(ctx, "u@example.com", pw, "")
	requireCode(t, err, http.StatusUnauthorized, "totp_required")
	tok := e.session(t, u.ID)

	requireCode(t, e.svc.AdminResetTOTP(ctx, admin.ID, admin.ID), http.StatusUnprocessableEntity, "self_admin_action")
	requireCode(t, e.svc.AdminResetTOTP(ctx, admin.ID, 99999), http.StatusNotFound, "not_found")
	require.NoError(t, e.svc.AdminResetTOTP(ctx, admin.ID, u.ID))
	require.False(t, e.live(t, tok))
	row, err := e.db.User.Get(ctx, u.ID)
	require.NoError(t, err)
	require.False(t, row.TotpEnabled)
	require.Nil(t, row.TotpSecret)
	require.Nil(t, row.TotpPendingSecret)
	_, err = e.svc.Login(ctx, "u@example.com", pw, "")
	require.NoError(t, err)

	tok = e.session(t, u.ID)
	require.NoError(t, e.svc.AdminResetTOTP(ctx, admin.ID, u.ID), "already off")
	require.False(t, e.live(t, tok))
}

func TestAdminRemovePasskeys(t *testing.T) {
	e := newAdminEnv(t)
	ctx := context.Background()
	admin := e.user(t, "admin@example.com", true)
	u := e.user(t, "u@example.com", false)
	other := e.user(t, "o@example.com", false)
	e.addPasskeyState(t, u.ID)
	e.addPasskeyState(t, other.ID)
	tok := e.session(t, u.ID)
	otherTok := e.session(t, other.ID)

	requireCode(t, e.svc.AdminRemovePasskeys(ctx, admin.ID, admin.ID), http.StatusUnprocessableEntity, "self_admin_action")
	requireCode(t, e.svc.AdminRemovePasskeys(ctx, admin.ID, 99999), http.StatusNotFound, "not_found")
	require.NoError(t, e.svc.AdminRemovePasskeys(ctx, admin.ID, u.ID))
	creds, chs := e.passkeyState(t, u.ID)
	require.Zero(t, creds)
	require.Zero(t, chs)
	require.False(t, e.live(t, tok))
	creds, chs = e.passkeyState(t, other.ID)
	require.Equal(t, 1, creds)
	require.Equal(t, 1, chs)
	require.True(t, e.live(t, otherTok))
	require.NoError(t, e.svc.AdminRemovePasskeys(ctx, admin.ID, u.ID), "idempotent")
}

func TestListAdminUsersPasskeyCount(t *testing.T) {
	e := newAdminEnv(t)
	ctx := context.Background()
	a := e.user(t, "a@example.com", true)
	b := e.user(t, "b@example.com", false)
	c := e.user(t, "c@example.com", false)
	insertCredentials(t, e.db, b.ID, 2)
	insertCredentials(t, e.db, c.ID, 1)

	us, total, err := e.svc.ListAdminUsers(ctx, 1, 2)
	require.NoError(t, err)
	require.Equal(t, 3, total)
	require.Len(t, us, 2)
	require.Equal(t, a.ID, us[0].ID)
	require.Equal(t, 0, us[0].PasskeyCount)
	require.Equal(t, "b@example.com", us[1].Email)
	require.Equal(t, 2, us[1].PasskeyCount)
	us, _, err = e.svc.ListAdminUsers(ctx, 2, 2)
	require.NoError(t, err)
	require.Len(t, us, 1)
	require.Equal(t, 1, us[0].PasskeyCount)
}

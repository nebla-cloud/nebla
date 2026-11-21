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

package projects_test

import (
	"context"
	"testing"

	"github.com/nebla-cloud/nebla/internal/auth"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ext"
	"github.com/nebla-cloud/nebla/internal/projects"
	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/stretchr/testify/require"
)

type tokenEnv struct {
	svc   *projects.Service
	au    *auth.Service
	db    *ent.Client
	owner auth.User
	pid   int64
}

func newTokenEnv(t *testing.T) tokenEnv {
	t.Helper()
	svc, client, _, owner := setup(t, projects.Options{})
	p, err := svc.Create(context.Background(), owner.ID, false, "Web")
	require.NoError(t, err)
	return tokenEnv{svc: svc, au: auth.New(client, ext.Defaults(nil).Registration), db: client, owner: owner, pid: p.ID}
}

// member joins the project with role and mints a read_write token.
func (e tokenEnv) member(t *testing.T, email, role string) (auth.User, string) {
	t.Helper()
	ctx := context.Background()
	u, err := e.au.CreateUser(ctx, email, testutil.Password, "M", false)
	require.NoError(t, err)
	link, err := e.svc.Invite(ctx, e.owner.ID, e.pid, email, role)
	require.NoError(t, err)
	_, err = e.svc.AcceptInvite(ctx, inviteToken(t, link), &u, "", "", false)
	require.NoError(t, err)
	tk, plain, err := e.svc.CreateToken(ctx, u.ID, e.pid, "ci", auth.PermReadWrite)
	require.NoError(t, err)
	require.Equal(t, &u.ID, tk.CreatedBy)
	return u, plain
}

func (e tokenEnv) live(t *testing.T, plain string) bool {
	t.Helper()
	_, err := e.au.AuthenticateToken(context.Background(), plain)
	return err == nil
}

func TestTokensRevokedWhenCreatorRemoved(t *testing.T) {
	e := newTokenEnv(t)
	m, tok := e.member(t, "m@example.com", projects.RoleAdmin)
	_, ownerTok, err := e.svc.CreateToken(context.Background(), e.owner.ID, e.pid, "owner", auth.PermRead)
	require.NoError(t, err)
	require.NoError(t, e.svc.RemoveMember(context.Background(), e.owner.ID, e.pid, m.ID))
	require.False(t, e.live(t, tok))
	require.True(t, e.live(t, ownerTok))
}

func TestTokensRevokedWhenCreatorLeaves(t *testing.T) {
	e := newTokenEnv(t)
	m, tok := e.member(t, "m@example.com", projects.RoleAdmin)
	require.NoError(t, e.svc.RemoveMember(context.Background(), m.ID, e.pid, m.ID))
	require.False(t, e.live(t, tok))
}

func TestTokensRevokedWhenCreatorDemoted(t *testing.T) {
	for _, role := range []string{projects.RoleMember, projects.RoleReadOnly} {
		t.Run(role, func(t *testing.T) {
			e := newTokenEnv(t)
			m, tok := e.member(t, "m@example.com", projects.RoleAdmin)
			_, err := e.svc.SetRole(context.Background(), e.owner.ID, e.pid, m.ID, role)
			require.NoError(t, err)
			require.False(t, e.live(t, tok))
		})
	}
}

func TestTokensKeptWhenCreatorStaysManager(t *testing.T) {
	e := newTokenEnv(t)
	m, tok := e.member(t, "m@example.com", projects.RoleAdmin)
	_, err := e.svc.SetRole(context.Background(), e.owner.ID, e.pid, m.ID, projects.RoleOwner)
	require.NoError(t, err)
	require.True(t, e.live(t, tok))
}

func TestTokensRevokedWhenCreatorDeleted(t *testing.T) {
	e := newTokenEnv(t)
	ctx := context.Background()
	m, tok := e.member(t, "m@example.com", projects.RoleAdmin)
	admin, err := e.au.CreateUser(ctx, "root@example.com", testutil.Password, "Root", true)
	require.NoError(t, err)
	require.NoError(t, e.svc.DeleteUser(ctx, admin.ID, m.ID, false))
	require.False(t, e.live(t, tok))
}

func TestTokensOnlyRevokedInThatProject(t *testing.T) {
	e := newTokenEnv(t)
	ctx := context.Background()
	m, tok := e.member(t, "m@example.com", projects.RoleAdmin)
	own, err := e.svc.Create(ctx, m.ID, false, "Mine")
	require.NoError(t, err)
	_, mine, err := e.svc.CreateToken(ctx, m.ID, own.ID, "mine", auth.PermReadWrite)
	require.NoError(t, err)
	require.NoError(t, e.svc.RemoveMember(ctx, e.owner.ID, e.pid, m.ID))
	require.False(t, e.live(t, tok))
	require.True(t, e.live(t, mine))
}

func TestLegacyTokenWithoutCreatorKept(t *testing.T) {
	e := newTokenEnv(t)
	ctx := context.Background()
	m, _ := e.member(t, "m@example.com", projects.RoleAdmin)
	plain, hash, err := auth.NewAPIToken()
	require.NoError(t, err)
	require.NoError(t, e.db.APIToken.Create().SetProjectID(e.pid).SetName("legacy").
		SetPermission(auth.PermReadWrite).SetTokenHash(hash).Exec(ctx))
	d, _ := e.member(t, "d@example.com", projects.RoleAdmin)
	_, err = e.svc.SetRole(ctx, e.owner.ID, e.pid, d.ID, projects.RoleReadOnly)
	require.NoError(t, err)
	require.True(t, e.live(t, plain))
	require.NoError(t, e.svc.RemoveMember(ctx, e.owner.ID, e.pid, m.ID))
	require.True(t, e.live(t, plain))
}

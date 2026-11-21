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

	"github.com/nebla-cloud/nebla/internal/apierr"
	"github.com/nebla-cloud/nebla/internal/auth"
	"github.com/nebla-cloud/nebla/internal/ext"
	"github.com/nebla-cloud/nebla/internal/projects"
	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/stretchr/testify/require"
)

// twoOwners returns a project where a and b are both owners.
func twoOwners(t *testing.T) (*projects.Service, *auth.Service, auth.User, auth.User, projects.Project) {
	t.Helper()
	ctx := context.Background()
	svc, client, _, a := setup(t, projects.Options{})
	au := auth.New(client, ext.Defaults(nil).Registration)
	b, err := au.CreateUser(ctx, "b@example.test", testutil.Password, "B", false)
	require.NoError(t, err)
	p, err := svc.Create(ctx, a.ID, false, "P")
	require.NoError(t, err)
	l, err := svc.Invite(ctx, a.ID, p.ID, "b@example.test", projects.RoleOwner)
	require.NoError(t, err)
	_, err = svc.AcceptInvite(ctx, inviteToken(t, l), &b, "", "", true)
	require.NoError(t, err)
	return svc, au, a, b, p
}

func requireNotFound(t *testing.T, err error) {
	t.Helper()
	var e apierr.E
	require.ErrorAs(t, err, &e)
	require.Equal(t, "not_found", e.Code)
}

func TestOwnerInviteRevokedWhenCreatorDemoted(t *testing.T) {
	ctx := context.Background()
	svc, au, a, b, p := twoOwners(t)
	c, err := au.CreateUser(ctx, "c@example.test", testutil.Password, "C", false)
	require.NoError(t, err)
	lc, err := svc.Invite(ctx, a.ID, p.ID, "c@example.test", projects.RoleOwner)
	require.NoError(t, err)
	_, err = svc.SetRole(ctx, b.ID, p.ID, a.ID, projects.RoleAdmin)
	require.NoError(t, err)

	_, err = svc.AcceptInvite(ctx, inviteToken(t, lc), &c, "", "", true)
	requireNotFound(t, err)
	_, err = svc.Role(ctx, c.ID, p.ID)
	requireNotFound(t, err)
}

func TestAdminInviteSurvivesOwnerDemotedToAdmin(t *testing.T) {
	ctx := context.Background()
	svc, _, a, b, p := twoOwners(t)
	lc, err := svc.Invite(ctx, a.ID, p.ID, "c@example.test", projects.RoleAdmin)
	require.NoError(t, err)
	_, err = svc.SetRole(ctx, b.ID, p.ID, a.ID, projects.RoleAdmin)
	require.NoError(t, err)

	acc, err := svc.AcceptInvite(ctx, inviteToken(t, lc), nil, "C", testutil.Password, true)
	require.NoError(t, err)
	r, err := svc.Role(ctx, acc.NewUser.ID, p.ID)
	require.NoError(t, err)
	require.Equal(t, projects.RoleAdmin, r)
}

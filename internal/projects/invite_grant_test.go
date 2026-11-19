package projects_test

import (
	"context"
	"testing"

	"github.com/Hexeption/nebla/internal/apierr"
	"github.com/Hexeption/nebla/internal/auth"
	"github.com/Hexeption/nebla/internal/ext"
	"github.com/Hexeption/nebla/internal/projects"
	"github.com/Hexeption/nebla/internal/testutil"
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

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

func requireCapReached(t *testing.T, err error) {
	t.Helper()
	var e apierr.E
	require.ErrorAs(t, err, &e)
	require.Equal(t, "quota_exceeded", e.Code)
}

func TestCapIgnoresInvitedOwnership(t *testing.T) {
	ctx := context.Background()
	svc, client, _, a := setup(t, projects.Options{MaxOwnedProjects: 1})
	b, err := auth.New(client, ext.Defaults(nil).Registration).CreateUser(ctx, "b@example.test", testutil.Password, "B", false)
	require.NoError(t, err)
	p, err := svc.Create(ctx, a.ID, false, "P")
	require.NoError(t, err)
	l, err := svc.Invite(ctx, a.ID, p.ID, "b@example.test", projects.RoleOwner)
	require.NoError(t, err)
	_, err = svc.AcceptInvite(ctx, inviteToken(t, l), &b, "", "", true)
	require.NoError(t, err)

	_, err = svc.Create(ctx, b.ID, false, "B's own")
	require.NoError(t, err)
}

func TestCapCountsCreatedAfterTransfer(t *testing.T) {
	ctx := context.Background()
	svc, client, _, a := setup(t, projects.Options{MaxOwnedProjects: 1})
	b, err := auth.New(client, ext.Defaults(nil).Registration).CreateUser(ctx, "b@example.test", testutil.Password, "B", false)
	require.NoError(t, err)
	p, err := svc.Create(ctx, a.ID, false, "P")
	require.NoError(t, err)
	row, err := client.Project.Get(ctx, p.ID)
	require.NoError(t, err)
	require.Equal(t, a.ID, *row.CreatedBy)
	l, err := svc.Invite(ctx, a.ID, p.ID, "b@example.test", projects.RoleOwner)
	require.NoError(t, err)
	_, err = svc.AcceptInvite(ctx, inviteToken(t, l), &b, "", "", true)
	require.NoError(t, err)
	_, err = svc.SetRole(ctx, b.ID, p.ID, a.ID, projects.RoleMember)
	require.NoError(t, err)

	_, err = svc.Create(ctx, a.ID, false, "again")
	requireCapReached(t, err)
}

func TestCapCountsLegacyOwnedProjects(t *testing.T) {
	ctx := context.Background()
	svc, client, _, a := setup(t, projects.Options{MaxOwnedProjects: 1})
	p, err := client.Project.Create().SetName("legacy").Save(ctx)
	require.NoError(t, err)
	require.NoError(t, client.ProjectMember.Create().SetProjectID(p.ID).SetUserID(a.ID).SetRole(projects.RoleOwner).Exec(ctx))
	_, err = projects.BackfillCreatedBy(ctx, client)
	require.NoError(t, err)

	_, err = svc.Create(ctx, a.ID, false, "new")
	requireCapReached(t, err)
}

func TestDeletingCreatorKeepsProject(t *testing.T) {
	ctx := context.Background()
	svc, client, _, a := setup(t, projects.Options{})
	p, err := svc.Create(ctx, a.ID, false, "P")
	require.NoError(t, err)
	require.NoError(t, client.User.DeleteOneID(a.ID).Exec(ctx))

	row, err := client.Project.Get(ctx, p.ID)
	require.NoError(t, err)
	require.Nil(t, row.CreatedBy)
}

func TestCapIgnoresProjectsOfDeletedCreator(t *testing.T) {
	ctx := context.Background()
	svc, client, _, a := setup(t, projects.Options{MaxOwnedProjects: 1})
	b, err := auth.New(client, ext.Defaults(nil).Registration).CreateUser(ctx, "b@example.test", testutil.Password, "B", false)
	require.NoError(t, err)
	p, err := svc.Create(ctx, a.ID, false, "P")
	require.NoError(t, err)
	l, err := svc.Invite(ctx, a.ID, p.ID, "b@example.test", projects.RoleOwner)
	require.NoError(t, err)
	_, err = svc.AcceptInvite(ctx, inviteToken(t, l), &b, "", "", true)
	require.NoError(t, err)
	require.NoError(t, client.User.DeleteOneID(a.ID).Exec(ctx))

	_, err = svc.Create(ctx, b.ID, false, "B's own")
	require.NoError(t, err)
}

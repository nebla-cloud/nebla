package projects_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/Hexeption/nebla/internal/apierr"
	"github.com/Hexeption/nebla/internal/auth"
	"github.com/Hexeption/nebla/internal/db"
	"github.com/Hexeption/nebla/internal/ent"
	"github.com/Hexeption/nebla/internal/ext"
	"github.com/Hexeption/nebla/internal/projects"
	"github.com/Hexeption/nebla/internal/testutil"
	"github.com/stretchr/testify/require"
)

func setup(t *testing.T, opts projects.Options) (*projects.Service, *ent.Client, *sql.DB, auth.User) {
	t.Helper()
	pool := testutil.NewDB(t)
	client, sqlDB := db.NewEnt(pool)
	t.Cleanup(func() { client.Close() })
	au := auth.New(client, ext.Defaults(nil).Registration)
	u, err := au.CreateUser(context.Background(), "owner@example.com", testutil.Password, "Owner", false)
	require.NoError(t, err)
	opts.Auth = au
	return projects.New(client, opts), client, sqlDB, u
}

func TestCreatorIsOwner(t *testing.T) {
	ctx := context.Background()
	svc, _, _, u := setup(t, projects.Options{})
	p, err := svc.Create(ctx, u.ID, "Web")
	require.NoError(t, err)
	require.Equal(t, projects.RoleOwner, p.Role)

	got, err := svc.Get(ctx, u.ID, p.ID)
	require.NoError(t, err)
	require.Equal(t, "Web", got.Name)
	require.Equal(t, projects.RoleOwner, got.Role)

	ms, err := svc.Members(ctx, u.ID, p.ID)
	require.NoError(t, err)
	require.Len(t, ms, 1)
	require.Equal(t, u.ID, ms[0].UserID)
	require.Equal(t, projects.RoleOwner, ms[0].Role)
}

func TestQuotaExceeded(t *testing.T) {
	ctx := context.Background()
	existing := projects.Usage{Servers: 1, VCPU: 2}
	svc, _, sqlDB, u := setup(t, projects.Options{Hooks: projects.Hooks{
		Usage: func(ctx context.Context, tx *ent.Client, projectID int64) (projects.Usage, error) {
			return existing, nil
		},
	}})
	p, err := svc.Create(ctx, u.ID, "Web")
	require.NoError(t, err)
	_, err = svc.SetQuota(ctx, p.ID, projects.Quota{MaxServers: 1})
	require.NoError(t, err)

	reserve := func(d projects.Usage) error {
		return db.WithTx(ctx, sqlDB, func(_ *sql.Tx, etx *ent.Client) error {
			return svc.ReserveQuota(ctx, etx, p.ID, d)
		})
	}
	err = reserve(projects.Usage{Servers: 1, VCPU: 2})
	var e apierr.E
	require.True(t, errors.As(err, &e), "got %v", err)
	require.Equal(t, 422, e.Status)
	require.Equal(t, "quota_exceeded", e.Code)
	require.Contains(t, e.Msg, "max_servers")

	existing = projects.Usage{}
	require.NoError(t, reserve(projects.Usage{Servers: 1, VCPU: 64}), "0 limits are unlimited")
}

func inviteToken(t *testing.T, link string) string {
	t.Helper()
	i := strings.LastIndex(link, "/")
	require.Positive(t, i)
	return link[i+1:]
}

func TestInviteEmailMismatch(t *testing.T) {
	ctx := context.Background()
	svc, client, _, owner := setup(t, projects.Options{})
	au := auth.New(client, ext.Defaults(nil).Registration)
	other, err := au.CreateUser(ctx, "other@example.com", testutil.Password, "Other", false)
	require.NoError(t, err)
	p, err := svc.Create(ctx, owner.ID, "Web")
	require.NoError(t, err)
	link, err := svc.Invite(ctx, owner.ID, p.ID, "invited@example.com", projects.RoleAdmin)
	require.NoError(t, err)

	_, err = svc.AcceptInvite(ctx, inviteToken(t, link), &other, "", "")
	var e apierr.E
	require.True(t, errors.As(err, &e), "got %v", err)
	require.Equal(t, 403, e.Status)
	require.Equal(t, "invite_email_mismatch", e.Code)
}

func TestRevokedInviteNotFound(t *testing.T) {
	ctx := context.Background()
	svc, _, _, owner := setup(t, projects.Options{})
	p, err := svc.Create(ctx, owner.ID, "Web")
	require.NoError(t, err)
	link, err := svc.Invite(ctx, owner.ID, p.ID, "new@example.com", projects.RoleMember)
	require.NoError(t, err)

	invs, err := svc.Invites(ctx, owner.ID, p.ID)
	require.NoError(t, err)
	require.Len(t, invs, 1)
	require.Equal(t, "new@example.com", invs[0].Email)
	require.Equal(t, owner.ID, invs[0].CreatedBy)
	require.NoError(t, svc.RevokeInvite(ctx, owner.ID, p.ID, invs[0].ID))

	_, err = svc.AcceptInvite(ctx, inviteToken(t, link), nil, "New", testutil.Password)
	var e apierr.E
	require.True(t, errors.As(err, &e), "got %v", err)
	require.Equal(t, 404, e.Status)
}

func TestRemovingCreatorKillsInvites(t *testing.T) {
	ctx := context.Background()
	svc, client, _, owner := setup(t, projects.Options{})
	au := auth.New(client, ext.Defaults(nil).Registration)
	adm, err := au.CreateUser(ctx, "adm@example.com", testutil.Password, "Adm", false)
	require.NoError(t, err)
	p, err := svc.Create(ctx, owner.ID, "Web")
	require.NoError(t, err)
	link, err := svc.Invite(ctx, owner.ID, p.ID, "adm@example.com", projects.RoleAdmin)
	require.NoError(t, err)
	_, err = svc.AcceptInvite(ctx, inviteToken(t, link), &adm, "", "")
	require.NoError(t, err)

	kept, err := svc.Invite(ctx, owner.ID, p.ID, "keep@example.com", projects.RoleMember)
	require.NoError(t, err)
	backdoor, err := svc.Invite(ctx, adm.ID, p.ID, "adm@example.com", projects.RoleAdmin)
	require.NoError(t, err)
	require.NoError(t, svc.RemoveMember(ctx, owner.ID, p.ID, adm.ID))

	_, err = svc.AcceptInvite(ctx, inviteToken(t, backdoor), &adm, "", "")
	var e apierr.E
	require.True(t, errors.As(err, &e), "got %v", err)
	require.Equal(t, 404, e.Status)
	invs, err := svc.Invites(ctx, owner.ID, p.ID)
	require.NoError(t, err)
	require.Len(t, invs, 1, "only the owner's invite survives")
	require.Equal(t, "keep@example.com", invs[0].Email)
	_ = kept
}

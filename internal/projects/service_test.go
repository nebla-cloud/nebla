package projects_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/nebla-cloud/nebla/internal/apierr"
	"github.com/nebla-cloud/nebla/internal/auth"
	"github.com/nebla-cloud/nebla/internal/db"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ext"
	"github.com/nebla-cloud/nebla/internal/projects"
	"github.com/nebla-cloud/nebla/internal/testutil"
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
	p, err := svc.Create(ctx, u.ID, false, "Web")
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
	p, err := svc.Create(ctx, u.ID, false, "Web")
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
	p, err := svc.Create(ctx, owner.ID, false, "Web")
	require.NoError(t, err)
	link, err := svc.Invite(ctx, owner.ID, p.ID, "invited@example.com", projects.RoleAdmin)
	require.NoError(t, err)

	_, err = svc.AcceptInvite(ctx, inviteToken(t, link), &other, "", "", true)
	var e apierr.E
	require.True(t, errors.As(err, &e), "got %v", err)
	require.Equal(t, 403, e.Status)
	require.Equal(t, "invite_email_mismatch", e.Code)
}

func TestRevokedInviteNotFound(t *testing.T) {
	ctx := context.Background()
	svc, _, _, owner := setup(t, projects.Options{})
	p, err := svc.Create(ctx, owner.ID, false, "Web")
	require.NoError(t, err)
	link, err := svc.Invite(ctx, owner.ID, p.ID, "new@example.com", projects.RoleMember)
	require.NoError(t, err)

	invs, err := svc.Invites(ctx, owner.ID, p.ID)
	require.NoError(t, err)
	require.Len(t, invs, 1)
	require.Equal(t, "new@example.com", invs[0].Email)
	require.Equal(t, owner.ID, invs[0].CreatedBy)
	require.NoError(t, svc.RevokeInvite(ctx, owner.ID, p.ID, invs[0].ID))

	_, err = svc.AcceptInvite(ctx, inviteToken(t, link), nil, "New", testutil.Password, true)
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
	p, err := svc.Create(ctx, owner.ID, false, "Web")
	require.NoError(t, err)
	link, err := svc.Invite(ctx, owner.ID, p.ID, "adm@example.com", projects.RoleAdmin)
	require.NoError(t, err)
	_, err = svc.AcceptInvite(ctx, inviteToken(t, link), &adm, "", "", true)
	require.NoError(t, err)

	kept, err := svc.Invite(ctx, owner.ID, p.ID, "keep@example.com", projects.RoleMember)
	require.NoError(t, err)
	backdoor, err := svc.Invite(ctx, adm.ID, p.ID, "adm@example.com", projects.RoleAdmin)
	require.NoError(t, err)
	require.NoError(t, svc.RemoveMember(ctx, owner.ID, p.ID, adm.ID))

	_, err = svc.AcceptInvite(ctx, inviteToken(t, backdoor), &adm, "", "", true)
	var e apierr.E
	require.True(t, errors.As(err, &e), "got %v", err)
	require.Equal(t, 404, e.Status)
	invs, err := svc.Invites(ctx, owner.ID, p.ID)
	require.NoError(t, err)
	require.Len(t, invs, 1, "only the owner's invite survives")
	require.Equal(t, "keep@example.com", invs[0].Email)
	_ = kept
}

func TestServiceReusesCallerTx(t *testing.T) {
	ctx := context.Background()
	_, client, sqlDB, u := setup(t, projects.Options{})
	boom := errors.New("boom")
	err := db.WithTx(ctx, sqlDB, func(_ *sql.Tx, etx *ent.Client) error {
		if _, err := projects.New(etx, projects.Options{}).Create(ctx, u.ID, false, "Web"); err != nil {
			return err
		}
		return boom
	})
	require.ErrorIs(t, err, boom, "withTx must join the caller's tx, not nest")
	require.Zero(t, client.Project.Query().CountX(ctx), "rolled back with the caller")
}

func TestNewProjectGetsDefaultQuota(t *testing.T) {
	ctx := context.Background()
	dq := projects.Quota{MaxServers: 5, MaxVCPU: 8, MaxMemoryMB: 16384, MaxDiskGB: 200, MaxIPs: 5}
	svc, _, _, u := setup(t, projects.Options{DefaultQuota: dq})
	p, err := svc.Create(ctx, u.ID, false, "Web")
	require.NoError(t, err)
	require.Equal(t, dq, p.Quota)
	got, err := svc.Get(ctx, u.ID, p.ID)
	require.NoError(t, err)
	require.Equal(t, dq, got.Quota)
}

func TestProjectCapPerUser(t *testing.T) {
	ctx := context.Background()
	svc, _, _, u := setup(t, projects.Options{MaxOwnedProjects: 3})
	for _, n := range []string{"a", "b", "c"} {
		_, err := svc.Create(ctx, u.ID, false, n)
		require.NoError(t, err)
	}
	_, err := svc.Create(ctx, u.ID, false, "d")
	var ae apierr.E
	require.ErrorAs(t, err, &ae)
	require.Equal(t, http.StatusUnprocessableEntity, ae.Status)
	require.Equal(t, "quota_exceeded", ae.Code)
	require.Equal(t, "project limit reached", ae.Msg)
}

func TestAdminProjectsUnlimited(t *testing.T) {
	ctx := context.Background()
	svc, _, _, u := setup(t, projects.Options{DefaultQuota: projects.Quota{MaxServers: 1}, MaxOwnedProjects: 1})
	for _, n := range []string{"a", "b", "c"} {
		p, err := svc.Create(ctx, u.ID, true, n)
		require.NoError(t, err)
		require.Equal(t, projects.Quota{}, p.Quota)
	}
}

func TestProjectCapConcurrentCreate(t *testing.T) {
	ctx := context.Background()
	svc, _, _, u := setup(t, projects.Options{MaxOwnedProjects: 1})
	const n = 8
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() { _, errs[i] = svc.Create(ctx, u.ID, false, fmt.Sprintf("p%d", i)) })
	}
	wg.Wait()
	ok := 0
	for _, err := range errs {
		if err == nil {
			ok++
			continue
		}
		var ae apierr.E
		require.ErrorAs(t, err, &ae)
		require.Equal(t, "quota_exceeded", ae.Code)
	}
	require.Equal(t, 1, ok)
	list, err := svc.List(ctx, u.ID)
	require.NoError(t, err)
	require.Len(t, list, 1)
}

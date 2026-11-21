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
	"github.com/nebla-cloud/nebla/internal/ent/projectmember"
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

func TestQuotaVolumes(t *testing.T) {
	ctx := context.Background()
	existing := projects.Usage{Volumes: 1, VolumeGB: 50}
	svc, _, sqlDB, u := setup(t, projects.Options{Hooks: projects.Hooks{
		Usage: func(context.Context, *ent.Client, int64) (projects.Usage, error) { return existing, nil },
	}})
	p, err := svc.Create(ctx, u.ID, false, "Web")
	require.NoError(t, err)
	q, err := svc.SetQuota(ctx, p.ID, projects.Quota{MaxVolumes: 1, MaxVolumeGB: 60})
	require.NoError(t, err)
	require.Equal(t, projects.Quota{MaxVolumes: 1, MaxVolumeGB: 60}, q)

	reserve := func(d projects.Usage) error {
		return db.WithTx(ctx, sqlDB, func(_ *sql.Tx, etx *ent.Client) error {
			return svc.ReserveQuota(ctx, etx, p.ID, d)
		})
	}
	var e apierr.E
	err = reserve(projects.Usage{Volumes: 1})
	require.True(t, errors.As(err, &e), "got %v", err)
	require.Equal(t, "quota_exceeded", e.Code)
	require.Contains(t, e.Msg, "max_volumes")

	err = reserve(projects.Usage{VolumeGB: 20})
	require.True(t, errors.As(err, &e), "got %v", err)
	require.Equal(t, "quota_exceeded", e.Code)
	require.Contains(t, e.Msg, "max_volume_gb")

	require.NoError(t, reserve(projects.Usage{VolumeGB: 10}))

	for _, bad := range []projects.Quota{{MaxVolumes: -1}, {MaxVolumeGB: -1}} {
		_, err = svc.SetQuota(ctx, p.ID, bad)
		require.True(t, errors.As(err, &e), "got %v", err)
		require.Equal(t, 422, e.Status)
	}
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

func requireErr(t *testing.T, err error, status int, code string) {
	t.Helper()
	var e apierr.E
	require.True(t, errors.As(err, &e), "got %v", err)
	require.Equal(t, status, e.Status)
	require.Equal(t, code, e.Code)
}

func TestPreviewInviteStates(t *testing.T) {
	ctx := context.Background()
	svc, client, _, owner := setup(t, projects.Options{})
	au := auth.New(client, ext.Defaults(nil).Registration)
	p, err := svc.Create(ctx, owner.ID, false, "Homelab")
	require.NoError(t, err)
	link, err := svc.Invite(ctx, owner.ID, p.ID, "new@example.com", projects.RoleMember)
	require.NoError(t, err)
	tok := inviteToken(t, link)

	pv, err := svc.PreviewInvite(ctx, tok, false)
	require.NoError(t, err)
	require.Equal(t, "Homelab", pv.ProjectName)
	require.Equal(t, "new@example.com", pv.Email)
	require.Equal(t, projects.RoleMember, pv.Role)
	require.Equal(t, "Owner", pv.InviterName)
	require.False(t, pv.AccountExists)
	require.False(t, pv.CanCreateAccount)

	pv, err = svc.PreviewInvite(ctx, tok, true)
	require.NoError(t, err)
	require.True(t, pv.CanCreateAccount)

	_, err = au.CreateUser(ctx, "new@example.com", testutil.Password, "New", false)
	require.NoError(t, err)
	pv, err = svc.PreviewInvite(ctx, tok, false)
	require.NoError(t, err)
	require.True(t, pv.AccountExists)

	_, err = svc.PreviewInvite(ctx, "no-such-token", false)
	requireErr(t, err, 404, "not_found")

	adm, err := au.CreateUser(ctx, "adm@example.com", testutil.Password, "Adm", true)
	require.NoError(t, err)
	ap, err := svc.Create(ctx, adm.ID, true, "Admin P")
	require.NoError(t, err)
	alink, err := svc.Invite(ctx, adm.ID, ap.ID, "x@example.com", projects.RoleMember)
	require.NoError(t, err)
	pv, err = svc.PreviewInvite(ctx, inviteToken(t, alink), false)
	require.NoError(t, err)
	require.True(t, pv.CanCreateAccount)

	invs, err := svc.Invites(ctx, owner.ID, p.ID)
	require.NoError(t, err)
	require.NoError(t, svc.RevokeInvite(ctx, owner.ID, p.ID, invs[0].ID))
	_, err = svc.PreviewInvite(ctx, tok, false)
	requireErr(t, err, 404, "not_found")
}

func TestPreviewInviteExpiredAcceptedAndDemoted(t *testing.T) {
	ctx := context.Background()
	svc, client, sqlDB, owner := setup(t, projects.Options{})
	au := auth.New(client, ext.Defaults(nil).Registration)
	p, err := svc.Create(ctx, owner.ID, false, "Web")
	require.NoError(t, err)

	expired, err := svc.Invite(ctx, owner.ID, p.ID, "e@example.com", projects.RoleMember)
	require.NoError(t, err)
	_, err = sqlDB.ExecContext(ctx, "update invites set expires_at = now() - interval '1 hour'")
	require.NoError(t, err)
	_, err = svc.PreviewInvite(ctx, inviteToken(t, expired), false)
	requireErr(t, err, 404, "not_found")

	u, err := au.CreateUser(ctx, "acc@example.com", testutil.Password, "Acc", false)
	require.NoError(t, err)
	acc, err := svc.Invite(ctx, owner.ID, p.ID, "acc@example.com", projects.RoleMember)
	require.NoError(t, err)
	_, err = svc.AcceptInvite(ctx, inviteToken(t, acc), &u, "", "", false)
	require.NoError(t, err)
	_, err = svc.PreviewInvite(ctx, inviteToken(t, acc), false)
	requireErr(t, err, 404, "not_found")

	adm, err := au.CreateUser(ctx, "adm@example.com", testutil.Password, "Adm", false)
	require.NoError(t, err)
	al, err := svc.Invite(ctx, owner.ID, p.ID, "adm@example.com", projects.RoleAdmin)
	require.NoError(t, err)
	_, err = svc.AcceptInvite(ctx, inviteToken(t, al), &adm, "", "", false)
	require.NoError(t, err)
	link, err := svc.Invite(ctx, adm.ID, p.ID, "z@example.com", projects.RoleMember)
	require.NoError(t, err)
	_, err = svc.PreviewInvite(ctx, inviteToken(t, link), false)
	require.NoError(t, err)
	_, err = client.ProjectMember.Update().Where(projectmember.UserID(adm.ID)).SetRole(projects.RoleMember).Save(ctx)
	require.NoError(t, err)
	_, err = svc.PreviewInvite(ctx, inviteToken(t, link), false)
	requireErr(t, err, 404, "not_found")
}

func TestSelfLeave(t *testing.T) {
	ctx := context.Background()
	svc, client, _, owner := setup(t, projects.Options{})
	au := auth.New(client, ext.Defaults(nil).Registration)
	p, err := svc.Create(ctx, owner.ID, false, "Web")
	require.NoError(t, err)
	join := func(email, role string) auth.User {
		u, err := au.CreateUser(ctx, email, testutil.Password, email, false)
		require.NoError(t, err)
		l, err := svc.Invite(ctx, owner.ID, p.ID, email, role)
		require.NoError(t, err)
		_, err = svc.AcceptInvite(ctx, inviteToken(t, l), &u, "", "", false)
		require.NoError(t, err)
		return u
	}
	mem := join("m@example.com", projects.RoleMember)
	ro := join("r@example.com", projects.RoleReadOnly)
	other := join("o@example.com", projects.RoleMember)

	requireErr(t, svc.RemoveMember(ctx, mem.ID, p.ID, other.ID), 403, "forbidden")
	require.NoError(t, svc.RemoveMember(ctx, mem.ID, p.ID, mem.ID))
	require.NoError(t, svc.RemoveMember(ctx, ro.ID, p.ID, ro.ID))

	requireErr(t, svc.RemoveMember(ctx, owner.ID, p.ID, owner.ID), 409, "last_owner")
	o2 := join("o2@example.com", projects.RoleOwner)
	require.NoError(t, svc.RemoveMember(ctx, o2.ID, p.ID, o2.ID))
	_, err = svc.Role(ctx, o2.ID, p.ID)
	requireErr(t, err, 404, "not_found")

	adm := join("a@example.com", projects.RoleAdmin)
	_, err = svc.Invite(ctx, adm.ID, p.ID, "pending@example.com", projects.RoleMember)
	require.NoError(t, err)
	require.NoError(t, svc.RemoveMember(ctx, adm.ID, p.ID, adm.ID))
	invs, err := svc.Invites(ctx, owner.ID, p.ID)
	require.NoError(t, err)
	require.Empty(t, invs)
}

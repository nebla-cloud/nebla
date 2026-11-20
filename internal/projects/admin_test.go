package projects_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/nebla-cloud/nebla/internal/apierr"
	"github.com/nebla-cloud/nebla/internal/auth"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/invite"
	"github.com/nebla-cloud/nebla/internal/ent/locationenrollment"
	"github.com/nebla-cloud/nebla/internal/ent/projectmember"
	"github.com/nebla-cloud/nebla/internal/ent/session"
	"github.com/nebla-cloud/nebla/internal/ent/webauthncredential"
	"github.com/nebla-cloud/nebla/internal/ext"
	"github.com/nebla-cloud/nebla/internal/projects"
	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/stretchr/testify/require"
)

func requireAPIErr(t *testing.T, err error, status int, code string) apierr.E {
	t.Helper()
	var e apierr.E
	require.True(t, errors.As(err, &e), "got %v", err)
	require.Equal(t, status, e.Status)
	require.Equal(t, code, e.Code)
	return e
}

type adminEnv struct {
	svc    *projects.Service
	db     *ent.Client
	au     *auth.Service
	owner  auth.User
	admin  auth.User
	busy   map[int64]bool
	usages map[int64]projects.Usage
}

func newAdminEnv(t *testing.T) *adminEnv {
	t.Helper()
	e := &adminEnv{busy: map[int64]bool{}, usages: map[int64]projects.Usage{}}
	svc, client, _, owner := setup(t, projects.Options{Hooks: projects.Hooks{
		HasServers: func(_ context.Context, _ *ent.Client, id int64) (bool, error) { return e.busy[id], nil },
		Usage: func(_ context.Context, _ *ent.Client, id int64) (projects.Usage, error) {
			return e.usages[id], nil
		},
	}})
	e.svc, e.db, e.owner = svc, client, owner
	e.au = auth.New(client, ext.Defaults(nil).Registration)
	e.admin = e.user(t, "admin@example.com", true)
	return e
}

func (e *adminEnv) user(t *testing.T, email string, admin bool) auth.User {
	t.Helper()
	u, err := e.au.CreateUser(context.Background(), email, testutil.Password, "U", admin)
	require.NoError(t, err)
	return u
}

func (e *adminEnv) project(t *testing.T, ownerID int64, name string) int64 {
	t.Helper()
	p, err := e.svc.Create(context.Background(), ownerID, false, name)
	require.NoError(t, err)
	return p.ID
}

func (e *adminEnv) addMember(t *testing.T, projectID, userID int64, role string) {
	t.Helper()
	require.NoError(t, e.db.ProjectMember.Create().SetProjectID(projectID).SetUserID(userID).SetRole(role).
		Exec(context.Background()))
}

func (e *adminEnv) role(t *testing.T, projectID, userID int64) string {
	t.Helper()
	r, err := e.svc.Role(context.Background(), userID, projectID)
	if err != nil {
		return ""
	}
	return r
}

func TestDeleteUser(t *testing.T) {
	ctx := context.Background()
	e := newAdminEnv(t)
	requireAPIErr(t, e.svc.DeleteUser(ctx, e.admin.ID, e.admin.ID, false), http.StatusUnprocessableEntity, "self_admin_action")
	other := e.user(t, "other@example.com", false)
	requireAPIErr(t, e.svc.DeleteUser(ctx, other.ID, e.admin.ID, false), http.StatusConflict, "last_admin")
	requireAPIErr(t, e.svc.DeleteUser(ctx, e.admin.ID, 99999, false), http.StatusNotFound, "not_found")

	u := e.user(t, "u@example.com", false)
	pid := e.project(t, e.owner.ID, "Shared")
	e.addMember(t, pid, u.ID, projects.RoleAdmin)
	_, err := e.svc.Invite(ctx, u.ID, pid, "x@example.com", projects.RoleMember)
	require.NoError(t, err)
	_, err = e.svc.Invite(ctx, e.owner.ID, pid, "y@example.com", projects.RoleMember)
	require.NoError(t, err)
	mine, err := e.db.Project.Create().SetName("Created").SetCreatedBy(u.ID).Save(ctx)
	require.NoError(t, err)
	e.addMember(t, mine.ID, e.owner.ID, projects.RoleOwner)
	_, err = e.au.NewSession(ctx, u.ID)
	require.NoError(t, err)
	require.NoError(t, e.db.WebauthnCredential.Create().SetUserID(u.ID).SetCredentialID([]byte("c")).SetName("P").
		SetData(webauthn.Credential{}).Exec(ctx))
	require.NoError(t, e.db.LocationEnrollment.Create().SetName("L").SetTokenName("t").SetTokenHash([]byte("h")).
		SetExpiresAt(time.Now().Add(time.Hour)).SetCreatedBy(u.ID).Exec(ctx))

	require.NoError(t, e.svc.DeleteUser(ctx, e.admin.ID, u.ID, false))
	_, err = e.db.User.Get(ctx, u.ID)
	require.True(t, ent.IsNotFound(err))
	require.Zero(t, e.db.Session.Query().Where(session.UserID(u.ID)).CountX(ctx))
	require.Zero(t, e.db.WebauthnCredential.Query().Where(webauthncredential.UserID(u.ID)).CountX(ctx))
	require.Zero(t, e.db.ProjectMember.Query().Where(projectmember.UserID(u.ID)).CountX(ctx))
	require.Zero(t, e.db.Invite.Query().Where(invite.CreatedBy(u.ID)).CountX(ctx))
	require.Equal(t, 1, e.db.Invite.Query().Where(invite.ProjectID(pid)).CountX(ctx), "others' invites stay")
	require.Zero(t, e.db.LocationEnrollment.Query().Where(locationenrollment.CreatedBy(u.ID)).CountX(ctx))
	p, err := e.db.Project.Get(ctx, mine.ID)
	require.NoError(t, err)
	require.Nil(t, p.CreatedBy)
}

func TestDeleteUserSoleOwner(t *testing.T) {
	ctx := context.Background()
	e := newAdminEnv(t)
	u := e.user(t, "u@example.com", false)
	homelab := e.project(t, u.ID, "Homelab")
	lab2 := e.project(t, u.ID, "Lab 2")
	shared := e.project(t, u.ID, "Shared")
	e.addMember(t, shared, e.owner.ID, projects.RoleOwner)

	err := e.svc.DeleteUser(ctx, e.admin.ID, u.ID, false)
	ae := requireAPIErr(t, err, http.StatusConflict, "sole_owner")
	require.Equal(t, "They're the only owner of Homelab and Lab 2.", ae.Msg)
	require.NotContains(t, ae.Msg, "Shared")
	_, err = e.db.User.Get(ctx, u.ID)
	require.NoError(t, err, "nothing deleted")
	require.Equal(t, projects.RoleOwner, e.role(t, homelab, u.ID))
	require.Equal(t, projects.RoleOwner, e.role(t, lab2, u.ID))

	for _, name := range []string{"Lab 3", "Lab 4"} {
		e.project(t, u.ID, name)
	}
	err = e.svc.DeleteUser(ctx, e.admin.ID, u.ID, false)
	ae = requireAPIErr(t, err, http.StatusConflict, "sole_owner")
	require.Equal(t, "They're the only owner of Homelab, Lab 2, Lab 3 and 1 more.", ae.Msg)
}

func TestDeleteUserTakeOver(t *testing.T) {
	ctx := context.Background()
	t.Run("not_member", func(t *testing.T) {
		e := newAdminEnv(t)
		u := e.user(t, "u@example.com", false)
		pid := e.project(t, u.ID, "Homelab")
		e.addMember(t, pid, e.owner.ID, projects.RoleMember)
		_, err := e.svc.Invite(ctx, u.ID, pid, "mine@example.com", projects.RoleMember)
		require.NoError(t, err)
		e.busy[pid] = true

		require.NoError(t, e.svc.DeleteUser(ctx, e.admin.ID, u.ID, true))
		require.Equal(t, projects.RoleOwner, e.role(t, pid, e.admin.ID))
		require.Equal(t, projects.RoleMember, e.role(t, pid, e.owner.ID))
		_, err = e.db.Project.Get(ctx, pid)
		require.NoError(t, err, "the project and its servers stay")
		d, err := e.svc.AdminGet(ctx, pid)
		require.NoError(t, err)
		require.Equal(t, 2, d.MemberCount)
		require.Zero(t, e.db.Invite.Query().Where(invite.ProjectID(pid)).CountX(ctx), "the deleted user's invites go")
	})
	t.Run("existing_member", func(t *testing.T) {
		e := newAdminEnv(t)
		u := e.user(t, "u@example.com", false)
		pid := e.project(t, u.ID, "Homelab")
		e.addMember(t, pid, e.admin.ID, projects.RoleReadOnly)
		e.addMember(t, pid, e.owner.ID, projects.RoleAdmin)
		_, err := e.svc.Invite(ctx, e.owner.ID, pid, "theirs@example.com", projects.RoleMember)
		require.NoError(t, err)

		require.NoError(t, e.svc.DeleteUser(ctx, e.admin.ID, u.ID, true))
		require.Equal(t, projects.RoleOwner, e.role(t, pid, e.admin.ID))
		n := e.db.ProjectMember.Query().Where(projectmember.ProjectID(pid), projectmember.UserID(e.admin.ID)).CountX(ctx)
		require.Equal(t, 1, n)
		require.Equal(t, 1, e.db.Invite.Query().Where(invite.ProjectID(pid)).CountX(ctx), "others' invites stay")
	})
}

func TestAdminGetProject(t *testing.T) {
	ctx := context.Background()
	e := newAdminEnv(t)
	pid := e.project(t, e.owner.ID, "Web")
	m := e.user(t, "m@example.com", false)
	e.addMember(t, pid, m.ID, projects.RoleMember)
	e.usages[pid] = projects.Usage{Servers: 2, VCPU: 4, MemoryMB: 2048, DiskGB: 40, IPs: 1}

	got, err := e.svc.AdminGet(ctx, pid)
	require.NoError(t, err)
	require.Equal(t, "Web", got.Name)
	require.Equal(t, "owner@example.com", got.OwnerEmail)
	require.Equal(t, e.usages[pid], got.Usage)
	require.Equal(t, 2, got.MemberCount)
	require.False(t, got.Created.IsZero())
	require.Len(t, got.Members, 2)
	require.Equal(t, e.owner.ID, got.Members[0].UserID)
	require.Equal(t, projects.RoleOwner, got.Members[0].Role)
	require.Equal(t, "m@example.com", got.Members[1].Email)

	_, err = e.svc.AdminGet(ctx, 99999)
	requireAPIErr(t, err, http.StatusNotFound, "not_found")
}

func TestAdminDeleteProject(t *testing.T) {
	ctx := context.Background()
	e := newAdminEnv(t)
	pid := e.project(t, e.owner.ID, "Web")
	e.busy[pid] = true
	requireAPIErr(t, e.svc.AdminDelete(ctx, pid), http.StatusConflict, "project_not_empty")
	_, err := e.db.Project.Get(ctx, pid)
	require.NoError(t, err)

	e.busy[pid] = false
	require.NoError(t, e.svc.AdminDelete(ctx, pid))
	_, err = e.db.Project.Get(ctx, pid)
	require.True(t, ent.IsNotFound(err))
	requireAPIErr(t, e.svc.AdminDelete(ctx, pid), http.StatusNotFound, "not_found")
}

func TestListAllUsageAndMembers(t *testing.T) {
	ctx := context.Background()
	e := newAdminEnv(t)
	a := e.project(t, e.owner.ID, "A")
	b := e.project(t, e.owner.ID, "B")
	c := e.project(t, e.owner.ID, "C")
	for _, email := range []string{"x@example.com", "y@example.com"} {
		e.addMember(t, b, e.user(t, email, false).ID, projects.RoleMember)
	}
	e.usages[a] = projects.Usage{Servers: 1}
	e.usages[b] = projects.Usage{Servers: 3, IPs: 2}

	ps, total, err := e.svc.ListAll(ctx, 1, 2)
	require.NoError(t, err)
	require.Equal(t, 3, total)
	require.Len(t, ps, 2)
	require.Equal(t, e.usages[a], ps[0].Usage)
	require.Equal(t, 1, ps[0].MemberCount)
	require.Equal(t, e.usages[b], ps[1].Usage)
	require.Equal(t, 3, ps[1].MemberCount)
	require.False(t, ps[1].Created.IsZero())
	ps, _, err = e.svc.ListAll(ctx, 2, 2)
	require.NoError(t, err)
	require.Equal(t, c, ps[0].ID)
	require.Equal(t, projects.Usage{}, ps[0].Usage)
	require.Equal(t, 1, ps[0].MemberCount)
}

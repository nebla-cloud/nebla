package projects_test

import (
	"context"
	"testing"

	"github.com/Hexeption/nebla/internal/auth"
	"github.com/Hexeption/nebla/internal/ent"
	"github.com/Hexeption/nebla/internal/ext"
	"github.com/Hexeption/nebla/internal/projects"
	"github.com/Hexeption/nebla/internal/testutil"
	"github.com/stretchr/testify/require"
)

var backfillQ = projects.Quota{MaxServers: 5, MaxVCPU: 8, MaxMemoryMB: 16384, MaxDiskGB: 200, MaxIPs: 5}

// legacyProject inserts a project the way it existed before created_by and default quotas.
func legacyProject(t *testing.T, c *ent.Client, owner int64, q projects.Quota) int64 {
	t.Helper()
	ctx := context.Background()
	p, err := c.Project.Create().SetName("legacy").SetMaxServers(q.MaxServers).SetMaxVcpu(q.MaxVCPU).
		SetMaxMemoryMB(q.MaxMemoryMB).SetMaxDiskGB(q.MaxDiskGB).SetMaxIps(q.MaxIPs).Save(ctx)
	require.NoError(t, err)
	require.NoError(t, c.ProjectMember.Create().SetProjectID(p.ID).SetUserID(owner).SetRole(projects.RoleOwner).Exec(ctx))
	return p.ID
}

func quotaOf(t *testing.T, c *ent.Client, id int64) projects.Quota {
	t.Helper()
	p, err := c.Project.Get(context.Background(), id)
	require.NoError(t, err)
	return projects.Quota{MaxServers: p.MaxServers, MaxVCPU: p.MaxVcpu, MaxMemoryMB: p.MaxMemoryMB, MaxDiskGB: p.MaxDiskGB, MaxIPs: p.MaxIps}
}

func TestBackfillAppliesDefaultToLegacy(t *testing.T) {
	ctx := context.Background()
	_, c, _, u := setup(t, projects.Options{})
	admin, err := auth.New(c, ext.Defaults(nil).Registration).CreateUser(ctx, "admin@example.test", testutil.Password, "A", true)
	require.NoError(t, err)
	plain := legacyProject(t, c, u.ID, projects.Quota{})
	adminOwned := legacyProject(t, c, admin.ID, projects.Quota{})
	custom := legacyProject(t, c, u.ID, projects.Quota{MaxServers: 9})

	n, err := projects.BackfillDefaultQuota(ctx, c, backfillQ)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.Equal(t, backfillQ, quotaOf(t, c, plain))
	require.Equal(t, projects.Quota{}, quotaOf(t, c, adminOwned))
	require.Equal(t, projects.Quota{MaxServers: 9}, quotaOf(t, c, custom))
}

func TestBackfillRunsOnce(t *testing.T) {
	ctx := context.Background()
	svc, c, _, u := setup(t, projects.Options{})
	id := legacyProject(t, c, u.ID, projects.Quota{})
	n, err := projects.BackfillDefaultQuota(ctx, c, backfillQ)
	require.NoError(t, err)
	require.Equal(t, 1, n)

	_, err = svc.SetQuota(ctx, id, projects.Quota{})
	require.NoError(t, err)
	n, err = projects.BackfillDefaultQuota(ctx, c, backfillQ)
	require.NoError(t, err)
	require.Zero(t, n)
	require.Equal(t, projects.Quota{}, quotaOf(t, c, id))
}

func TestBackfillSkipsZeroDefault(t *testing.T) {
	ctx := context.Background()
	_, c, _, u := setup(t, projects.Options{})
	id := legacyProject(t, c, u.ID, projects.Quota{})
	n, err := projects.BackfillDefaultQuota(ctx, c, projects.Quota{})
	require.NoError(t, err)
	require.Zero(t, n)
	require.Zero(t, c.Setting.Query().CountX(ctx))

	n, err = projects.BackfillDefaultQuota(ctx, c, backfillQ)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.Equal(t, backfillQ, quotaOf(t, c, id))
}

func TestBackfillCreatedByUsesFirstOwner(t *testing.T) {
	ctx := context.Background()
	svc, c, _, a := setup(t, projects.Options{MaxOwnedProjects: 1})
	b, err := auth.New(c, ext.Defaults(nil).Registration).CreateUser(ctx, "b@example.test", testutil.Password, "B", false)
	require.NoError(t, err)
	id := legacyProject(t, c, a.ID, projects.Quota{})
	require.NoError(t, c.ProjectMember.Create().SetProjectID(id).SetUserID(b.ID).SetRole(projects.RoleOwner).Exec(ctx))

	n, err := projects.BackfillCreatedBy(ctx, c)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	p, err := c.Project.Get(ctx, id)
	require.NoError(t, err)
	require.Equal(t, a.ID, *p.CreatedBy)

	_, err = svc.Create(ctx, a.ID, false, "a2")
	requireCapReached(t, err)
	_, err = svc.Create(ctx, b.ID, false, "b1")
	require.NoError(t, err)
}

func TestBackfillCreatedByRunsOnce(t *testing.T) {
	ctx := context.Background()
	_, c, _, a := setup(t, projects.Options{})
	b, err := auth.New(c, ext.Defaults(nil).Registration).CreateUser(ctx, "b@example.test", testutil.Password, "B", false)
	require.NoError(t, err)
	id := legacyProject(t, c, a.ID, projects.Quota{})
	require.NoError(t, c.ProjectMember.Create().SetProjectID(id).SetUserID(b.ID).SetRole(projects.RoleOwner).Exec(ctx))
	_, err = projects.BackfillCreatedBy(ctx, c)
	require.NoError(t, err)
	require.NoError(t, c.User.DeleteOneID(a.ID).Exec(ctx))

	n, err := projects.BackfillCreatedBy(ctx, c)
	require.NoError(t, err)
	require.Zero(t, n)
	p, err := c.Project.Get(ctx, id)
	require.NoError(t, err)
	require.Nil(t, p.CreatedBy)
}

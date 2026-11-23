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

var backfillQ = projects.Quota{MaxServers: 5, MaxVCPU: 8, MaxMemoryMB: 16384, MaxDiskGB: 200, MaxIPs: 5}

// legacyProject inserts a project the way it existed before created_by and default quotas.
func legacyProject(t *testing.T, c *ent.Client, owner int64, q projects.Quota) int64 {
	t.Helper()
	ctx := context.Background()
	p, err := c.Project.Create().SetName("legacy").SetMaxServers(q.MaxServers).SetMaxVcpu(q.MaxVCPU).
		SetMaxMemoryMB(q.MaxMemoryMB).SetMaxDiskGB(q.MaxDiskGB).SetMaxIps(q.MaxIPs).
		SetMaxVolumes(q.MaxVolumes).SetMaxVolumeGB(q.MaxVolumeGB).SetMaxBackups(q.MaxBackups).SetMaxNetworks(q.MaxNetworks).
		SetMaxSnapshots(q.MaxSnapshots).SetMaxSnapshotGB(q.MaxSnapshotGB).Save(ctx)
	require.NoError(t, err)
	require.NoError(t, c.ProjectMember.Create().SetProjectID(p.ID).SetUserID(owner).SetRole(projects.RoleOwner).Exec(ctx))
	return p.ID
}

func quotaOf(t *testing.T, c *ent.Client, id int64) projects.Quota {
	t.Helper()
	p, err := c.Project.Get(context.Background(), id)
	require.NoError(t, err)
	return projects.Quota{MaxServers: p.MaxServers, MaxVCPU: p.MaxVcpu, MaxMemoryMB: p.MaxMemoryMB, MaxDiskGB: p.MaxDiskGB, MaxIPs: p.MaxIps,
		MaxVolumes: p.MaxVolumes, MaxVolumeGB: p.MaxVolumeGB, MaxBackups: p.MaxBackups, MaxNetworks: p.MaxNetworks,
		MaxSnapshots: p.MaxSnapshots, MaxSnapshotGB: p.MaxSnapshotGB}
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

func TestBackfillVolumeQuota(t *testing.T) {
	ctx := context.Background()
	_, c, _, u := setup(t, projects.Options{})
	admin, err := auth.New(c, ext.Defaults(nil).Registration).CreateUser(ctx, "admin@example.test", testutil.Password, "A", true)
	require.NoError(t, err)
	limited := legacyProject(t, c, u.ID, projects.Quota{MaxServers: 9})
	adminOwned := legacyProject(t, c, admin.ID, projects.Quota{MaxServers: 9})
	unlimited := legacyProject(t, c, u.ID, projects.Quota{})
	has := legacyProject(t, c, u.ID, projects.Quota{MaxServers: 9, MaxVolumes: 2})
	vq := projects.Quota{MaxVolumes: 5, MaxVolumeGB: 100}

	n, err := projects.BackfillVolumeQuota(ctx, c, vq)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.Equal(t, projects.Quota{MaxServers: 9, MaxVolumes: 5, MaxVolumeGB: 100}, quotaOf(t, c, limited))
	require.Equal(t, projects.Quota{MaxServers: 9}, quotaOf(t, c, adminOwned))
	require.Equal(t, projects.Quota{}, quotaOf(t, c, unlimited))
	require.Equal(t, projects.Quota{MaxServers: 9, MaxVolumes: 2}, quotaOf(t, c, has))

	second := legacyProject(t, c, u.ID, projects.Quota{MaxServers: 3})
	n, err = projects.BackfillVolumeQuota(ctx, c, vq)
	require.NoError(t, err)
	require.Zero(t, n)
	require.Equal(t, projects.Quota{MaxServers: 3}, quotaOf(t, c, second))
}

func TestBackfillVolumeQuotaSkipsZero(t *testing.T) {
	ctx := context.Background()
	_, c, _, u := setup(t, projects.Options{})
	id := legacyProject(t, c, u.ID, projects.Quota{MaxServers: 9})
	n, err := projects.BackfillVolumeQuota(ctx, c, projects.Quota{MaxServers: 1})
	require.NoError(t, err)
	require.Zero(t, n)
	require.Zero(t, c.Setting.Query().CountX(ctx))
	n, err = projects.BackfillVolumeQuota(ctx, c, projects.Quota{MaxVolumes: 5, MaxVolumeGB: 100})
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.Equal(t, 5, quotaOf(t, c, id).MaxVolumes)
}

func TestBackfillBackupQuota(t *testing.T) {
	ctx := context.Background()
	_, c, _, u := setup(t, projects.Options{})
	admin, err := auth.New(c, ext.Defaults(nil).Registration).CreateUser(ctx, "admin@example.test", testutil.Password, "A", true)
	require.NoError(t, err)
	limited := legacyProject(t, c, u.ID, projects.Quota{MaxServers: 9})
	adminOwned := legacyProject(t, c, admin.ID, projects.Quota{MaxServers: 9})
	unlimited := legacyProject(t, c, u.ID, projects.Quota{})
	has := legacyProject(t, c, u.ID, projects.Quota{MaxServers: 9, MaxBackups: 2})
	bq := projects.Quota{MaxBackups: 35}

	n, err := projects.BackfillBackupQuota(ctx, c, projects.Quota{MaxServers: 1})
	require.NoError(t, err)
	require.Zero(t, n)
	require.Zero(t, c.Setting.Query().CountX(ctx), "a zero backup limit leaves the guard unset")

	n, err = projects.BackfillBackupQuota(ctx, c, bq)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.Equal(t, projects.Quota{MaxServers: 9, MaxBackups: 35}, quotaOf(t, c, limited))
	require.Equal(t, projects.Quota{MaxServers: 9}, quotaOf(t, c, adminOwned))
	require.Equal(t, projects.Quota{}, quotaOf(t, c, unlimited))
	require.Equal(t, projects.Quota{MaxServers: 9, MaxBackups: 2}, quotaOf(t, c, has))

	second := legacyProject(t, c, u.ID, projects.Quota{MaxServers: 3})
	n, err = projects.BackfillBackupQuota(ctx, c, bq)
	require.NoError(t, err)
	require.Zero(t, n)
	require.Equal(t, projects.Quota{MaxServers: 3}, quotaOf(t, c, second))
}

func TestBackfillNetworkQuota(t *testing.T) {
	ctx := context.Background()
	_, c, _, u := setup(t, projects.Options{})
	admin, err := auth.New(c, ext.Defaults(nil).Registration).CreateUser(ctx, "admin@example.test", testutil.Password, "A", true)
	require.NoError(t, err)
	limited := legacyProject(t, c, u.ID, projects.Quota{MaxServers: 9})
	adminOwned := legacyProject(t, c, admin.ID, projects.Quota{MaxServers: 9})
	unlimited := legacyProject(t, c, u.ID, projects.Quota{})
	has := legacyProject(t, c, u.ID, projects.Quota{MaxServers: 9, MaxNetworks: 2})
	nq := projects.Quota{MaxNetworks: 3}

	n, err := projects.BackfillNetworkQuota(ctx, c, nq)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.Equal(t, projects.Quota{MaxServers: 9, MaxNetworks: 3}, quotaOf(t, c, limited))
	require.Equal(t, projects.Quota{MaxServers: 9}, quotaOf(t, c, adminOwned))
	require.Equal(t, projects.Quota{}, quotaOf(t, c, unlimited))
	require.Equal(t, projects.Quota{MaxServers: 9, MaxNetworks: 2}, quotaOf(t, c, has))

	second := legacyProject(t, c, u.ID, projects.Quota{MaxServers: 3})
	n, err = projects.BackfillNetworkQuota(ctx, c, nq)
	require.NoError(t, err)
	require.Zero(t, n)
	require.Equal(t, projects.Quota{MaxServers: 3}, quotaOf(t, c, second))
}

func TestBackfillNetworkQuotaSkipsZero(t *testing.T) {
	ctx := context.Background()
	_, c, _, u := setup(t, projects.Options{})
	id := legacyProject(t, c, u.ID, projects.Quota{MaxServers: 9})
	n, err := projects.BackfillNetworkQuota(ctx, c, projects.Quota{MaxServers: 1})
	require.NoError(t, err)
	require.Zero(t, n)
	require.Zero(t, c.Setting.Query().CountX(ctx))
	n, err = projects.BackfillNetworkQuota(ctx, c, projects.Quota{MaxNetworks: 3})
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.Equal(t, 3, quotaOf(t, c, id).MaxNetworks)
}

func TestBackfillSnapshotQuota(t *testing.T) {
	ctx := context.Background()
	_, c, _, u := setup(t, projects.Options{})
	admin, err := auth.New(c, ext.Defaults(nil).Registration).CreateUser(ctx, "admin@example.test", testutil.Password, "A", true)
	require.NoError(t, err)
	limited := legacyProject(t, c, u.ID, projects.Quota{MaxServers: 9})
	adminOwned := legacyProject(t, c, admin.ID, projects.Quota{MaxServers: 9})
	unlimited := legacyProject(t, c, u.ID, projects.Quota{})
	has := legacyProject(t, c, u.ID, projects.Quota{MaxServers: 9, MaxSnapshots: 2})
	sq := projects.Quota{MaxSnapshots: 10, MaxSnapshotGB: 200}

	n, err := projects.BackfillSnapshotQuota(ctx, c, sq)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.Equal(t, projects.Quota{MaxServers: 9, MaxSnapshots: 10, MaxSnapshotGB: 200}, quotaOf(t, c, limited))
	require.Equal(t, projects.Quota{MaxServers: 9}, quotaOf(t, c, adminOwned))
	require.Equal(t, projects.Quota{}, quotaOf(t, c, unlimited))
	require.Equal(t, projects.Quota{MaxServers: 9, MaxSnapshots: 2}, quotaOf(t, c, has))

	second := legacyProject(t, c, u.ID, projects.Quota{MaxServers: 3})
	n, err = projects.BackfillSnapshotQuota(ctx, c, sq)
	require.NoError(t, err)
	require.Zero(t, n)
	require.Equal(t, projects.Quota{MaxServers: 3}, quotaOf(t, c, second))
}

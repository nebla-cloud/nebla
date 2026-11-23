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

package projects

import (
	"context"
	"database/sql"
	"errors"

	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/project"
	"github.com/nebla-cloud/nebla/internal/ent/projectmember"
	"github.com/nebla-cloud/nebla/internal/ent/setting"
	"github.com/nebla-cloud/nebla/internal/ent/user"
)

const backfillKey = "projects.default_quota_backfilled"

// BackfillDefaultQuota gives q, once per install, to unlimited projects that no panel admin owns.
func BackfillDefaultQuota(ctx context.Context, db *ent.Client, q Quota) (int, error) {
	if q == (Quota{}) {
		return 0, nil
	}
	n := 0
	err := withTx(ctx, db, func(tx *ent.Client) error {
		_, err := tx.Setting.Create().SetID(backfillKey).SetValue("true").
			OnConflictColumns(setting.FieldID).DoNothing().ID(ctx)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		n, err = tx.Project.Update().
			Where(
				project.MaxServers(0), project.MaxVcpu(0), project.MaxMemoryMB(0), project.MaxDiskGB(0), project.MaxIps(0),
				project.Not(project.HasMembersWith(projectmember.Role(RoleOwner), projectmember.HasUserWith(user.IsAdmin(true)))),
			).
			SetMaxServers(q.MaxServers).SetMaxVcpu(q.MaxVCPU).SetMaxMemoryMB(q.MaxMemoryMB).
			SetMaxDiskGB(q.MaxDiskGB).SetMaxIps(q.MaxIPs).Save(ctx)
		return err
	})
	return n, err
}

const volumeBackfillKey = "projects.volume_quota_backfilled"

// BackfillVolumeQuota gives q's volume limits, once per install, to limited projects that no panel admin owns.
func BackfillVolumeQuota(ctx context.Context, db *ent.Client, q Quota) (int, error) {
	if q.MaxVolumes == 0 && q.MaxVolumeGB == 0 {
		return 0, nil
	}
	n := 0
	err := withTx(ctx, db, func(tx *ent.Client) error {
		_, err := tx.Setting.Create().SetID(volumeBackfillKey).SetValue("true").
			OnConflictColumns(setting.FieldID).DoNothing().ID(ctx)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		// A project with all five older limits at 0 was made unlimited on purpose.
		n, err = tx.Project.Update().
			Where(
				project.MaxVolumes(0), project.MaxVolumeGB(0),
				project.Not(project.And(project.MaxServers(0), project.MaxVcpu(0), project.MaxMemoryMB(0), project.MaxDiskGB(0), project.MaxIps(0))),
				project.Not(project.HasMembersWith(projectmember.Role(RoleOwner), projectmember.HasUserWith(user.IsAdmin(true)))),
			).
			SetMaxVolumes(q.MaxVolumes).SetMaxVolumeGB(q.MaxVolumeGB).Save(ctx)
		return err
	})
	return n, err
}

const backupBackfillKey = "projects.backup_quota_backfilled"

// BackfillBackupQuota gives q's backup limit, once per install, to limited projects that no panel admin owns.
func BackfillBackupQuota(ctx context.Context, db *ent.Client, q Quota) (int, error) {
	if q.MaxBackups == 0 {
		return 0, nil
	}
	n := 0
	err := withTx(ctx, db, func(tx *ent.Client) error {
		_, err := tx.Setting.Create().SetID(backupBackfillKey).SetValue("true").
			OnConflictColumns(setting.FieldID).DoNothing().ID(ctx)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		// A project with all five older limits at 0 was made unlimited on purpose.
		n, err = tx.Project.Update().
			Where(
				project.MaxBackups(0),
				project.Not(project.And(project.MaxServers(0), project.MaxVcpu(0), project.MaxMemoryMB(0), project.MaxDiskGB(0), project.MaxIps(0))),
				project.Not(project.HasMembersWith(projectmember.Role(RoleOwner), projectmember.HasUserWith(user.IsAdmin(true)))),
			).
			SetMaxBackups(q.MaxBackups).Save(ctx)
		return err
	})
	return n, err
}

const networkBackfillKey = "projects.network_quota_backfilled"

// BackfillNetworkQuota gives q's network limit, once per install, to limited projects that no panel admin owns.
func BackfillNetworkQuota(ctx context.Context, db *ent.Client, q Quota) (int, error) {
	if q.MaxNetworks == 0 {
		return 0, nil
	}
	n := 0
	err := withTx(ctx, db, func(tx *ent.Client) error {
		_, err := tx.Setting.Create().SetID(networkBackfillKey).SetValue("true").
			OnConflictColumns(setting.FieldID).DoNothing().ID(ctx)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		// A project with all five older limits at 0 was made unlimited on purpose.
		n, err = tx.Project.Update().
			Where(
				project.MaxNetworks(0),
				project.Not(project.And(project.MaxServers(0), project.MaxVcpu(0), project.MaxMemoryMB(0), project.MaxDiskGB(0), project.MaxIps(0))),
				project.Not(project.HasMembersWith(projectmember.Role(RoleOwner), projectmember.HasUserWith(user.IsAdmin(true)))),
			).
			SetMaxNetworks(q.MaxNetworks).Save(ctx)
		return err
	})
	return n, err
}

const snapshotBackfillKey = "projects.snapshot_quota_backfilled"

// BackfillSnapshotQuota gives q's snapshot limits, once per install, to limited projects that no panel admin owns.
func BackfillSnapshotQuota(ctx context.Context, db *ent.Client, q Quota) (int, error) {
	if q.MaxSnapshots == 0 && q.MaxSnapshotGB == 0 {
		return 0, nil
	}
	n := 0
	err := withTx(ctx, db, func(tx *ent.Client) error {
		_, err := tx.Setting.Create().SetID(snapshotBackfillKey).SetValue("true").
			OnConflictColumns(setting.FieldID).DoNothing().ID(ctx)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		// A project with all five older limits at 0 was made unlimited on purpose.
		n, err = tx.Project.Update().
			Where(
				project.MaxSnapshots(0), project.MaxSnapshotGB(0),
				project.Not(project.And(project.MaxServers(0), project.MaxVcpu(0), project.MaxMemoryMB(0), project.MaxDiskGB(0), project.MaxIps(0))),
				project.Not(project.HasMembersWith(projectmember.Role(RoleOwner), projectmember.HasUserWith(user.IsAdmin(true)))),
			).
			SetMaxSnapshots(q.MaxSnapshots).SetMaxSnapshotGB(q.MaxSnapshotGB).Save(ctx)
		return err
	})
	return n, err
}

const createdByKey = "projects.created_by_backfilled"

// BackfillCreatedBy sets created_by, once per install, to each older project's earliest owner.
func BackfillCreatedBy(ctx context.Context, db *ent.Client) (int, error) {
	n := 0
	err := withTx(ctx, db, func(tx *ent.Client) error {
		_, err := tx.Setting.Create().SetID(createdByKey).SetValue("true").
			OnConflictColumns(setting.FieldID).DoNothing().ID(ctx)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		ps, err := tx.Project.Query().Where(project.CreatedByIsNil()).
			WithMembers(func(q *ent.ProjectMemberQuery) {
				q.Where(projectmember.Role(RoleOwner)).
					Order(ent.Asc(projectmember.FieldCreatedAt), ent.Asc(projectmember.FieldID))
			}).All(ctx)
		if err != nil {
			return err
		}
		for _, p := range ps {
			if len(p.Edges.Members) == 0 {
				continue
			}
			if err := tx.Project.UpdateOneID(p.ID).SetCreatedBy(p.Edges.Members[0].UserID).Exec(ctx); err != nil {
				return err
			}
			n++
		}
		return nil
	})
	return n, err
}

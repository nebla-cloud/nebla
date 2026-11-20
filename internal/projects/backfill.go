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

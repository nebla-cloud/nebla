package projects

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Hexeption/nebla/internal/ent"
	"github.com/Hexeption/nebla/internal/ent/project"
	"github.com/Hexeption/nebla/internal/ent/projectmember"
	"github.com/Hexeption/nebla/internal/ent/setting"
	"github.com/Hexeption/nebla/internal/ent/user"
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

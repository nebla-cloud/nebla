package projects

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"entgo.io/ent/dialect/sql"

	"github.com/nebla-cloud/nebla/internal/apierr"
	"github.com/nebla-cloud/nebla/internal/auth"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/project"
	"github.com/nebla-cloud/nebla/internal/ent/projectmember"
)

// AdminProject is a project with its first owner's email, usage and member count, for the admin list.
type AdminProject struct {
	ID          int64
	Name        string
	Quota       Quota
	OwnerEmail  string
	Usage       Usage
	MemberCount int
	Created     time.Time
}

// AdminProjectDetail is an AdminProject with its members.
type AdminProjectDetail struct {
	AdminProject
	Members []Member
}

// ListAll returns one page of every project ordered by id, plus the total count.
func (s *Service) ListAll(ctx context.Context, page, perPage int) ([]AdminProject, int, error) {
	total, err := s.db.Project.Query().Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	rows, err := s.db.Project.Query().Order(project.ByID()).Offset((page - 1) * perPage).Limit(perPage).All(ctx)
	if err != nil {
		return nil, 0, err
	}
	out, err := s.adminProjects(ctx, rows)
	if err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

// AdminGet returns any project with its members.
func (s *Service) AdminGet(ctx context.Context, projectID int64) (AdminProjectDetail, error) {
	p, err := s.db.Project.Get(ctx, projectID)
	if err != nil {
		return AdminProjectDetail{}, notFound(err)
	}
	aps, err := s.adminProjects(ctx, []*ent.Project{p})
	if err != nil {
		return AdminProjectDetail{}, err
	}
	ms, err := s.db.ProjectMember.Query().Where(projectmember.ProjectID(projectID)).
		WithUser().Order(projectmember.ByUserID()).All(ctx)
	if err != nil {
		return AdminProjectDetail{}, err
	}
	out := AdminProjectDetail{AdminProject: aps[0], Members: make([]Member, len(ms))}
	for i, m := range ms {
		out.Members[i] = toMember(m)
	}
	return out, nil
}

// AdminDelete removes any project; it fails with project_not_empty while servers or volumes exist.
func (s *Service) AdminDelete(ctx context.Context, projectID int64) error {
	return s.deleteProject(ctx, projectID)
}

func (s *Service) adminProjects(ctx context.Context, rows []*ent.Project) ([]AdminProject, error) {
	ids := make([]int64, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	owners, err := s.db.ProjectMember.Query().Where(projectmember.ProjectIDIn(ids...), projectmember.Role(RoleOwner)).
		WithUser().Order(projectmember.ByUserID(sql.OrderDesc())).All(ctx)
	if err != nil {
		return nil, err
	}
	email := map[int64]string{}
	for _, m := range owners {
		if m.Edges.User != nil {
			email[m.ProjectID] = m.Edges.User.Email // descending order leaves the lowest user id last
		}
	}
	members, err := memberCounts(ctx, s.db, ids, "")
	if err != nil {
		return nil, err
	}
	out := make([]AdminProject, len(rows))
	for i, r := range rows {
		out[i] = AdminProject{ID: r.ID, Name: r.Name, Quota: quotaOf(r), OwnerEmail: email[r.ID],
			MemberCount: members[r.ID], Created: r.CreatedAt}
		if out[i].Usage, err = s.opts.Usage(ctx, s.db, r.ID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// memberCounts counts members per project in one grouped query, only those with role when set.
func memberCounts(ctx context.Context, db *ent.Client, ids []int64, role string) (map[int64]int, error) {
	q := db.ProjectMember.Query().Where(projectmember.ProjectIDIn(ids...))
	if role != "" {
		q = q.Where(projectmember.Role(role))
	}
	var rows []struct {
		ProjectID int64 `json:"project_id"`
		Count     int   `json:"count"`
	}
	if err := q.GroupBy(projectmember.FieldProjectID).Aggregate(ent.Count()).Scan(ctx, &rows); err != nil {
		return nil, err
	}
	out := make(map[int64]int, len(rows))
	for _, r := range rows {
		out[r.ProjectID] = r.Count
	}
	return out, nil
}

// DeleteUser deletes another user; with takeOver the actor first becomes owner of projects only they own.
func (s *Service) DeleteUser(ctx context.Context, actorID, userID int64, takeOver bool) error {
	if actorID == userID {
		return auth.ErrSelfAdminAction
	}
	return withTx(ctx, s.db, func(tx *ent.Client) error {
		admins, err := auth.LockAdmins(ctx, tx)
		if err != nil {
			return err
		}
		if _, err := auth.LockUser(ctx, tx, userID); err != nil {
			return err
		}
		if err := auth.CheckLastAdmin(admins, userID); err != nil {
			return err
		}
		sole, err := soleOwned(ctx, tx, userID)
		if err != nil {
			return err
		}
		if len(sole) > 0 && !takeOver {
			return errSoleOwner(sole)
		}
		for _, p := range sole {
			n, err := tx.ProjectMember.Update().
				Where(projectmember.ProjectID(p.ID), projectmember.UserID(actorID)).SetRole(RoleOwner).Save(ctx)
			if err != nil {
				return err
			}
			if n == 0 {
				err = tx.ProjectMember.Create().SetProjectID(p.ID).SetUserID(actorID).SetRole(RoleOwner).Exec(ctx)
				if err != nil {
					return err
				}
			}
		}
		return tx.User.DeleteOneID(userID).Exec(ctx)
	})
}

// soleOwned locks the projects userID owns and returns those with no other owner, by id.
func soleOwned(ctx context.Context, tx *ent.Client, userID int64) ([]*ent.Project, error) {
	ps, err := tx.Project.Query().
		Where(project.HasMembersWith(projectmember.UserID(userID), projectmember.Role(RoleOwner))).
		Order(project.ByID()).ForUpdate().All(ctx)
	if err != nil || len(ps) == 0 {
		return nil, err
	}
	ids := make([]int64, len(ps))
	for i, p := range ps {
		ids[i] = p.ID
	}
	owners, err := memberCounts(ctx, tx, ids, RoleOwner)
	if err != nil {
		return nil, err
	}
	var out []*ent.Project
	for _, p := range ps {
		if owners[p.ID] == 1 {
			out = append(out, p)
		}
	}
	return out, nil
}

func errSoleOwner(ps []*ent.Project) error {
	names := make([]string, 0, 3)
	for _, p := range ps[:min(len(ps), 3)] {
		names = append(names, p.Name)
	}
	var list string
	switch {
	case len(ps) > 3:
		list = fmt.Sprintf("%s and %d more", strings.Join(names, ", "), len(ps)-3)
	case len(names) == 1:
		list = names[0]
	default:
		list = strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
	}
	return apierr.New(http.StatusConflict, "sole_owner", "They're the only owner of "+list+".")
}

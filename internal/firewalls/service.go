package firewalls

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/db"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/firewall"
	"github.com/nebla-cloud/nebla/internal/ent/firewallresource"
	"github.com/nebla-cloud/nebla/internal/ent/firewallrule"
	"github.com/nebla-cloud/nebla/internal/ent/project"
	"github.com/nebla-cloud/nebla/internal/ent/schema"
	"github.com/nebla-cloud/nebla/internal/ent/server"
	"github.com/nebla-cloud/nebla/internal/jobs"
	"github.com/nebla-cloud/nebla/internal/labels"
)

type Options struct {
	Ent  *ent.Client
	SQL  *sql.DB
	Jobs *jobs.Jobs
}

type Service struct{ o Options }

func New(o Options) *Service { return &Service{o: o} }

type Firewall struct {
	ID, ProjectID int64
	Name          string
	Labels        map[string]string
	Rules         []Rule
	AppliedTo     []Resource
	Created       time.Time
}

// Resource is what a firewall is applied to; MatchedServerIDs is set for selectors.
type Resource struct {
	Type             string
	ServerID         *int64
	Selector         *string
	MatchedServerIDs []int64
}

type ListFilter struct {
	LabelSelector, Name string
}

type CreateInput struct {
	Name    string
	Labels  map[string]string
	Rules   []RuleInput
	ApplyTo []ResourceInput
}

// UpdateInput changes only the non-nil fields.
type UpdateInput struct {
	Name   *string
	Labels *map[string]string
}

func notFound(err error) error {
	if ent.IsNotFound(err) {
		return errNotFound
	}
	return err
}

func writeErr(err error) error {
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.ConstraintName == "firewalls_project_name_key" {
		return errNameTaken
	}
	return notFound(err)
}

func (s *Service) Get(ctx context.Context, projectID, id int64) (Firewall, error) {
	row, err := s.o.Ent.Firewall.Query().Where(firewall.ID(id), firewall.ProjectID(projectID)).Only(ctx)
	if err != nil {
		return Firewall{}, notFound(err)
	}
	out, err := s.hydrate(ctx, projectID, []*ent.Firewall{row})
	if err != nil {
		return Firewall{}, err
	}
	return out[0], nil
}

// List filters in memory; a project holds at most MaxFirewalls.
func (s *Service) List(ctx context.Context, projectID int64, f ListFilter, page, perPage int) ([]Firewall, int, error) {
	var sel labels.Selector
	if f.LabelSelector != "" {
		var err error
		if sel, err = labels.Parse(f.LabelSelector); err != nil {
			return nil, 0, err
		}
	}
	q := s.o.Ent.Firewall.Query().Where(firewall.ProjectID(projectID))
	if f.Name != "" {
		q = q.Where(firewall.Name(f.Name))
	}
	rows, err := q.Order(firewall.ByID()).All(ctx)
	if err != nil {
		return nil, 0, err
	}
	rows = slices.DeleteFunc(rows, func(r *ent.Firewall) bool { return !sel.Matches(r.Labels) })
	total := len(rows)
	page, perPage = max(page, 1), max(perPage, 1)
	lo := min((page-1)*perPage, total)
	out, err := s.hydrate(ctx, projectID, rows[lo:min(lo+perPage, total)])
	return out, total, err
}

func (s *Service) hydrate(ctx context.Context, projectID int64, rows []*ent.Firewall) ([]Firewall, error) {
	ids := make([]int64, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	rules, err := s.o.Ent.FirewallRule.Query().Where(firewallrule.FirewallIDIn(ids...)).
		Order(firewallrule.ByFirewallID(), firewallrule.ByPosition()).All(ctx)
	if err != nil {
		return nil, err
	}
	res, err := s.o.Ent.FirewallResource.Query().Where(firewallresource.FirewallIDIn(ids...)).Order(firewallresource.ByID()).All(ctx)
	if err != nil {
		return nil, err
	}
	var srvs []*ent.Server
	if slices.ContainsFunc(res, func(r *ent.FirewallResource) bool { return r.Selector != nil }) {
		if srvs, err = liveServers(ctx, s.o.Ent, projectID); err != nil {
			return nil, err
		}
	}
	byID := map[int64]*Firewall{}
	out := make([]Firewall, len(rows))
	for i, r := range rows {
		out[i] = Firewall{ID: r.ID, ProjectID: r.ProjectID, Name: r.Name, Labels: r.Labels, Rules: []Rule{}, AppliedTo: []Resource{},
			Created: r.CreatedAt}
		if out[i].Labels == nil {
			out[i].Labels = map[string]string{}
		}
		byID[r.ID] = &out[i]
	}
	for _, r := range rules {
		rule, err := toRule(r)
		if err != nil {
			return nil, err
		}
		fw := byID[r.FirewallID]
		fw.Rules = append(fw.Rules, rule)
	}
	for _, r := range res {
		ar := Resource{Type: string(r.Type), ServerID: r.ServerID, Selector: r.Selector}
		if r.Selector != nil {
			sel, err := labels.Parse(*r.Selector)
			if err != nil {
				return nil, fmt.Errorf("firewall %d selector %q: %w", r.FirewallID, *r.Selector, err)
			}
			ar.MatchedServerIDs = []int64{}
			for _, sv := range srvs {
				if sel.Matches(sv.Labels) {
					ar.MatchedServerIDs = append(ar.MatchedServerIDs, sv.ID)
				}
			}
		}
		fw := byID[r.FirewallID]
		fw.AppliedTo = append(fw.AppliedTo, ar)
	}
	return out, nil
}

// liveServers are the project's servers that firewall changes apply to, ordered by id.
func liveServers(ctx context.Context, c *ent.Client, projectID int64) ([]*ent.Server, error) {
	return c.Server.Query().Where(server.ProjectID(projectID), server.StatusNEQ(server.StatusDeleting)).
		Order(server.ByID()).All(ctx)
}

func (s *Service) Create(ctx context.Context, projectID int64, in CreateInput) (Firewall, []actions.Action, error) {
	if err := validName(in.Name); err != nil {
		return Firewall{}, nil, err
	}
	if in.Labels == nil {
		in.Labels = map[string]string{}
	}
	if err := validLabels(in.Labels); err != nil {
		return Firewall{}, nil, err
	}
	rules, err := validRules(in.Rules)
	if err != nil {
		return Firewall{}, nil, err
	}
	res, err := validResources(in.ApplyTo)
	if err != nil {
		return Firewall{}, nil, err
	}
	var id int64
	var acts []actions.Action
	err = db.WithTx(ctx, s.o.SQL, func(tx *sql.Tx, etx *ent.Client) error {
		if _, err := etx.Project.Query().Where(project.ID(projectID)).ForUpdate().Only(ctx); err != nil {
			return notFound(err)
		}
		n, err := etx.Firewall.Query().Where(firewall.ProjectID(projectID)).Count(ctx)
		if err != nil {
			return err
		}
		if n >= MaxFirewalls {
			return limitExceeded(fmt.Sprintf("a project can have at most %d firewalls", MaxFirewalls))
		}
		if err := ownServers(ctx, etx, projectID, res, true); err != nil {
			return err
		}
		acts, err = s.change(ctx, tx, etx, projectID, true, func() (int64, error) {
			fw, err := etx.Firewall.Create().SetProjectID(projectID).SetName(in.Name).SetLabels(in.Labels).Save(ctx)
			if err != nil {
				return 0, writeErr(err)
			}
			id = fw.ID
			if err := insertRules(ctx, etx, id, rules); err != nil {
				return 0, err
			}
			return id, insertResources(ctx, etx, id, res)
		})
		return err
	})
	if err != nil {
		return Firewall{}, nil, err
	}
	fw, err := s.Get(ctx, projectID, id)
	return fw, acts, err
}

func (s *Service) Update(ctx context.Context, projectID, id int64, in UpdateInput) (Firewall, error) {
	if in.Name != nil {
		if err := validName(*in.Name); err != nil {
			return Firewall{}, err
		}
	}
	var lbls map[string]string
	if in.Labels != nil {
		if lbls = *in.Labels; lbls == nil {
			lbls = map[string]string{}
		}
		if err := validLabels(lbls); err != nil {
			return Firewall{}, err
		}
	}
	err := db.WithTx(ctx, s.o.SQL, func(_ *sql.Tx, etx *ent.Client) error {
		if err := lockFirewall(ctx, etx, projectID, id); err != nil {
			return err
		}
		u := etx.Firewall.UpdateOneID(id)
		if in.Name != nil {
			u.SetName(*in.Name)
		}
		if lbls != nil {
			u.SetLabels(lbls)
		}
		return writeErr(u.Exec(ctx))
	})
	if err != nil {
		return Firewall{}, err
	}
	return s.Get(ctx, projectID, id)
}

// Delete removes the firewall from every server and queues their applies.
func (s *Service) Delete(ctx context.Context, projectID, id int64) ([]actions.Action, error) {
	return s.locked(ctx, projectID, id, false, false, nil, func(etx *ent.Client) error {
		return etx.Firewall.DeleteOneID(id).Exec(ctx)
	})
}

// SetRules replaces the firewall's rules.
func (s *Service) SetRules(ctx context.Context, projectID, id int64, in []RuleInput) ([]actions.Action, error) {
	rules, err := validRules(in)
	if err != nil {
		return nil, err
	}
	return s.locked(ctx, projectID, id, false, false, nil, func(etx *ent.Client) error {
		if _, err := etx.FirewallRule.Delete().Where(firewallrule.FirewallID(id)).Exec(ctx); err != nil {
			return err
		}
		return insertRules(ctx, etx, id, rules)
	})
}

// ApplyTo attaches resources; ones already attached are left alone.
func (s *Service) ApplyTo(ctx context.Context, projectID, id int64, in []ResourceInput) ([]actions.Action, error) {
	res, err := validResources(in)
	if err != nil {
		return nil, err
	}
	return s.locked(ctx, projectID, id, true, true, res, func(etx *ent.Client) error {
		cur, err := etx.FirewallResource.Query().Where(firewallresource.FirewallID(id)).All(ctx)
		if err != nil {
			return err
		}
		res = slices.DeleteFunc(res, func(r ResourceInput) bool {
			return slices.ContainsFunc(cur, func(c *ent.FirewallResource) bool { return sameResource(c, r) })
		})
		return insertResources(ctx, etx, id, res)
	})
}

// RemoveFrom detaches resources; ones not attached are ignored.
func (s *Service) RemoveFrom(ctx context.Context, projectID, id int64, in []ResourceInput) ([]actions.Action, error) {
	res, err := validResources(in)
	if err != nil {
		return nil, err
	}
	return s.locked(ctx, projectID, id, true, false, res, func(etx *ent.Client) error {
		cur, err := etx.FirewallResource.Query().Where(firewallresource.FirewallID(id)).All(ctx)
		if err != nil {
			return err
		}
		var drop []int64
		for _, c := range cur {
			if slices.ContainsFunc(res, func(r ResourceInput) bool { return sameResource(c, r) }) {
				drop = append(drop, c.ID)
			}
		}
		if len(drop) == 0 {
			return nil
		}
		_, err = etx.FirewallResource.Delete().Where(firewallresource.IDIn(drop...)).Exec(ctx)
		return err
	})
}

func sameResource(c *ent.FirewallResource, r ResourceInput) bool {
	if r.Type == TypeServer {
		return c.ServerID != nil && *c.ServerID == *r.ServerID
	}
	return c.Selector != nil && *c.Selector == *r.Selector
}

func lockFirewall(ctx context.Context, etx *ent.Client, projectID, id int64) error {
	_, err := etx.Firewall.Query().Where(firewall.ID(id), firewall.ProjectID(projectID)).ForUpdate().Only(ctx)
	return notFound(err)
}

// ownServers refuses server resources outside the project with not_found, and deleting ones with locked when attaching.
func ownServers(ctx context.Context, etx *ent.Client, projectID int64, res []ResourceInput, attach bool) error {
	var ids []int64
	for _, r := range res {
		if r.ServerID != nil {
			ids = append(ids, *r.ServerID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := etx.Server.Query().Where(server.IDIn(ids...), server.ProjectID(projectID)).All(ctx)
	if err != nil {
		return err
	}
	if len(rows) != len(ids) {
		return errNotFound
	}
	if attach && slices.ContainsFunc(rows, func(r *ent.Server) bool { return r.Status == server.StatusDeleting }) {
		return errDeleting
	}
	return nil
}

// locked runs mutate under the firewall's row lock and fans out to the servers it affects.
func (s *Service) locked(ctx context.Context, projectID, id int64, onlyChanged, attach bool, res []ResourceInput,
	mutate func(etx *ent.Client) error) ([]actions.Action, error) {
	var acts []actions.Action
	err := db.WithTx(ctx, s.o.SQL, func(tx *sql.Tx, etx *ent.Client) error {
		if err := lockFirewall(ctx, etx, projectID, id); err != nil {
			return err
		}
		if err := ownServers(ctx, etx, projectID, res, attach); err != nil {
			return err
		}
		var err error
		acts, err = s.change(ctx, tx, etx, projectID, onlyChanged, func() (int64, error) { return id, mutate(etx) })
		return err
	})
	return acts, err
}

// change runs mutate, bumps servers the firewall covers before or after (onlyChanged: where that differs) and enforces MaxEffective.
func (s *Service) change(ctx context.Context, tx *sql.Tx, etx *ent.Client, projectID int64, onlyChanged bool,
	mutate func() (int64, error)) ([]actions.Action, error) {
	srvs, err := liveServers(ctx, etx, projectID)
	if err != nil {
		return nil, err
	}
	before, err := loadIndex(ctx, etx, projectID)
	if err != nil {
		return nil, err
	}
	id, err := mutate()
	if err != nil {
		return nil, err
	}
	after, err := loadIndex(ctx, etx, projectID)
	if err != nil {
		return nil, err
	}
	var affected []int64
	for _, sv := range srvs {
		b, a := before.has(sv.ID, sv.Labels, id), after.has(sv.ID, sv.Labels, id)
		if (onlyChanged && b != a) || (!onlyChanged && (b || a)) {
			affected = append(affected, sv.ID)
		}
	}
	acts, err := Bump(ctx, tx, etx, s.o.Jobs, projectID, affected, []int64{id})
	if err != nil || len(affected) == 0 {
		return acts, err
	}
	// Re-read under the server locks so concurrent changes to the same servers are counted.
	fresh, err := loadIndex(ctx, etx, projectID)
	if err != nil {
		return nil, err
	}
	rows, err := etx.Server.Query().Where(server.IDIn(affected...)).Order(server.ByID()).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		n := len(fresh.effective(r.ID, r.Labels, nil))
		if n > MaxEffective && n > len(before.effective(r.ID, r.Labels, nil)) {
			return nil, overLimit(r.Name)
		}
	}
	return acts, nil
}

func insertRules(ctx context.Context, etx *ent.Client, id int64, rules []RuleInput) error {
	if len(rules) == 0 {
		return nil
	}
	b := make([]*ent.FirewallRuleCreate, len(rules))
	for i, r := range rules {
		b[i] = etx.FirewallRule.Create().SetFirewallID(id).SetPosition(i).SetDirection(schema.Direction(r.Direction)).
			SetProtocol(firewallrule.Protocol(r.Protocol)).SetNillablePort(r.Port).SetIps(r.IPs).SetDescription(r.Description)
	}
	return etx.FirewallRule.CreateBulk(b...).Exec(ctx)
}

func insertResources(ctx context.Context, etx *ent.Client, id int64, res []ResourceInput) error {
	if len(res) == 0 {
		return nil
	}
	b := make([]*ent.FirewallResourceCreate, len(res))
	for i, r := range res {
		b[i] = etx.FirewallResource.Create().SetFirewallID(id).SetType(firewallresource.Type(r.Type)).
			SetNillableServerID(r.ServerID).SetNillableSelector(r.Selector)
	}
	return etx.FirewallResource.CreateBulk(b...).Exec(ctx)
}

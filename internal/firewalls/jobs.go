package firewalls

import (
	"context"
	"database/sql"
	"encoding/json"
	"slices"

	entsql "entgo.io/ent/dialect/sql"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/action"
	"github.com/nebla-cloud/nebla/internal/ent/firewall"
	"github.com/nebla-cloud/nebla/internal/ent/server"
	"github.com/nebla-cloud/nebla/internal/jobs"
	"github.com/riverqueue/river"
)

const (
	KindApply = "apply_firewalls"
	// ResourceType is the actions resource type for firewalls.
	ResourceType = "firewall"
	// MaxAttempts mirrors servers.MaxAttempts plus spare; servers imports this package, so it can't be shared.
	MaxAttempts = 5
	// CreateDone is servers' finished create step; servers asserts it matches.
	CreateDone = 8
)

// ApplyArgs is the apply_firewalls job; it brings the server's VM up to generation Gen.
type ApplyArgs struct {
	ActionID int64 `json:"action_id"`
	ServerID int64 `json:"server_id"`
	Gen      int64 `json:"gen"`
}

func (ApplyArgs) Kind() string { return KindApply }

func (ApplyArgs) InsertOpts() river.InsertOpts { return river.InsertOpts{MaxAttempts: MaxAttempts} }

// Bump locks the live servers ascending, raises their desired generation and queues one apply each.
func Bump(ctx context.Context, tx *sql.Tx, etx *ent.Client, j *jobs.Jobs, projectID int64, serverIDs []int64, causes []int64) ([]actions.Action, error) {
	ids := slices.Compact(slices.Sorted(slices.Values(serverIDs)))
	if len(ids) == 0 {
		return nil, nil
	}
	if j == nil {
		return nil, errNoJobs
	}
	q := etx.Server.Query().Where(server.IDIn(ids...), server.ProjectID(projectID), server.StatusNEQ(server.StatusDeleting),
		server.Not(server.And(server.StatusEQ(server.StatusError), server.CreateStepLT(CreateDone)))).
		Order(server.ByID()).Unique(false)
	// NO KEY UPDATE leaves the FK key-share locks of concurrent resource inserts compatible.
	q.Modify(func(s *entsql.Selector) { s.For(entsql.LockNoKeyUpdate) })
	rows, err := q.All(ctx)
	if err != nil {
		return nil, err
	}
	causes = slices.Compact(slices.Sorted(slices.Values(causes)))
	out := make([]actions.Action, 0, len(rows))
	for _, row := range rows {
		up, err := etx.Server.UpdateOneID(row.ID).AddFirewallGenDesired(1).Save(ctx)
		if err != nil {
			return nil, err
		}
		res := []actions.Resource{{ID: row.ID, Type: jobs.ServerResource}}
		for _, c := range causes {
			res = append(res, actions.Resource{ID: c, Type: ResourceType})
		}
		a, err := j.StartAction(ctx, tx, etx, projectID, KindApply, res, func(actionID int64) river.JobArgs {
			return ApplyArgs{ActionID: actionID, ServerID: row.ID, Gen: up.FirewallGenDesired}
		}, nil)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

// Actions lists the project's actions that firewall id caused; not_found when the firewall isn't the project's.
func (s *Service) Actions(ctx context.Context, projectID, id int64, f actions.ListFilter, page, perPage int) ([]actions.Action, int, error) {
	ok, err := s.o.Ent.Firewall.Query().Where(firewall.ID(id), firewall.ProjectID(projectID)).Exist(ctx)
	if err != nil {
		return nil, 0, err
	}
	if !ok {
		return nil, 0, errNotFound
	}
	doc, _ := json.Marshal([]actions.Resource{{ID: id, Type: ResourceType}})
	f.Where = append(f.Where, func(s *entsql.Selector) {
		s.Where(entsql.P(func(b *entsql.Builder) {
			b.Ident(s.C(action.FieldResources)).WriteString(" @> ").Arg(string(doc)).WriteString("::jsonb")
		}))
	})
	return actions.List(ctx, s.o.Ent, projectID, f, page, perPage)
}

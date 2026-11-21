package firewalls

import (
	"context"
	"database/sql"
	"slices"

	entsql "entgo.io/ent/dialect/sql"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/ent"
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

// Bump locks the project's servers ascending (skipping deleting and failed-create ones), raises each desired generation and starts one apply_firewalls Action per server.
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

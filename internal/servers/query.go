package servers

import (
	"context"
	"encoding/json"
	"strings"

	"entgo.io/ent/dialect/sql"
	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/action"
	"github.com/nebla-cloud/nebla/internal/ent/ipallocation"
	"github.com/nebla-cloud/nebla/internal/ent/predicate"
	"github.com/nebla-cloud/nebla/internal/ent/server"
	"github.com/nebla-cloud/nebla/internal/firewalls"
	"github.com/nebla-cloud/nebla/internal/labels"
	"github.com/nebla-cloud/nebla/internal/projects"
)

func labelsContain(k, v string) predicate.Server {
	doc, _ := json.Marshal(map[string]string{k: v})
	return func(s *sql.Selector) {
		s.Where(sql.P(func(b *sql.Builder) {
			b.Ident(s.C(server.FieldLabels)).WriteString(" @> ").Arg(string(doc)).WriteString("::jsonb")
		}))
	}
}

func labelsHave(k string) predicate.Server {
	return func(s *sql.Selector) {
		s.Where(sql.P(func(b *sql.Builder) {
			b.WriteString("jsonb_exists(").Ident(s.C(server.FieldLabels)).Comma().Arg(k).WriteString(")")
		}))
	}
}

// LabelSelector turns a label selector into predicates; blank means no filter.
func LabelSelector(sel string) ([]predicate.Server, error) {
	if strings.TrimSpace(sel) == "" {
		return nil, nil
	}
	parsed, err := labels.Parse(sel)
	if err != nil {
		return nil, err
	}
	out := make([]predicate.Server, 0, len(parsed))
	for _, t := range parsed {
		switch t.Op {
		case labels.Has:
			out = append(out, labelsHave(t.Key))
		case labels.Lacks:
			out = append(out, server.Not(labelsHave(t.Key)))
		case labels.Eq:
			out = append(out, labelsContain(t.Key, t.Value))
		case labels.Ne:
			out = append(out, server.Not(labelsContain(t.Key, t.Value)))
		}
	}
	return out, nil
}

// ActionsOf matches the project's actions whose resources include the server.
func ActionsOf(projectID, serverID int64) predicate.Action {
	doc, _ := json.Marshal([]actions.Resource{{ID: serverID, Type: ResourceType}})
	return action.And(action.ProjectID(projectID), func(s *sql.Selector) {
		s.Where(sql.P(func(b *sql.Builder) {
			b.Ident(s.C(action.FieldResources)).WriteString(" @> ").Arg(string(doc)).WriteString("::jsonb")
		}))
	})
}

// actionRunning leaves out firewall applies, which run alongside other server work.
var actionRunning = action.And(action.StatusEQ(action.StatusRunning), action.CommandNEQ(firewalls.KindApply))

// HasServers is the projects.Hooks.HasServers implementation.
func HasServers(ctx context.Context, c *ent.Client, projectID int64) (bool, error) {
	return c.Server.Query().Where(server.ProjectID(projectID)).Exist(ctx)
}

// LocationInUse is the catalog.Hooks.LocationInUse implementation.
func LocationInUse(ctx context.Context, c *ent.Client, locationID int64) (bool, error) {
	return c.Server.Query().Where(server.LocationID(locationID)).Exist(ctx)
}

// Usage is the projects.Hooks.Usage implementation: every server row counts with its snapshotted specs.
func Usage(ctx context.Context, c *ent.Client, projectID int64) (projects.Usage, error) {
	rows, err := c.Server.Query().Where(server.ProjectID(projectID)).
		Select(server.FieldID, server.FieldCores, server.FieldMemoryMB, server.FieldDiskGB).All(ctx)
	if err != nil {
		return projects.Usage{}, err
	}
	u := projects.Usage{Servers: len(rows)}
	ids := make([]int64, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
		u.VCPU += r.Cores
		u.MemoryMB += r.MemoryMB
		u.DiskGB += r.DiskGB
	}
	if len(ids) > 0 {
		if u.IPs, err = c.IPAllocation.Query().Where(ipallocation.ServerIDIn(ids...)).Count(ctx); err != nil {
			return projects.Usage{}, err
		}
	}
	return u, nil
}

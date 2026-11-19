package servers

import (
	"context"
	"encoding/json"
	"strings"

	"entgo.io/ent/dialect/sql"
	"github.com/Hexeption/nebla/internal/actions"
	"github.com/Hexeption/nebla/internal/ent"
	"github.com/Hexeption/nebla/internal/ent/action"
	"github.com/Hexeption/nebla/internal/ent/ipallocation"
	"github.com/Hexeption/nebla/internal/ent/predicate"
	"github.com/Hexeption/nebla/internal/ent/server"
	"github.com/Hexeption/nebla/internal/projects"
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

// LabelSelector parses comma-separated k=v, k!=v, k and !k terms into predicates.
func LabelSelector(sel string) ([]predicate.Server, error) {
	if strings.TrimSpace(sel) == "" {
		return nil, nil
	}
	var out []predicate.Server
	for term := range strings.SplitSeq(sel, ",") {
		term = strings.TrimSpace(term)
		key, val, op := term, "", ""
		switch {
		case strings.HasPrefix(term, "!"):
			key, op = term[1:], "!"
		case strings.Contains(term, "!="):
			key, val, _ = strings.Cut(term, "!=")
			op = "!="
		case strings.Contains(term, "="):
			key, val, _ = strings.Cut(term, "=")
			op = "="
		}
		key, val = strings.TrimSpace(key), strings.TrimSpace(val)
		if !labelKeyRE.MatchString(key) {
			return nil, invalid("label_selector: bad key in %q", term)
		}
		if op == "=" || op == "!=" {
			if !labelValueRE.MatchString(val) {
				return nil, invalid("label_selector: bad value in %q", term)
			}
		}
		switch op {
		case "":
			out = append(out, labelsHave(key))
		case "!":
			out = append(out, server.Not(labelsHave(key)))
		case "=":
			out = append(out, labelsContain(key, val))
		case "!=":
			out = append(out, server.Not(labelsContain(key, val)))
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

var actionRunning = action.StatusEQ(action.StatusRunning)

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

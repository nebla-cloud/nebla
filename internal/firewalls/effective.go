package firewalls

import (
	"cmp"
	"context"
	"fmt"
	"net/netip"
	"slices"

	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/firewall"
	"github.com/nebla-cloud/nebla/internal/ent/firewallresource"
	"github.com/nebla-cloud/nebla/internal/ent/firewallrule"
	"github.com/nebla-cloud/nebla/internal/ent/server"
	"github.com/nebla-cloud/nebla/internal/labels"
)

// ViaDirect is Applied.Via for a firewall attached to the server itself.
const ViaDirect = "direct"

// Applied is one firewall in effect on a server and why.
type Applied struct {
	FirewallID int64
	Name, Via  string
}

type selectorRes struct {
	firewallID int64
	sel        labels.Selector
}

// index is a project's firewall attachments, enough to compute any server's effective set.
type index struct {
	names     map[int64]string
	direct    map[int64][]int64
	selectors []selectorRes
}

func loadIndex(ctx context.Context, c *ent.Client, projectID int64) (*index, error) {
	fws, err := c.Firewall.Query().Where(firewall.ProjectID(projectID)).WithResources().All(ctx)
	if err != nil {
		return nil, err
	}
	x := &index{names: map[int64]string{}, direct: map[int64][]int64{}}
	for _, fw := range fws {
		x.names[fw.ID] = fw.Name
		for _, r := range fw.Edges.Resources {
			switch {
			case r.Type == firewallresource.TypeServer && r.ServerID != nil:
				x.direct[*r.ServerID] = append(x.direct[*r.ServerID], fw.ID)
			case r.Type == firewallresource.TypeLabelSelector && r.Selector != nil:
				sel, err := labels.Parse(*r.Selector)
				if err != nil {
					return nil, fmt.Errorf("firewall %d selector %q: %w", fw.ID, *r.Selector, err)
				}
				x.selectors = append(x.selectors, selectorRes{fw.ID, sel})
			}
		}
	}
	return x, nil
}

// effective lists the firewalls on a server with these labels, plus extraDirect, sorted by id; direct wins over a selector.
func (x *index) effective(serverID int64, lbls map[string]string, extraDirect []int64) []Applied {
	via := map[int64]string{}
	for _, id := range append(slices.Clone(x.direct[serverID]), extraDirect...) {
		via[id] = ViaDirect
	}
	for _, s := range x.selectors {
		if _, ok := via[s.firewallID]; !ok && s.sel.Matches(lbls) {
			via[s.firewallID] = s.sel.String()
		}
	}
	out := make([]Applied, 0, len(via))
	for id, v := range via {
		out = append(out, Applied{FirewallID: id, Name: x.names[id], Via: v})
	}
	slices.SortFunc(out, func(a, b Applied) int { return cmp.Compare(a.FirewallID, b.FirewallID) })
	return out
}

func (x *index) has(serverID int64, lbls map[string]string, firewallID int64) bool {
	return slices.ContainsFunc(x.effective(serverID, lbls, nil), func(a Applied) bool { return a.FirewallID == firewallID })
}

// Effective lists the firewalls in effect on a server, ordered by firewall id.
func Effective(ctx context.Context, c *ent.Client, serverID int64) ([]Applied, error) {
	row, err := c.Server.Get(ctx, serverID)
	if err != nil {
		return nil, notFound(err)
	}
	x, err := loadIndex(ctx, c, row.ProjectID)
	if err != nil {
		return nil, err
	}
	return x.effective(row.ID, row.Labels, nil), nil
}

// RulesFor returns a server's effective rules ordered by firewall id then position, and whether any firewall applies.
func RulesFor(ctx context.Context, c *ent.Client, serverID int64) ([]Rule, bool, error) {
	eff, err := Effective(ctx, c, serverID)
	if err != nil || len(eff) == 0 {
		return nil, false, err
	}
	ids := make([]int64, len(eff))
	for i, a := range eff {
		ids[i] = a.FirewallID
	}
	rows, err := c.FirewallRule.Query().Where(firewallrule.FirewallIDIn(ids...)).
		Order(firewallrule.ByFirewallID(), firewallrule.ByPosition()).All(ctx)
	if err != nil {
		return nil, false, err
	}
	out := make([]Rule, len(rows))
	for i, r := range rows {
		if out[i], err = toRule(r); err != nil {
			return nil, false, err
		}
	}
	return out, true, nil
}

func toRule(r *ent.FirewallRule) (Rule, error) {
	out := Rule{ID: r.ID, FirewallID: r.FirewallID, Direction: string(r.Direction), Protocol: string(r.Protocol),
		Port: r.Port, Description: r.Description, IPs: make([]netip.Prefix, len(r.Ips))}
	for i, s := range r.Ips {
		p, err := netip.ParsePrefix(s)
		if err != nil {
			return Rule{}, fmt.Errorf("firewall rule %d: %w", r.ID, err)
		}
		out.IPs[i] = p
	}
	return out, nil
}

func overLimit(name string) error {
	return limitExceeded(fmt.Sprintf("server %s would have more than %d firewalls", name, MaxEffective))
}

// CheckLimit refuses a server whose effective firewalls, with these labels and extraDirect added, would exceed MaxEffective.
func CheckLimit(ctx context.Context, c *ent.Client, projectID, serverID int64, lbls map[string]string, extraDirect []int64) error {
	row, err := c.Server.Query().Where(server.ID(serverID), server.ProjectID(projectID)).Only(ctx)
	if err != nil {
		return notFound(err)
	}
	x, err := loadIndex(ctx, c, projectID)
	if err != nil {
		return err
	}
	if len(x.effective(serverID, lbls, extraDirect)) > MaxEffective {
		return overLimit(row.Name)
	}
	return nil
}

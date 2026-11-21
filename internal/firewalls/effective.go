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
	rows, err := c.FirewallRule.Query().Where(firewallrule.FirewallIDIn(firewallIDs(eff)...)).
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

func overRules(name string) error {
	return limitExceeded(fmt.Sprintf("server %s would have more than %d firewall rules", name, MaxRenderedRules))
}

// ruleCounts maps each firewall in ids to the number of Proxmox rules its rules render to.
func ruleCounts(ctx context.Context, c *ent.Client, ids []int64) (map[int64]int, error) {
	out := map[int64]int{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := c.FirewallRule.Query().Where(firewallrule.FirewallIDIn(ids...)).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		rule, err := toRule(r)
		if err != nil {
			return nil, err
		}
		out[r.FirewallID] += len(renderRule(rule))
	}
	return out, nil
}

func firewallIDs(eff []Applied) []int64 {
	out := make([]int64, len(eff))
	for i, a := range eff {
		out[i] = a.FirewallID
	}
	return out
}

func rendered(eff []Applied, counts map[int64]int) int {
	n := 0
	for _, a := range eff {
		n += counts[a.FirewallID]
	}
	return n
}

// CheckLimit refuses a server over MaxEffective firewalls or MaxRenderedRules rules with these labels and extraDirect.
func CheckLimit(ctx context.Context, c *ent.Client, projectID, serverID int64, lbls map[string]string, extraDirect []int64) error {
	row, err := c.Server.Query().Where(server.ID(serverID), server.ProjectID(projectID)).Only(ctx)
	if err != nil {
		return notFound(err)
	}
	x, err := loadIndex(ctx, c, projectID)
	if err != nil {
		return err
	}
	eff := x.effective(serverID, lbls, extraDirect)
	if len(eff) > MaxEffective {
		return overLimit(row.Name)
	}
	counts, err := ruleCounts(ctx, c, firewallIDs(eff))
	if err != nil {
		return err
	}
	if rendered(eff, counts) > MaxRenderedRules {
		return overRules(row.Name)
	}
	return nil
}

// Attachments is a snapshot of a project's firewall attachments.
type Attachments struct{ x *index }

// LoadAttachments reads a project's firewall attachments.
func LoadAttachments(ctx context.Context, c *ent.Client, projectID int64) (Attachments, error) {
	x, err := loadIndex(ctx, c, projectID)
	return Attachments{x}, err
}

// Effective lists the firewalls in effect on a server with these labels, ordered by firewall id.
func (a Attachments) Effective(serverID int64, lbls map[string]string) []Applied {
	return a.x.effective(serverID, lbls, nil)
}

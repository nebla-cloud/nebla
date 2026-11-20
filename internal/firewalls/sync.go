package firewalls

import (
	"context"
	"fmt"
	"slices"

	"github.com/nebla-cloud/nebla/internal/pve"
)

// Sync applies d to a VM so that a protected VM is never briefly open.
func Sync(ctx context.Context, pc pve.Client, node string, vmid int, d Desired) error {
	cur, err := pc.FirewallRules(ctx, node, vmid)
	if err != nil {
		return fmt.Errorf("read firewall rules: %w", err)
	}
	missing, stale := diff(cur, d)
	if d.Options["policy_in"] == "DROP" {
		n := len(cur)
		for _, r := range missing {
			r.Pos = n
			if err := pc.AddFirewallRule(ctx, node, vmid, r); err != nil {
				return fmt.Errorf("add firewall rule: %w", err)
			}
			n++
		}
		if err := deleteRules(ctx, pc, node, vmid, stale); err != nil {
			return err
		}
		return setOptions(ctx, pc, node, vmid, d.Options)
	}
	if err := setOptions(ctx, pc, node, vmid, d.Options); err != nil {
		return err
	}
	return deleteRules(ctx, pc, node, vmid, stale)
}

// deleteRules removes rules highest position first so lower positions stay valid.
func deleteRules(ctx context.Context, pc pve.Client, node string, vmid int, pos []int) error {
	slices.Sort(pos)
	for _, p := range slices.Backward(pos) {
		if err := pc.DeleteFirewallRule(ctx, node, vmid, p); err != nil {
			return fmt.Errorf("delete firewall rule: %w", err)
		}
	}
	return nil
}

func setOptions(ctx context.Context, pc pve.Client, node string, vmid int, want map[string]string) error {
	have, err := pc.FirewallOptions(ctx, node, vmid)
	if err != nil {
		return fmt.Errorf("read firewall options: %w", err)
	}
	if optionsMatch(have, want) {
		return nil
	}
	if err := pc.SetFirewallOptions(ctx, node, vmid, want); err != nil {
		return fmt.Errorf("set firewall options: %w", err)
	}
	return nil
}

// InSync reports whether live rules and options already match d.
func InSync(cur []pve.FirewallRule, curOpts map[string]string, d Desired) bool {
	missing, stale := diff(cur, d)
	return len(missing) == 0 && len(stale) == 0 && optionsMatch(curOpts, d.Options)
}

func optionsMatch(have, want map[string]string) bool {
	for k, v := range want {
		if have[k] != v {
			return false
		}
	}
	return true
}

// same compares every rendered field, ignoring position.
func same(a, b pve.FirewallRule) bool {
	a.Pos, b.Pos = 0, 0
	return a == b
}

// diff returns desired rules not live and positions of live rules not desired.
func diff(cur []pve.FirewallRule, d Desired) (missing []pve.FirewallRule, stale []int) {
	want := map[string]pve.FirewallRule{}
	for _, r := range d.Rules {
		want[r.Comment] = r
	}
	have := map[string]bool{}
	for _, r := range cur {
		if w, ok := want[r.Comment]; ok && !have[r.Comment] && same(r, w) {
			have[r.Comment] = true
			continue
		}
		stale = append(stale, r.Pos)
	}
	for _, r := range d.Rules {
		if !have[r.Comment] {
			missing = append(missing, r)
		}
	}
	return missing, stale
}

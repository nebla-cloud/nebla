package firewalls

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/nebla-cloud/nebla/internal/pve"
)

// Sync applies d to a VM so that a protected VM is never briefly open, then verifies it.
func Sync(ctx context.Context, pc pve.Client, node string, vmid int, d Desired) error {
	cur, err := readRules(ctx, pc, node, vmid)
	if err != nil {
		return err
	}
	missing, stale := diff(cur, d)
	if d.Options["policy_in"] == "DROP" {
		for _, r := range missing {
			if err := pc.AddFirewallRule(ctx, node, vmid, r); err != nil {
				return fmt.Errorf("add firewall rule: %w", err)
			}
		}
		// Adds shift positions (PVE prepends), so find stale rules again.
		if len(missing) > 0 {
			if cur, err = readRules(ctx, pc, node, vmid); err != nil {
				return err
			}
			if missing, stale = diff(cur, d); len(missing) > 0 {
				return fmt.Errorf("firewall rule %s not in place after adding it", missing[0].Comment)
			}
		}
		if err := deleteRules(ctx, pc, node, vmid, stale); err != nil {
			return err
		}
		if err := setOptions(ctx, pc, node, vmid, d.Options); err != nil {
			return err
		}
	} else {
		if err := setOptions(ctx, pc, node, vmid, d.Options); err != nil {
			return err
		}
		if err := deleteRules(ctx, pc, node, vmid, stale); err != nil {
			return err
		}
	}
	return verify(ctx, pc, node, vmid, d)
}

// verify re-reads the VM firewall and errors if it still differs from d.
func verify(ctx context.Context, pc pve.Client, node string, vmid int, d Desired) error {
	cur, err := readRules(ctx, pc, node, vmid)
	if err != nil {
		return err
	}
	opts, err := pc.FirewallOptions(ctx, node, vmid)
	if err != nil {
		return fmt.Errorf("read firewall options: %w", err)
	}
	if drift := Drift(cur, opts, d); drift != "" {
		return fmt.Errorf("firewall differs after sync: %s", drift)
	}
	return nil
}

func readRules(ctx context.Context, pc pve.Client, node string, vmid int) ([]pve.FirewallRule, error) {
	cur, err := pc.FirewallRules(ctx, node, vmid)
	if err != nil {
		return nil, fmt.Errorf("read firewall rules: %w", err)
	}
	return cur, nil
}

// deleteRules removes rules highest position first so lower positions stay valid.
func deleteRules(ctx context.Context, pc pve.Client, node string, vmid int, rules []pve.FirewallRule) error {
	pos := make([]int, len(rules))
	for i, r := range rules {
		pos[i] = r.Pos
	}
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

// Drift describes the first difference between live rules and options and d; "" when in sync.
func Drift(cur []pve.FirewallRule, curOpts map[string]string, d Desired) string {
	for _, k := range slices.Sorted(maps.Keys(d.Options)) {
		if curOpts[k] != d.Options[k] {
			return fmt.Sprintf("option %s is %q, want %q", k, curOpts[k], d.Options[k])
		}
	}
	missing, stale := diff(cur, d)
	if len(missing) > 0 {
		return "rule missing: " + missing[0].Comment
	}
	if len(stale) > 0 {
		return fmt.Sprintf("unexpected rule at pos %d (%q)", stale[0].Pos, stale[0].Comment)
	}
	return ""
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

// diff returns desired rules not live and live rules not desired.
func diff(cur []pve.FirewallRule, d Desired) (missing, stale []pve.FirewallRule) {
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
		stale = append(stale, r)
	}
	for _, r := range d.Rules {
		if !have[r.Comment] {
			missing = append(missing, r)
		}
	}
	return missing, stale
}

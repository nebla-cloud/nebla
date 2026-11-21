package firewalls

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/nebla-cloud/nebla/internal/pve"
)

// ErrStoredDiffers means Proxmox stored a rule differently from what was sent; retrying will not help.
var ErrStoredDiffers = errors.New("proxmox stored a firewall rule differently")

// Sync applies d to a VM so that a protected VM is never briefly open, then verifies it.
func Sync(ctx context.Context, pc pve.Client, node string, vmid int, d Desired) error {
	cur, err := readRules(ctx, pc, node, vmid)
	if err != nil {
		return err
	}
	missing, _ := diff(cur, d)
	all := func(pve.FirewallRule) bool { return true }
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
			if missing, _ = diff(cur, d); len(missing) > 0 {
				return storedDiffers(ctx, pc, node, vmid, d, cur, missing[0])
			}
		}
		if err := deleteStale(ctx, pc, node, vmid, d, cur, all); err != nil {
			return err
		}
		if err := setOptions(ctx, pc, node, vmid, d.Options); err != nil {
			return err
		}
	} else {
		if err := setOptions(ctx, pc, node, vmid, d.Options); err != nil {
			return err
		}
		if err := deleteStale(ctx, pc, node, vmid, d, cur, all); err != nil {
			return err
		}
	}
	return verify(ctx, pc, node, vmid, d)
}

// storedDiffers removes our mismatched copies, so retries cannot pile them up, and fails for good.
func storedDiffers(ctx context.Context, pc pve.Client, node string, vmid int, d Desired, cur []pve.FirewallRule, r pve.FirewallRule) error {
	why := ruleDrift(cur, r)
	ours := map[string]bool{}
	for _, w := range d.Rules {
		ours[w.Comment] = true
	}
	// Under DROP this only removes ACCEPTs, so nothing opens.
	if err := deleteStale(ctx, pc, node, vmid, d, cur, func(x pve.FirewallRule) bool { return ours[x.Comment] }); err != nil {
		return fmt.Errorf("%w: %s (cleanup: %w)", ErrStoredDiffers, why, err)
	}
	return fmt.Errorf("%w: %s", ErrStoredDiffers, why)
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

// deleteStale deletes stale rules matching only, highest position first, re-reading before each delete.
func deleteStale(ctx context.Context, pc pve.Client, node string, vmid int, d Desired, cur []pve.FirewallRule, only func(pve.FirewallRule) bool) error {
	for range len(cur) + 1 {
		_, stale := diff(cur, d)
		stale = slices.DeleteFunc(stale, func(r pve.FirewallRule) bool { return !only(r) })
		if len(stale) == 0 {
			return nil
		}
		top := slices.MaxFunc(stale, func(a, b pve.FirewallRule) int { return a.Pos - b.Pos })
		// The digest turns a concurrent edit into an error instead of a wrong-position delete.
		if err := pc.DeleteFirewallRule(ctx, node, vmid, top.Pos, top.Digest); err != nil {
			return fmt.Errorf("delete firewall rule: %w", err)
		}
		var err error
		if cur, err = readRules(ctx, pc, node, vmid); err != nil {
			return err
		}
	}
	return errors.New("stale firewall rules keep coming back")
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
		return ruleDrift(cur, missing[0])
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

// ruleDrift names the first field where the live rule carrying r's comment differs from r.
func ruleDrift(cur []pve.FirewallRule, r pve.FirewallRule) string {
	i := slices.IndexFunc(cur, func(x pve.FirewallRule) bool { return x.Comment == r.Comment })
	if i < 0 {
		return "rule missing: " + r.Comment
	}
	x := cur[i]
	for _, f := range []struct{ name, have, want string }{
		{"type", x.Type, r.Type}, {"action", x.Action, r.Action}, {"iface", x.Iface, r.Iface}, {"proto", x.Proto, r.Proto},
		{"dport", x.Dport, r.Dport}, {"source", x.Source, r.Source}, {"dest", x.Dest, r.Dest},
		{"enable", fmt.Sprint(x.Enable), fmt.Sprint(r.Enable)},
	} {
		if f.have != f.want {
			return fmt.Sprintf("rule %s: %s is %q, want %q", r.Comment, f.name, f.have, f.want)
		}
	}
	return "rule differs: " + r.Comment
}

// same compares every rendered field, ignoring position.
func same(a, b pve.FirewallRule) bool {
	a.Pos, b.Pos, a.Digest, b.Digest = 0, 0, "", ""
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

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
	want := map[string]bool{}
	for _, r := range d.Rules {
		want[r.Comment] = true
	}
	have := map[string]bool{}
	var stale []int
	for i, r := range cur {
		if want[r.Comment] && !have[r.Comment] {
			have[r.Comment] = true
			continue
		}
		stale = append(stale, i)
	}
	if d.Options["policy_in"] == "DROP" {
		n := len(cur)
		for _, r := range d.Rules {
			if have[r.Comment] {
				continue
			}
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
	for k, v := range want {
		if have[k] != v {
			if err := pc.SetFirewallOptions(ctx, node, vmid, want); err != nil {
				return fmt.Errorf("set firewall options: %w", err)
			}
			return nil
		}
	}
	return nil
}

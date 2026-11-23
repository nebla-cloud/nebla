// Copyright (C) 2026 The Nebla contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package firewalls

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/nebla-cloud/nebla/internal/pve"
)

// ErrStoredDiffers means Proxmox stored a rule differently from what was sent; retrying will not help.
var ErrStoredDiffers = errors.New("proxmox stored a firewall rule differently")

// ErrRuleGone means a rule vanished right after it was added, likely a concurrent edit; retry.
var ErrRuleGone = errors.New("firewall rule missing right after adding it")

// Sync applies d to a VM so that a protected VM is never briefly open, then verifies it.
func Sync(ctx context.Context, pc pve.Client, node string, vmid int, d Desired) error {
	cur, err := readRules(ctx, pc, node, vmid)
	if err != nil {
		return err
	}
	missing, stale := diff(cur, d)
	drop := d.Options["policy_in"] == "DROP"
	if !drop {
		if err := setOptions(ctx, pc, node, vmid, d.Options); err != nil {
			return err
		}
		// Setting options changes the config digest, so re-read before writing rules.
		if len(missing) > 0 || len(stale) > 0 {
			if cur, err = readRules(ctx, pc, node, vmid); err != nil {
				return err
			}
		}
	}
	if cur, err = addMissing(ctx, pc, node, vmid, d, cur, missing); err != nil {
		return err
	}
	if cur, err = keepSystemOnTop(ctx, pc, node, vmid, d, cur); err != nil {
		return err
	}
	if err := deleteStale(ctx, pc, node, vmid, d, cur); err != nil {
		return err
	}
	if drop {
		if err := setOptions(ctx, pc, node, vmid, d.Options); err != nil {
			return err
		}
	}
	return verify(ctx, pc, node, vmid, d)
}

func isSystem(r pve.FirewallRule) bool { return strings.HasPrefix(r.Comment, SystemPrefix) }

// addMissing adds the rules, system ones last and in reverse so PVE's prepend leaves them in order on top.
func addMissing(ctx context.Context, pc pve.Client, node string, vmid int, d Desired, cur, missing []pve.FirewallRule) ([]pve.FirewallRule, error) {
	if len(missing) == 0 {
		return cur, nil
	}
	var user, sys []pve.FirewallRule
	for _, r := range missing {
		if isSystem(r) {
			sys = append(sys, r)
		} else {
			user = append(user, r)
		}
	}
	slices.Reverse(sys)
	for _, r := range append(user, sys...) {
		if err := pc.AddFirewallRule(ctx, node, vmid, r); err != nil {
			return nil, fmt.Errorf("add firewall rule: %w", err)
		}
	}
	// Adds shift positions (PVE prepends), so read again.
	cur, err := readRules(ctx, pc, node, vmid)
	if err != nil {
		return nil, err
	}
	if still, _ := diff(cur, d); len(still) > 0 {
		return nil, storedDiffers(ctx, pc, node, vmid, d, cur, still[0])
	}
	return cur, nil
}

// systemOnTop reports whether every system rule sits within the first n positions.
func systemOnTop(cur []pve.FirewallRule, n int) bool {
	for _, r := range cur {
		if isSystem(r) && r.Pos >= n {
			return false
		}
	}
	return true
}

// countSystem counts the system rules in rs.
func countSystem(rs []pve.FirewallRule) int {
	n := 0
	for _, r := range rs {
		if isSystem(r) {
			n++
		}
	}
	return n
}

// keepSystemOnTop re-adds the desired system rules on top, then deletes every system rule below them.
// The guard is never absent: PVE prepends, so the fresh copies land first.
func keepSystemOnTop(ctx context.Context, pc pve.Client, node string, vmid int, d Desired, cur []pve.FirewallRule) ([]pve.FirewallRule, error) {
	var sys []pve.FirewallRule
	for _, r := range d.Rules {
		if isSystem(r) {
			sys = append(sys, r)
		}
	}
	k := len(sys)
	if systemOnTop(cur, k) {
		return cur, nil
	}
	for _, r := range slices.Backward(sys) {
		if err := pc.AddFirewallRule(ctx, node, vmid, r); err != nil {
			return nil, fmt.Errorf("add firewall rule: %w", err)
		}
	}
	var err error
	if cur, err = readRules(ctx, pc, node, vmid); err != nil {
		return nil, err
	}
	for range len(cur) + 1 {
		var top *pve.FirewallRule
		for i, r := range cur {
			if isSystem(r) && r.Pos >= k && (top == nil || r.Pos > top.Pos) {
				top = &cur[i]
			}
		}
		if top == nil {
			return cur, nil
		}
		if err := pc.DeleteFirewallRule(ctx, node, vmid, top.Pos, top.Digest); err != nil {
			return nil, fmt.Errorf("delete firewall rule: %w", err)
		}
		if cur, err = readRules(ctx, pc, node, vmid); err != nil {
			return nil, err
		}
	}
	return nil, errors.New("system firewall rules are not on top after re-adding them")
}

// storedDiffers deletes all stale rules, then fails for good because Proxmox kept a rule differently.
func storedDiffers(ctx context.Context, pc pve.Client, node string, vmid int, d Desired, cur []pve.FirewallRule, r pve.FirewallRule) error {
	if !slices.ContainsFunc(cur, func(x pve.FirewallRule) bool { return x.Comment == r.Comment }) {
		return fmt.Errorf("%w: %s", ErrRuleGone, r.Comment)
	}
	why := ruleDrift(cur, r)
	// Deleting stale rules only narrows access; a conflict here is retried.
	if err := deleteStale(ctx, pc, node, vmid, d, cur); err != nil {
		return fmt.Errorf("clean up after %s: %w", why, err)
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

// deleteStale deletes stale rules highest position first, re-reading before each delete.
func deleteStale(ctx context.Context, pc pve.Client, node string, vmid int, d Desired, cur []pve.FirewallRule) error {
	for range len(cur) + 1 {
		_, stale := diff(cur, d)
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
	if n := countSystem(d.Rules); !systemOnTop(cur, n) {
		return "system rules are not on top"
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

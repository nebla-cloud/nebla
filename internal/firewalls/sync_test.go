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
	"net/netip"
	"slices"
	"strings"
	"testing"

	"github.com/nebla-cloud/nebla/internal/pve"
	"github.com/nebla-cloud/nebla/internal/pve/fake"
)

const node, vmid = "pve1", 100

func setup(t *testing.T) (*fake.Server, pve.Client) {
	t.Helper()
	s := fake.New(t)
	s.AddNode(node, 4, 8192)
	s.AddVM(node, vmid, "web1")
	s.RequireRuleDigest()
	return s, pve.New(s.Conn())
}

func comments(rs []pve.FirewallRule) map[string]bool {
	m := map[string]bool{}
	for _, r := range rs {
		m[r.Comment] = true
	}
	return m
}

func covers(have map[string]bool, want []pve.FirewallRule) bool {
	for _, r := range want {
		if !have[r.Comment] {
			return false
		}
	}
	return true
}

func converged(t *testing.T, s *fake.Server, d Desired) {
	t.Helper()
	v, _ := s.VM(vmid)
	for k, want := range d.Options {
		if v.Firewall[k] != want {
			t.Errorf("option %s = %q, want %q", k, v.Firewall[k], want)
		}
	}
	// Rule order does not matter for an allow-only list.
	if len(v.Rules) != len(d.Rules) {
		t.Fatalf("rules = %+v, want %+v", v.Rules, d.Rules)
	}
	for _, w := range d.Rules {
		if !slices.ContainsFunc(v.Rules, func(r pve.FirewallRule) bool { r.Pos = w.Pos; return r == w }) {
			t.Errorf("rule %+v missing from %+v", w, v.Rules)
		}
	}
}

func TestSyncNeverOpens(t *testing.T) {
	s, pc := setup(t)
	ctx := context.Background()
	a := Render([]Rule{{ID: 1, FirewallID: 1, Direction: "in", Protocol: "tcp", Port: ptr("22"), IPs: pfx("0.0.0.0/0", "::/0")}}, true, System{})
	b := Render([]Rule{
		{ID: 2, FirewallID: 1, Direction: "in", Protocol: "tcp", Port: ptr("443"), IPs: pfx("0.0.0.0/0")},
		{ID: 3, FirewallID: 2, Direction: "out", Protocol: "udp", Port: ptr("53"), IPs: pfx("192.0.2.1/32")},
	}, true, System{})
	none := Render(nil, false, System{})

	var old, next Desired
	var wasDrop bool
	var writes int
	s.OnFirewallWrite(func() {
		writes++
		v, _ := s.VM(vmid)
		have := comments(v.Rules)
		switch v.Firewall["policy_in"] {
		case "DROP":
			if !(wasDrop && covers(have, old.Rules)) && !(next.Options["policy_in"] == "DROP" && covers(have, next.Rules)) {
				t.Errorf("DROP with neither old nor new rules in place: %+v", v.Rules)
			}
		default:
			if wasDrop && next.Options["policy_in"] != "ACCEPT" {
				t.Errorf("protected VM opened mid-sync: %+v", v.Firewall)
			}
		}
		if v.Firewall["policy_out"] == "DROP" && !(old.Options["policy_out"] == "DROP" && covers(have, old.Rules)) && !covers(have, next.Rules) {
			t.Errorf("outbound DROP without its rules: %+v", v.Rules)
		}
	})
	old = none
	for _, d := range []Desired{a, b, none} {
		v, _ := s.VM(vmid)
		wasDrop = v.Firewall["policy_in"] == "DROP"
		next, writes = d, 0
		if err := Sync(ctx, pc, node, vmid, d); err != nil {
			t.Fatal(err)
		}
		if writes == 0 {
			t.Fatal("sync made no writes")
		}
		converged(t, s, d)
		old = d
	}
}

func TestSyncIdempotent(t *testing.T) {
	s, pc := setup(t)
	ctx := context.Background()
	d := Render([]Rule{{ID: 1, FirewallID: 1, Direction: "in", Protocol: "tcp", Port: ptr("22"), IPs: pfx("0.0.0.0/0", "::/0")}}, true, System{})
	if err := Sync(ctx, pc, node, vmid, d); err != nil {
		t.Fatal(err)
	}
	writes := 0
	s.OnFirewallWrite(func() { writes++ })
	if err := Sync(ctx, pc, node, vmid, d); err != nil {
		t.Fatal(err)
	}
	if writes != 0 {
		t.Fatalf("second sync made %d writes", writes)
	}
}

func TestSyncRemovesForeignRules(t *testing.T) {
	s, pc := setup(t)
	ctx := context.Background()
	if err := pc.AddFirewallRule(ctx, node, vmid, pve.FirewallRule{Type: "in", Action: "ACCEPT", Proto: "tcp", Dport: "3306", Enable: true, Comment: "mine"}); err != nil {
		t.Fatal(err)
	}
	if err := pc.AddFirewallRule(ctx, node, vmid, pve.FirewallRule{Type: "in", Action: "ACCEPT", Proto: "tcp", Dport: "25", Enable: true}); err != nil {
		t.Fatal(err)
	}
	d := Render([]Rule{{ID: 1, FirewallID: 1, Direction: "in", Protocol: "tcp", Port: ptr("22"), IPs: pfx("0.0.0.0/0")}}, true, System{})
	if err := Sync(ctx, pc, node, vmid, d); err != nil {
		t.Fatal(err)
	}
	converged(t, s, d)
	v, _ := s.VM(vmid)
	for _, r := range v.Rules {
		if !strings.HasPrefix(r.Comment, "nebla:") {
			t.Errorf("foreign rule kept: %+v", r)
		}
	}
}

func TestSyncResumesAfterFailure(t *testing.T) {
	s, pc := setup(t)
	ctx := context.Background()
	a := Render([]Rule{{ID: 1, FirewallID: 1, Direction: "in", Protocol: "tcp", Port: ptr("22"), IPs: pfx("0.0.0.0/0", "::/0")}}, true, System{})
	if err := Sync(ctx, pc, node, vmid, a); err != nil {
		t.Fatal(err)
	}
	b := Render([]Rule{{ID: 2, FirewallID: 1, Direction: "in", Protocol: "tcp", Port: ptr("443"), IPs: pfx("0.0.0.0/0", "::/0")}}, true, System{})
	s.FailNextRuleWrite()
	if err := Sync(ctx, pc, node, vmid, b); err == nil {
		t.Fatal("want an error from the failed rule write")
	}
	if v, _ := s.VM(vmid); v.Firewall["policy_in"] != "DROP" {
		t.Fatalf("half-finished sync opened the VM: %v", v.Firewall)
	}
	if err := Sync(ctx, pc, node, vmid, b); err != nil {
		t.Fatal(err)
	}
	converged(t, s, b)
}

func TestSyncRepairsEditedRule(t *testing.T) {
	s, pc := setup(t)
	ctx := context.Background()
	a := Render([]Rule{
		{ID: 1, FirewallID: 1, Direction: "in", Protocol: "tcp", Port: ptr("22"), IPs: pfx("192.0.2.0/24")},
		{ID: 2, FirewallID: 1, Direction: "in", Protocol: "tcp", Port: ptr("443"), IPs: pfx("0.0.0.0/0")},
	}, true, System{})
	if err := Sync(ctx, pc, node, vmid, a); err != nil {
		t.Fatal(err)
	}
	// Simulate a hand edit that widens rule 0 but keeps its comment.
	edited := a.Rules[0]
	edited.Source = "0.0.0.0/0"
	cur, _ := pc.FirewallRules(ctx, node, vmid)
	if err := pc.DeleteFirewallRule(ctx, node, vmid, 0, cur[0].Digest); err != nil {
		t.Fatal(err)
	}
	if err := pc.AddFirewallRule(ctx, node, vmid, edited); err != nil {
		t.Fatal(err)
	}
	cur, _ = pc.FirewallRules(ctx, node, vmid)
	opts, _ := pc.FirewallOptions(ctx, node, vmid)
	if InSync(cur, opts, a) {
		t.Fatal("InSync missed an edited rule")
	}
	s.OnFirewallWrite(func() {
		v, _ := s.VM(vmid)
		if v.Firewall["policy_in"] != "DROP" {
			t.Errorf("repair opened the VM: %v", v.Firewall)
		}
		have := map[string]int{}
		for _, r := range v.Rules {
			have[r.Comment]++
		}
		for _, r := range a.Rules {
			if have[r.Comment] == 0 {
				t.Errorf("rule %s missing mid-repair", r.Comment)
			}
		}
	})
	if err := Sync(ctx, pc, node, vmid, a); err != nil {
		t.Fatal(err)
	}
	s.OnFirewallWrite(nil)
	v, _ := s.VM(vmid)
	if len(v.Rules) != 2 || !slices.ContainsFunc(v.Rules, func(r pve.FirewallRule) bool { return r == a.Rules[0] }) {
		t.Fatalf("edited rule not repaired: %+v", v.Rules)
	}
	cur, _ = pc.FirewallRules(ctx, node, vmid)
	opts, _ = pc.FirewallOptions(ctx, node, vmid)
	if !InSync(cur, opts, a) {
		t.Fatal("InSync false after repair")
	}
}

func TestInSync(t *testing.T) {
	d := Render([]Rule{{ID: 1, FirewallID: 1, Direction: "in", Protocol: "tcp", Port: ptr("22"), IPs: pfx("0.0.0.0/0", "::/0")}}, true, System{})
	live := func() []pve.FirewallRule {
		out := slices.Clone(d.Rules)
		for i := range out {
			out[i].Pos = i
		}
		return out
	}
	opts := maps.Clone(d.Options)
	if !InSync(live(), opts, d) {
		t.Fatal("identical state reported out of sync")
	}
	mut := map[string]func(rs []pve.FirewallRule, o map[string]string) ([]pve.FirewallRule, map[string]string){
		"source": func(rs []pve.FirewallRule, o map[string]string) ([]pve.FirewallRule, map[string]string) {
			rs[0].Source = "10.0.0.0/8"
			return rs, o
		},
		"dport": func(rs []pve.FirewallRule, o map[string]string) ([]pve.FirewallRule, map[string]string) {
			rs[0].Dport = ""
			return rs, o
		},
		"type": func(rs []pve.FirewallRule, o map[string]string) ([]pve.FirewallRule, map[string]string) {
			rs[1].Type = "out"
			return rs, o
		},
		"disabled": func(rs []pve.FirewallRule, o map[string]string) ([]pve.FirewallRule, map[string]string) {
			rs[1].Enable = false
			return rs, o
		},
		"missing": func(rs []pve.FirewallRule, o map[string]string) ([]pve.FirewallRule, map[string]string) {
			return rs[:1], o
		},
		"duplicate": func(rs []pve.FirewallRule, o map[string]string) ([]pve.FirewallRule, map[string]string) {
			return append(rs, rs[0]), o
		},
		"foreign": func(rs []pve.FirewallRule, o map[string]string) ([]pve.FirewallRule, map[string]string) {
			return append(rs, pve.FirewallRule{Pos: 2, Type: "in", Action: "ACCEPT", Enable: true}), o
		},
		"policy": func(rs []pve.FirewallRule, o map[string]string) ([]pve.FirewallRule, map[string]string) {
			o["policy_in"] = "ACCEPT"
			return rs, o
		},
	}
	for name, m := range mut {
		rs, o := m(live(), maps.Clone(opts))
		if InSync(rs, o, d) {
			t.Errorf("%s: drift not detected", name)
		}
	}
}

func TestDrift(t *testing.T) {
	port := "22"
	d := Render([]Rule{{ID: 1, FirewallID: 1, Direction: "in", Protocol: "tcp", Port: &port, IPs: []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0")}}}, true, System{})
	cur := slices.Clone(d.Rules)
	if got := Drift(cur, maps.Clone(d.Options), d); got != "" {
		t.Fatalf("in sync: %q", got)
	}
	opts := maps.Clone(d.Options)
	opts["policy_in"] = "ACCEPT"
	if got := Drift(cur, opts, d); got != `option policy_in is "ACCEPT", want "DROP"` {
		t.Fatalf("policy: %q", got)
	}
	if got := Drift(nil, d.Options, d); !strings.HasPrefix(got, "rule missing: nebla:1:1:") {
		t.Fatalf("missing: %q", got)
	}
	extra := append(slices.Clone(cur), pve.FirewallRule{Pos: 1, Type: "in", Action: "ACCEPT", Comment: "hand made"})
	if got := Drift(extra, d.Options, d); got != `unexpected rule at pos 1 ("hand made")` {
		t.Fatalf("extra: %q", got)
	}
}

func TestSyncReplacesRulesWithPrependingAdds(t *testing.T) {
	s, pc := setup(t)
	ctx := context.Background()
	old := Render([]Rule{{ID: 1, FirewallID: 1, Direction: "in", Protocol: "tcp", Port: ptr("22"), IPs: pfx("0.0.0.0/0")}}, true, System{})
	if err := Sync(ctx, pc, node, vmid, old); err != nil {
		t.Fatal(err)
	}
	next := Render([]Rule{
		{ID: 2, FirewallID: 1, Direction: "in", Protocol: "tcp", Port: ptr("22"), IPs: pfx("0.0.0.0/0")},
		{ID: 3, FirewallID: 1, Direction: "in", Protocol: "udp", Port: ptr("9999"), IPs: pfx("10.99.0.11/32")},
	}, true, System{})
	if err := Sync(ctx, pc, node, vmid, next); err != nil {
		t.Fatal(err)
	}
	v, _ := s.VM(vmid)
	if got := comments(v.Rules); len(v.Rules) != 2 || !covers(got, next.Rules) {
		t.Fatalf("rules = %+v, want %+v", v.Rules, next.Rules)
	}
}

func TestSyncStoredRuleDiffers(t *testing.T) {
	s, pc := setup(t)
	ctx := context.Background()
	a := Render([]Rule{{ID: 1, FirewallID: 1, Direction: "in", Protocol: "tcp", Port: ptr("22"), IPs: pfx("0.0.0.0/0")}}, true, System{})
	if err := Sync(ctx, pc, node, vmid, a); err != nil {
		t.Fatal(err)
	}
	// Proxmox stores the new rule's proto differently from what we sent.
	s.OnRuleAdd(func(r *pve.FirewallRule) { r.Proto = strings.ToUpper(r.Proto) })
	s.OnFirewallWrite(func() {
		if v, _ := s.VM(vmid); v.Firewall["policy_in"] != "DROP" {
			t.Errorf("VM opened: %v", v.Firewall)
		}
	})
	b := Render([]Rule{
		{ID: 1, FirewallID: 1, Direction: "in", Protocol: "tcp", Port: ptr("22"), IPs: pfx("0.0.0.0/0")},
		{ID: 2, FirewallID: 1, Direction: "in", Protocol: "udp", Port: ptr("53"), IPs: pfx("0.0.0.0/0")},
	}, true, System{})
	for range 3 {
		err := Sync(ctx, pc, node, vmid, b)
		if !errors.Is(err, ErrStoredDiffers) {
			t.Fatalf("err = %v, want ErrStoredDiffers", err)
		}
		if want := `rule ` + b.Rules[1].Comment + `: proto is "UDP", want "udp"`; !strings.Contains(err.Error(), want) {
			t.Fatalf("err = %v, want it to contain %s", err, want)
		}
		v, _ := s.VM(vmid)
		if len(v.Rules) != 1 || v.Rules[0] != a.Rules[0] {
			t.Fatalf("copies left behind: %+v", v.Rules)
		}
	}
}

func TestDeleteRuleDigestMismatch(t *testing.T) {
	s, pc := setup(t)
	ctx := context.Background()
	a := Render([]Rule{{ID: 1, FirewallID: 1, Direction: "in", Protocol: "tcp", Port: ptr("22"), IPs: pfx("0.0.0.0/0", "::/0")}}, true, System{})
	if err := Sync(ctx, pc, node, vmid, a); err != nil {
		t.Fatal(err)
	}
	cur, _ := pc.FirewallRules(ctx, node, vmid)
	if err := pc.AddFirewallRule(ctx, node, vmid, pve.FirewallRule{Type: "in", Action: "ACCEPT", Comment: "concurrent"}); err != nil {
		t.Fatal(err)
	}
	if err := pc.DeleteFirewallRule(ctx, node, vmid, 1, cur[1].Digest); err == nil || !pve.IsTransient(err) {
		t.Fatalf("delete with a stale digest: err = %v, want a transient error", err)
	}
	if v, _ := s.VM(vmid); len(v.Rules) != 3 {
		t.Fatalf("rules = %+v", v.Rules)
	}
	// An options change also changes the digest, like the real .fw file.
	cur, _ = pc.FirewallRules(ctx, node, vmid)
	if err := pc.SetFirewallOptions(ctx, node, vmid, map[string]string{"log_level_in": "info"}); err != nil {
		t.Fatal(err)
	}
	if err := pc.DeleteFirewallRule(ctx, node, vmid, 0, cur[0].Digest); !pve.IsTransient(err) {
		t.Fatalf("delete after an options change: err = %v, want a transient error", err)
	}
}

func TestSyncStoredDiffersDeletesAllStale(t *testing.T) {
	s, pc := setup(t)
	ctx := context.Background()
	old := Render([]Rule{{ID: 9, FirewallID: 1, Direction: "in", Protocol: "tcp", Port: ptr("3306"), IPs: pfx("0.0.0.0/0")}}, true, System{})
	if err := Sync(ctx, pc, node, vmid, old); err != nil {
		t.Fatal(err)
	}
	s.OnRuleAdd(func(r *pve.FirewallRule) {
		if r.Dport == "22" {
			r.Proto = strings.ToUpper(r.Proto)
		}
	})
	s.OnFirewallWrite(func() {
		if v, _ := s.VM(vmid); v.Firewall["policy_in"] != "DROP" {
			t.Errorf("VM opened: %v", v.Firewall)
		}
	})
	d := Render([]Rule{
		{ID: 1, FirewallID: 1, Direction: "in", Protocol: "tcp", Port: ptr("22"), IPs: pfx("0.0.0.0/0")},
		{ID: 2, FirewallID: 1, Direction: "in", Protocol: "udp", Port: ptr("53"), IPs: pfx("0.0.0.0/0")},
	}, true, System{})
	if err := Sync(ctx, pc, node, vmid, d); !errors.Is(err, ErrStoredDiffers) {
		t.Fatalf("err = %v, want ErrStoredDiffers", err)
	}
	v, _ := s.VM(vmid)
	if len(v.Rules) != 1 || v.Rules[0] != d.Rules[1] {
		t.Fatalf("rules = %+v, want only %+v", v.Rules, d.Rules[1])
	}
}

func TestSyncRuleGoneAfterAddIsTransient(t *testing.T) {
	s, pc := setup(t)
	ctx := context.Background()
	// The rule is stored under another comment, as if deleted and replaced meanwhile.
	s.OnRuleAdd(func(r *pve.FirewallRule) { r.Comment = "someone else" })
	d := Render([]Rule{{ID: 1, FirewallID: 1, Direction: "in", Protocol: "tcp", Port: ptr("22"), IPs: pfx("0.0.0.0/0")}}, true, System{})
	err := Sync(ctx, pc, node, vmid, d)
	if !errors.Is(err, ErrRuleGone) || errors.Is(err, ErrStoredDiffers) {
		t.Fatalf("err = %v, want ErrRuleGone only", err)
	}
	if v, _ := s.VM(vmid); v.Firewall["policy_in"] == "DROP" {
		t.Fatalf("policy set although a rule is missing: %v", v.Firewall)
	}
}

func TestSyncStoredDiffersCleanupConflictRetries(t *testing.T) {
	s, pc := setup(t)
	ctx := context.Background()
	old := Render([]Rule{{ID: 9, FirewallID: 1, Direction: "in", Protocol: "tcp", Port: ptr("3306"), IPs: pfx("0.0.0.0/0")}}, true, System{})
	if err := Sync(ctx, pc, node, vmid, old); err != nil {
		t.Fatal(err)
	}
	s.OnRuleAdd(func(r *pve.FirewallRule) { r.Proto = strings.ToUpper(r.Proto) })
	s.OnFirewallWrite(func() {
		if v, _ := s.VM(vmid); v.Firewall["policy_in"] != "DROP" {
			t.Errorf("VM opened: %v", v.Firewall)
		}
	})
	d := Render([]Rule{{ID: 1, FirewallID: 1, Direction: "in", Protocol: "tcp", Port: ptr("22"), IPs: pfx("0.0.0.0/0")}}, true, System{})
	s.ConflictNextRuleDelete()
	err := Sync(ctx, pc, node, vmid, d)
	if errors.Is(err, ErrStoredDiffers) || !pve.IsTransient(err) {
		t.Fatalf("cleanup conflict: err = %v, want a transient error without ErrStoredDiffers", err)
	}
	if err := Sync(ctx, pc, node, vmid, d); !errors.Is(err, ErrStoredDiffers) {
		t.Fatalf("retry: err = %v, want ErrStoredDiffers", err)
	}
	if v, _ := s.VM(vmid); len(v.Rules) != 0 {
		t.Fatalf("stale rules left after cleanup: %+v", v.Rules)
	}
}

func TestSyncKeepsSystemRulesOnTop(t *testing.T) {
	s, pc := setup(t)
	ctx := context.Background()
	sys := System{PrivateNICs: []int{1, 2}, VXLANGuard: true}
	user := func(port string) Rule {
		return Rule{ID: 1, FirewallID: 1, Direction: "in", Protocol: "tcp", Port: ptr(port), IPs: pfx("0.0.0.0/0")}
	}
	first := Render([]Rule{user("22")}, true, sys)
	if err := Sync(ctx, pc, node, vmid, first); err != nil {
		t.Fatal(err)
	}
	next := Render([]Rule{user("22"), {ID: 2, FirewallID: 1, Direction: "in", Protocol: "tcp", Port: ptr("443"), IPs: pfx("0.0.0.0/0")}}, true, sys)
	if err := Sync(ctx, pc, node, vmid, next); err != nil {
		t.Fatal(err)
	}
	converged(t, s, next)
	v, _ := s.VM(vmid)
	k := 5
	for i, r := range v.Rules {
		if got := strings.HasPrefix(r.Comment, SystemPrefix); got != (i < k) {
			t.Fatalf("rule %d %q: system=%v, want system only in the first %d: %+v", i, r.Comment, got, k, v.Rules)
		}
	}
}

func TestVXLANGuardWithoutFirewall(t *testing.T) {
	s, pc := setup(t)
	ctx := context.Background()
	d := Render(nil, false, System{VXLANGuard: true})
	if err := Sync(ctx, pc, node, vmid, d); err != nil {
		t.Fatal(err)
	}
	v, _ := s.VM(vmid)
	if len(v.Rules) != 1 || v.Rules[0].Comment != "nebla:sys:vxlan" || v.Firewall["policy_in"] != "ACCEPT" {
		t.Fatalf("rules %+v options %v", v.Rules, v.Firewall)
	}
	writes := 0
	s.OnFirewallWrite(func() { writes++ })
	if err := Sync(ctx, pc, node, vmid, d); err != nil || writes != 0 {
		t.Fatalf("resync err %v writes %d", err, writes)
	}
	if err := Sync(ctx, pc, node, vmid, Render(nil, false, System{})); err != nil {
		t.Fatal(err)
	}
	if v, _ = s.VM(vmid); len(v.Rules) != 0 {
		t.Fatalf("guard should go when the location stops being vxlan: %+v", v.Rules)
	}
}

func TestSyncKeepsGuardPresent(t *testing.T) {
	s, pc := setup(t)
	ctx := context.Background()
	user := func(ids ...int64) []Rule {
		var rs []Rule
		for _, id := range ids {
			rs = append(rs, Rule{ID: id, FirewallID: 1, Direction: "in", Protocol: "tcp", Port: ptr(fmt.Sprint(1000 + id)), IPs: pfx("0.0.0.0/0")})
		}
		return rs
	}
	guard := func(nics ...int) System { return System{VXLANGuard: true, PrivateNICs: nics} }
	seen := false
	s.OnFirewallWrite(func() {
		v, _ := s.VM(vmid)
		if has := comments(v.Rules)["nebla:sys:vxlan"]; seen && !has {
			t.Errorf("guard missing mid-sync: %+v", v.Rules)
		} else if has {
			seen = true
		}
	})
	steps := []struct {
		name string
		d    Desired
	}{
		{"first", Render(user(1), true, guard(1, 2))},
		{"add a user rule", Render(user(1, 2), true, guard(1, 2))},
		{"drop to accept with NICs attached", Render(nil, false, guard(1, 2))},
		{"back to drop", Render(user(1), true, guard(1, 2))},
		{"drop a NIC under drop", Render(user(1), true, guard(1))},
	}
	for _, st := range steps {
		if err := Sync(ctx, pc, node, vmid, st.d); err != nil {
			t.Fatalf("%s: %v", st.name, err)
		}
		converged(t, s, st.d)
		v, _ := s.VM(vmid)
		k := countSystem(st.d.Rules)
		for i, r := range v.Rules {
			if isSystem(r) != (i < k) {
				t.Fatalf("%s: system rules not exactly on top: %+v", st.name, v.Rules)
			}
		}
	}
	if !seen {
		t.Fatal("guard never appeared")
	}
}

func TestDriftSeesSystemRulesBelowUserRules(t *testing.T) {
	s, pc := setup(t)
	ctx := context.Background()
	d := Render([]Rule{{ID: 1, FirewallID: 1, Direction: "in", Protocol: "tcp", Port: ptr("22"), IPs: pfx("0.0.0.0/0")}}, true, System{VXLANGuard: true})
	if err := Sync(ctx, pc, node, vmid, d); err != nil {
		t.Fatal(err)
	}
	v, _ := s.VM(vmid)
	opts := v.Firewall
	if got := Drift(v.Rules, opts, d); got != "" {
		t.Fatalf("drift = %q", got)
	}
	swapped := []pve.FirewallRule{v.Rules[1], v.Rules[0]}
	swapped[0].Pos, swapped[1].Pos = 0, 1
	if got := Drift(swapped, opts, d); got == "" {
		t.Fatal("system rule below a user rule is drift")
	}
}

package firewalls

import (
	"context"
	"maps"
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
	if len(v.Rules) != len(d.Rules) {
		t.Fatalf("rules = %+v, want %+v", v.Rules, d.Rules)
	}
	for i, r := range v.Rules {
		w := d.Rules[i]
		w.Pos = 0
		r.Pos = 0
		if r != w {
			t.Errorf("rule %d = %+v, want %+v", i, r, w)
		}
	}
}

func TestSyncNeverOpens(t *testing.T) {
	s, pc := setup(t)
	ctx := context.Background()
	a := Render([]Rule{{ID: 1, FirewallID: 1, Direction: "in", Protocol: "tcp", Port: ptr("22"), IPs: pfx("0.0.0.0/0", "::/0")}}, true)
	b := Render([]Rule{
		{ID: 2, FirewallID: 1, Direction: "in", Protocol: "tcp", Port: ptr("443"), IPs: pfx("0.0.0.0/0")},
		{ID: 3, FirewallID: 2, Direction: "out", Protocol: "udp", Port: ptr("53"), IPs: pfx("192.0.2.1/32")},
	}, true)
	none := Render(nil, false)

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
	d := Render([]Rule{{ID: 1, FirewallID: 1, Direction: "in", Protocol: "tcp", Port: ptr("22"), IPs: pfx("0.0.0.0/0", "::/0")}}, true)
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
	if err := pc.AddFirewallRule(ctx, node, vmid, pve.FirewallRule{Pos: 1, Type: "in", Action: "ACCEPT", Proto: "tcp", Dport: "25", Enable: true}); err != nil {
		t.Fatal(err)
	}
	d := Render([]Rule{{ID: 1, FirewallID: 1, Direction: "in", Protocol: "tcp", Port: ptr("22"), IPs: pfx("0.0.0.0/0")}}, true)
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
	a := Render([]Rule{{ID: 1, FirewallID: 1, Direction: "in", Protocol: "tcp", Port: ptr("22"), IPs: pfx("0.0.0.0/0", "::/0")}}, true)
	if err := Sync(ctx, pc, node, vmid, a); err != nil {
		t.Fatal(err)
	}
	b := Render([]Rule{{ID: 2, FirewallID: 1, Direction: "in", Protocol: "tcp", Port: ptr("443"), IPs: pfx("0.0.0.0/0", "::/0")}}, true)
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
	}, true)
	if err := Sync(ctx, pc, node, vmid, a); err != nil {
		t.Fatal(err)
	}
	// Simulate a hand edit that widens rule 0 but keeps its comment.
	edited := a.Rules[0]
	edited.Source = "0.0.0.0/0"
	if err := pc.DeleteFirewallRule(ctx, node, vmid, 0); err != nil {
		t.Fatal(err)
	}
	if err := pc.AddFirewallRule(ctx, node, vmid, edited); err != nil {
		t.Fatal(err)
	}
	cur, _ := pc.FirewallRules(ctx, node, vmid)
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
	d := Render([]Rule{{ID: 1, FirewallID: 1, Direction: "in", Protocol: "tcp", Port: ptr("22"), IPs: pfx("0.0.0.0/0", "::/0")}}, true)
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

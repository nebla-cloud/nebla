package firewalls

import (
	"context"
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

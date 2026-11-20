package firewalls

import (
	"fmt"
	"net/netip"
	"reflect"
	"strings"
	"testing"

	"github.com/nebla-cloud/nebla/internal/pve"
)

func ptr(s string) *string { return &s }

func pfx(ss ...string) []netip.Prefix {
	var out []netip.Prefix
	for _, s := range ss {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}

func baseOpts(in, out string) map[string]string {
	return map[string]string{"enable": "1", "ipfilter": "1", "macfilter": "1", "ndp": "1", "radv": "0", "dhcp": "0",
		"policy_in": in, "policy_out": out}
}

func strip(rs []pve.FirewallRule) []pve.FirewallRule {
	out := make([]pve.FirewallRule, len(rs))
	for i, r := range rs {
		r.Comment = ""
		out[i] = r
	}
	return out
}

func TestRender(t *testing.T) {
	ssh := Rule{ID: 7, FirewallID: 3, Direction: "in", Protocol: "tcp", Port: ptr("22"), IPs: pfx("0.0.0.0/0", "::/0")}
	cases := []struct {
		name  string
		rules []Rule
		any   bool
		opts  map[string]string
		want  []pve.FirewallRule
	}{
		{name: "no firewall", opts: baseOpts("ACCEPT", "ACCEPT")},
		{name: "no firewall ignores rules", rules: []Rule{ssh}, opts: baseOpts("ACCEPT", "ACCEPT")},
		{name: "ssh both families", rules: []Rule{ssh}, any: true, opts: baseOpts("DROP", "ACCEPT"), want: []pve.FirewallRule{
			{Type: "in", Action: "ACCEPT", Iface: "net0", Proto: "tcp", Dport: "22", Source: "0.0.0.0/0", Enable: true},
			{Type: "in", Action: "ACCEPT", Iface: "net0", Proto: "tcp", Dport: "22", Source: "::/0", Enable: true},
		}},
		{name: "firewall without rules", any: true, opts: baseOpts("DROP", "ACCEPT")},
		{name: "outbound", rules: []Rule{{ID: 1, FirewallID: 1, Direction: "out", Protocol: "udp", Port: ptr("53"), IPs: pfx("10.0.0.0/8")}},
			any: true, opts: baseOpts("DROP", "DROP"), want: []pve.FirewallRule{
				{Type: "out", Action: "ACCEPT", Iface: "net0", Proto: "udp", Dport: "53", Dest: "10.0.0.0/8", Enable: true},
			}},
		{name: "icmp per family", rules: []Rule{{ID: 1, FirewallID: 1, Direction: "in", Protocol: "icmp", IPs: pfx("2001:db8::/32", "192.0.2.0/24")}},
			any: true, opts: baseOpts("DROP", "ACCEPT"), want: []pve.FirewallRule{
				{Type: "in", Action: "ACCEPT", Iface: "net0", Proto: "icmp", Source: "192.0.2.0/24", Enable: true},
				{Type: "in", Action: "ACCEPT", Iface: "net0", Proto: "ipv6-icmp", Source: "2001:db8::/32", Enable: true},
			}},
		{name: "port range", rules: []Rule{{ID: 1, FirewallID: 1, Direction: "in", Protocol: "tcp", Port: ptr("8000-8100"), IPs: pfx("0.0.0.0/0")}},
			any: true, opts: baseOpts("DROP", "ACCEPT"), want: []pve.FirewallRule{
				{Type: "in", Action: "ACCEPT", Iface: "net0", Proto: "tcp", Dport: "8000:8100", Source: "0.0.0.0/0", Enable: true},
			}},
		{name: "order by firewall then rule position", any: true, opts: baseOpts("DROP", "ACCEPT"), rules: []Rule{
			{ID: 9, FirewallID: 2, Direction: "in", Protocol: "tcp", Port: ptr("1"), IPs: pfx("0.0.0.0/0")},
			{ID: 5, FirewallID: 1, Direction: "in", Protocol: "tcp", Port: ptr("2"), IPs: pfx("0.0.0.0/0")},
			{ID: 4, FirewallID: 1, Direction: "in", Protocol: "tcp", Port: ptr("3"), IPs: pfx("0.0.0.0/0")},
		}, want: []pve.FirewallRule{
			{Type: "in", Action: "ACCEPT", Iface: "net0", Proto: "tcp", Dport: "2", Source: "0.0.0.0/0", Enable: true},
			{Type: "in", Action: "ACCEPT", Iface: "net0", Proto: "tcp", Dport: "3", Source: "0.0.0.0/0", Enable: true},
			{Type: "in", Action: "ACCEPT", Iface: "net0", Proto: "tcp", Dport: "1", Source: "0.0.0.0/0", Enable: true},
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := Render(c.rules, c.any)
			if !reflect.DeepEqual(d.Options, c.opts) {
				t.Errorf("options = %v, want %v", d.Options, c.opts)
			}
			if got := strip(d.Rules); len(got) != len(c.want) || len(got) > 0 && !reflect.DeepEqual(got, c.want) {
				t.Errorf("rules = %+v\nwant %+v", got, c.want)
			}
		})
	}

	d := Render([]Rule{ssh}, true)
	for i, want := range []string{"nebla:3:7:0:", "nebla:3:7:0:"} {
		if !strings.HasPrefix(d.Rules[i].Comment, want) {
			t.Errorf("comment %d = %q, want prefix %q", i, d.Rules[i].Comment, want)
		}
	}
	if d.Rules[0].Comment == d.Rules[1].Comment {
		t.Error("v4 and v6 comments must differ")
	}
}

func TestRenderSplitsLongLists(t *testing.T) {
	var ips []netip.Prefix
	for i := range 50 {
		ips = append(ips, netip.MustParsePrefix(fmt.Sprintf("2001:db8:aaaa:bbbb:cccc:dddd:eeee:%x/128", i+1)))
	}
	d := Render([]Rule{{ID: 2, FirewallID: 1, Direction: "in", Protocol: "tcp", Port: ptr("443"), IPs: ips}}, true)
	if len(d.Rules) < 2 {
		t.Fatalf("want a split, got %d rules", len(d.Rules))
	}
	var all []string
	for i, r := range d.Rules {
		if len(r.Source) > 512 {
			t.Errorf("part %d source is %d chars", i, len(r.Source))
		}
		if want := fmt.Sprintf("nebla:1:2:%d:", i); !strings.HasPrefix(r.Comment, want) {
			t.Errorf("comment %q, want prefix %q", r.Comment, want)
		}
		all = append(all, strings.Split(r.Source, ",")...)
	}
	if len(all) != 50 {
		t.Errorf("rendered %d prefixes, want 50", len(all))
	}
}

func TestRenderHashAndDeterminism(t *testing.T) {
	base := Rule{ID: 1, FirewallID: 1, Direction: "in", Protocol: "tcp", Port: ptr("22"), IPs: pfx("192.0.2.0/24")}
	c0 := Render([]Rule{base}, true).Rules[0].Comment
	if again := Render([]Rule{base}, true); !reflect.DeepEqual(again, Render([]Rule{base}, true)) || again.Rules[0].Comment != c0 {
		t.Fatal("render is not deterministic")
	}
	variants := map[string]func(r *Rule){
		"proto":     func(r *Rule) { r.Protocol = "udp" },
		"port":      func(r *Rule) { r.Port = ptr("23") },
		"no port":   func(r *Rule) { r.Port = nil },
		"ips":       func(r *Rule) { r.IPs = pfx("198.51.100.0/24") },
		"direction": func(r *Rule) { r.Direction = "out" },
	}
	for name, mut := range variants {
		r := base
		mut(&r)
		if c := Render([]Rule{r}, true).Rules[0].Comment; c == c0 {
			t.Errorf("%s: comment hash unchanged (%s)", name, c)
		}
	}
	r := base
	r.Description = "ssh"
	if c := Render([]Rule{r}, true).Rules[0].Comment; c != c0 {
		t.Errorf("description is not rendered, hash should not change")
	}
}

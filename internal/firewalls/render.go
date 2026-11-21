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

// Package firewalls renders Nebla firewall rules onto Proxmox VM firewalls.
package firewalls

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/netip"
	"slices"
	"strings"

	"github.com/nebla-cloud/nebla/internal/pve"
)

// maxList is Proxmox's limit on a rule's source or dest.
const maxList = 512

// Rule is one effective Nebla rule on a server.
type Rule struct {
	ID, FirewallID int64
	Direction      string
	Protocol       string
	Port           *string
	IPs            []netip.Prefix
	Description    string
}

// Desired is the Proxmox firewall state a VM should have.
type Desired struct {
	Options map[string]string
	Rules   []pve.FirewallRule
}

// Render maps effective rules, given in position order per firewall, to the desired Proxmox state.
func Render(rules []Rule, anyFirewall bool) Desired {
	d := Desired{Options: map[string]string{"enable": "1", "ipfilter": "1", "macfilter": "1", "ndp": "1", "radv": "0", "dhcp": "0",
		"policy_in": "ACCEPT", "policy_out": "ACCEPT"}}
	if !anyFirewall {
		return d
	}
	d.Options["policy_in"] = "DROP"
	sorted := slices.Clone(rules)
	slices.SortStableFunc(sorted, func(a, b Rule) int { return cmp.Compare(a.FirewallID, b.FirewallID) })
	for _, r := range sorted {
		if r.Direction == "out" {
			d.Options["policy_out"] = "DROP"
		}
		d.Rules = append(d.Rules, renderRule(r)...)
	}
	return d
}

// renderRule splits one rule into Proxmox rules by address family and list length.
func renderRule(r Rule) []pve.FirewallRule {
	var out []pve.FirewallRule
	for _, v6 := range []bool{false, true} {
		var cidrs []string
		for _, p := range r.IPs {
			if p.Addr().Unmap().Is6() == v6 {
				cidrs = append(cidrs, p.String())
			}
		}
		for part, list := range split(cidrs) {
			out = append(out, render(r, v6, part, list))
		}
	}
	return out
}

func render(r Rule, v6 bool, part int, list string) pve.FirewallRule {
	pr := pve.FirewallRule{Type: r.Direction, Action: "ACCEPT", Iface: "net0", Proto: r.Protocol, Enable: true}
	if pr.Proto == "icmp" && v6 {
		pr.Proto = "ipv6-icmp"
	}
	if r.Port != nil {
		pr.Dport = strings.ReplaceAll(*r.Port, "-", ":")
	}
	if r.Direction == "out" {
		pr.Dest = list
	} else {
		pr.Source = list
	}
	sum := sha256.Sum256(fmt.Appendf(nil, "%s|%s|%s|%s|%s|%s|%s|%t", pr.Type, pr.Action, pr.Iface, pr.Proto, pr.Dport, pr.Source, pr.Dest, pr.Enable))
	pr.Comment = fmt.Sprintf("nebla:%d:%d:%d:%s", r.FirewallID, r.ID, part, hex.EncodeToString(sum[:6]))
	return pr
}

// split packs CIDRs into comma lists of at most maxList characters.
func split(cidrs []string) []string {
	var out []string
	cur := ""
	for _, c := range cidrs {
		if cur != "" && len(cur)+1+len(c) > maxList {
			out = append(out, cur)
			cur = ""
		}
		if cur != "" {
			cur += ","
		}
		cur += c
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

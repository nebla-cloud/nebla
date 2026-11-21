package firewalls

import (
	"fmt"
	"net/http"
	"net/netip"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/nebla-cloud/nebla/internal/apierr"
	"github.com/nebla-cloud/nebla/internal/labels"
)

const (
	MaxFirewalls   = 50
	MaxRules       = 100
	MaxIPs         = 50
	MaxEffective   = 5
	MaxDescription = 255
	MaxLabels      = 64
	// MaxRenderedRules caps the Proxmox rules one server's firewalls render to.
	MaxRenderedRules = 250

	TypeServer        = "server"
	TypeLabelSelector = "label_selector"
)

var (
	nameRE    = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	protocols = []string{"tcp", "udp", "icmp", "esp", "gre"}

	errNotFound  = apierr.New(http.StatusNotFound, "not_found", "not found")
	errNameTaken = apierr.New(http.StatusConflict, "name_taken", "firewall name is already taken")
	errNoJobs    = apierr.New(http.StatusServiceUnavailable, "service_unavailable", "the job runtime is not running")
	errDeleting  = apierr.New(http.StatusConflict, "locked", "server is being deleted")
)

// RuleInput is one rule as submitted; IPs are the sources of an in rule or the destinations of an out rule.
type RuleInput struct {
	Direction, Protocol string
	Port                *string
	IPs                 []string
	Description         string
}

// ResourceInput is a server or a label selector to apply a firewall to.
type ResourceInput struct {
	Type     string
	ServerID *int64
	Selector *string
}

func invalid(format string, a ...any) error {
	return apierr.New(http.StatusUnprocessableEntity, "invalid_input", fmt.Sprintf(format, a...))
}

func limitExceeded(msg string) error {
	return apierr.New(http.StatusUnprocessableEntity, "limit_exceeded", msg)
}

func validName(n string) error {
	if !nameRE.MatchString(n) {
		return invalid("name must match %s", nameRE)
	}
	return nil
}

func validLabels(l map[string]string) error {
	if len(l) > MaxLabels {
		return invalid("at most %d labels", MaxLabels)
	}
	for k, v := range l {
		if !labels.KeyRE.MatchString(k) {
			return invalid("label key %q must match %s", k, labels.KeyRE)
		}
		if !labels.ValueRE.MatchString(v) {
			return invalid("label value %q must match %s", v, labels.ValueRE)
		}
	}
	return nil
}

// IPsFor picks the address list a rule's direction uses and refuses the other one.
func IPsFor(i int, direction string, sourceIPs, destinationIPs []string) ([]string, error) {
	switch direction {
	case "in":
		if len(destinationIPs) > 0 {
			return nil, invalid("rules[%d]: in rules take source_ips, not destination_ips", i)
		}
		return sourceIPs, nil
	case "out":
		if len(sourceIPs) > 0 {
			return nil, invalid("rules[%d]: out rules take destination_ips, not source_ips", i)
		}
		return destinationIPs, nil
	}
	return nil, invalid("rules[%d]: direction must be in or out", i)
}

// portNum parses a port without signs or spaces.
func portNum(s string) (int, bool) {
	n, err := strconv.Atoi(s)
	return n, err == nil && strings.Trim(s, "0123456789") == "" && n >= 1 && n <= 65535
}

// canonPort returns p as "80" or "8000-8100", or false if it is not a valid port or range.
func canonPort(p string) (string, bool) {
	lo, hi, isRange := strings.Cut(p, "-")
	a, ok := portNum(lo)
	if !ok {
		return "", false
	}
	if !isRange {
		return strconv.Itoa(a), true
	}
	b, ok := portNum(hi)
	if !ok || a > b {
		return "", false
	}
	return strconv.Itoa(a) + "-" + strconv.Itoa(b), true
}

// validRules checks rules and returns them with canonical, deduplicated CIDRs.
func validRules(in []RuleInput) ([]RuleInput, error) {
	if len(in) > MaxRules {
		return nil, invalid("a firewall can have at most %d rules", MaxRules)
	}
	out := make([]RuleInput, len(in))
	for i, r := range in {
		if r.Direction != "in" && r.Direction != "out" {
			return nil, invalid("rules[%d]: direction must be in or out", i)
		}
		if !slices.Contains(protocols, r.Protocol) {
			return nil, invalid("rules[%d]: protocol must be one of %s", i, strings.Join(protocols, ", "))
		}
		needsPort := r.Protocol == "tcp" || r.Protocol == "udp"
		switch {
		case needsPort && r.Port == nil:
			return nil, invalid("rules[%d]: port is required for %s", i, r.Protocol)
		case !needsPort && r.Port != nil:
			return nil, invalid("rules[%d]: port is only allowed for tcp and udp", i)
		}
		if r.Port != nil {
			p, ok := canonPort(*r.Port)
			if !ok {
				return nil, invalid("rules[%d]: port must be 1-65535 or a range like 8000-8100", i)
			}
			r.Port = &p
		}
		if len(r.Description) > MaxDescription {
			return nil, invalid("rules[%d]: description must be at most %d characters", i, MaxDescription)
		}
		if len(r.IPs) == 0 {
			return nil, invalid("rules[%d]: at least one IP range is required", i)
		}
		if len(r.IPs) > MaxIPs {
			return nil, invalid("rules[%d]: at most %d IP ranges", i, MaxIPs)
		}
		ips := make([]string, 0, len(r.IPs))
		for _, s := range r.IPs {
			p, err := netip.ParsePrefix(strings.TrimSpace(s))
			if err != nil || p.Addr().Zone() != "" {
				return nil, invalid("rules[%d]: %q is not a CIDR like 10.0.0.0/8", i, s)
			}
			if p.Addr().Is4In6() {
				return nil, invalid("rules[%d]: %q is an IPv4-mapped IPv6 range; use the IPv4 form", i, s)
			}
			if c := p.Masked().String(); !slices.Contains(ips, c) {
				ips = append(ips, c)
			}
		}
		r.IPs = ips
		out[i] = r
	}
	return out, nil
}

// validResources checks resources, canonicalises selectors and drops duplicates.
func validResources(in []ResourceInput) ([]ResourceInput, error) {
	var out []ResourceInput
	seen := map[string]bool{}
	for i, r := range in {
		var key string
		switch r.Type {
		case TypeServer:
			if r.ServerID == nil || r.Selector != nil {
				return nil, invalid("apply_to[%d]: a server resource takes a server id only", i)
			}
			key = "s:" + strconv.FormatInt(*r.ServerID, 10)
			r.ServerID = new(*r.ServerID)
		case TypeLabelSelector:
			if r.Selector == nil || r.ServerID != nil {
				return nil, invalid("apply_to[%d]: a label_selector resource takes a selector only", i)
			}
			sel, err := labels.Parse(*r.Selector)
			if err != nil {
				return nil, err
			}
			canon := sel.String()
			key, r.Selector = "l:"+canon, &canon
		default:
			return nil, invalid("apply_to[%d]: type must be server or label_selector", i)
		}
		if !seen[key] {
			seen[key] = true
			out = append(out, r)
		}
	}
	return out, nil
}

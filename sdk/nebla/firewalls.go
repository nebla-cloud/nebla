// Copyright (C) 2026 The Nebla contributors
// SPDX-License-Identifier: Apache-2.0

package nebla

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"time"

	"github.com/nebla-cloud/nebla/sdk/nebla/internal/wire"
)

// FirewallDirection is the traffic direction a rule matches.
type FirewallDirection string

// Firewall directions.
const (
	FirewallDirectionIn  FirewallDirection = "in"
	FirewallDirectionOut FirewallDirection = "out"
)

// FirewallProtocol is the protocol a rule matches.
type FirewallProtocol string

// Firewall protocols.
const (
	FirewallProtocolTCP  FirewallProtocol = "tcp"
	FirewallProtocolUDP  FirewallProtocol = "udp"
	FirewallProtocolICMP FirewallProtocol = "icmp"
	FirewallProtocolESP  FirewallProtocol = "esp"
	FirewallProtocolGRE  FirewallProtocol = "gre"
)

// FirewallResourceType says what a FirewallResource points at.
type FirewallResourceType string

// Firewall resource types.
const (
	FirewallResourceTypeServer        FirewallResourceType = "server"
	FirewallResourceTypeLabelSelector FirewallResourceType = "label_selector"
)

// FirewallRule allows traffic through a firewall.
type FirewallRule struct {
	Direction FirewallDirection
	Protocol  FirewallProtocol
	// Port is a port or range such as 8000-8100; required for tcp and udp.
	Port string
	// SourceIPs are the CIDRs allowed in, for inbound rules.
	SourceIPs []string
	// DestinationIPs are the CIDRs allowed out, for outbound rules.
	DestinationIPs []string
	Description    string
}

// FirewallResourceServer identifies a server.
type FirewallResourceServer struct {
	ID int64 `json:"id"`
}

// FirewallLabelSelector selects servers by label.
type FirewallLabelSelector struct {
	Selector string `json:"selector"`
}

// FirewallAppliedResource is a server a label selector currently matches.
type FirewallAppliedResource struct {
	Type   FirewallResourceType
	Server FirewallResourceServer
}

// FirewallResource is what a firewall applies to: a server or a label selector.
type FirewallResource struct {
	Type          FirewallResourceType
	Server        *FirewallResourceServer
	LabelSelector *FirewallLabelSelector
	// AppliedToResources lists the servers a label selector matches; read-only.
	AppliedToResources []FirewallAppliedResource
}

// FirewallServer returns a resource for one server.
func FirewallServer(id int64) FirewallResource {
	return FirewallResource{Type: FirewallResourceTypeServer, Server: &FirewallResourceServer{ID: id}}
}

// FirewallLabels returns a resource for the servers matching a label selector.
func FirewallLabels(selector string) FirewallResource {
	return FirewallResource{Type: FirewallResourceTypeLabelSelector, LabelSelector: &FirewallLabelSelector{Selector: selector}}
}

// MarshalJSON encodes the resource in API form, leaving out the read-only matches.
func (r FirewallResource) MarshalJSON() ([]byte, error) {
	return json.Marshal(firewallResourceToWire(r))
}

// Firewall is a set of rules applied to servers.
type Firewall struct {
	ID        int64
	Name      string
	Labels    map[string]string
	Rules     []FirewallRule
	AppliedTo []FirewallResource
	Created   time.Time
}

func firewallRuleFromWire(w wire.FirewallRule) FirewallRule {
	r := FirewallRule{Direction: FirewallDirection(w.Direction), Protocol: FirewallProtocol(w.Protocol)}
	if w.Port != nil {
		r.Port = *w.Port
	}
	if w.SourceIps != nil {
		r.SourceIPs = *w.SourceIps
	}
	if w.DestinationIps != nil {
		r.DestinationIPs = *w.DestinationIps
	}
	if w.Description != nil {
		r.Description = *w.Description
	}
	return r
}

func firewallRuleToWire(r FirewallRule) wire.FirewallRule {
	w := wire.FirewallRule{Direction: wire.FirewallRuleDirection(r.Direction), Protocol: wire.FirewallRuleProtocol(r.Protocol)}
	if r.Port != "" {
		w.Port = &r.Port
	}
	if len(r.SourceIPs) > 0 {
		w.SourceIps = &r.SourceIPs
	}
	if len(r.DestinationIPs) > 0 {
		w.DestinationIps = &r.DestinationIPs
	}
	if r.Description != "" {
		w.Description = &r.Description
	}
	return w
}

func firewallRulesToWire(rs []FirewallRule) []wire.FirewallRule {
	out := make([]wire.FirewallRule, 0, len(rs))
	for _, r := range rs {
		out = append(out, firewallRuleToWire(r))
	}
	return out
}

func firewallResourceFromWire(w wire.FirewallResource) FirewallResource {
	r := FirewallResource{Type: FirewallResourceType(w.Type)}
	if w.Server != nil {
		r.Server = &FirewallResourceServer{ID: w.Server.ID}
	}
	if w.LabelSelector != nil {
		r.LabelSelector = &FirewallLabelSelector{Selector: w.LabelSelector.Selector}
	}
	if w.AppliedToResources != nil {
		for _, a := range *w.AppliedToResources {
			r.AppliedToResources = append(r.AppliedToResources, FirewallAppliedResource{
				Type: FirewallResourceType(a.Type), Server: FirewallResourceServer{ID: a.Server.ID},
			})
		}
	}
	return r
}

func firewallResourceToWire(r FirewallResource) wire.FirewallResource {
	w := wire.FirewallResource{Type: wire.FirewallResourceType(r.Type)}
	if r.Server != nil {
		w.Server = &struct {
			ID int64 `json:"id"`
		}{ID: r.Server.ID}
	}
	if r.LabelSelector != nil {
		w.LabelSelector = &struct {
			Selector string `json:"selector"`
		}{Selector: r.LabelSelector.Selector}
	}
	return w
}

func firewallResourcesToWire(rs []FirewallResource) []wire.FirewallResource {
	out := make([]wire.FirewallResource, 0, len(rs))
	for _, r := range rs {
		out = append(out, firewallResourceToWire(r))
	}
	return out
}

func firewallFromWire(w wire.Firewall) Firewall {
	f := Firewall{ID: w.ID, Name: w.Name, Labels: map[string]string(w.Labels), Created: w.Created}
	for _, r := range w.Rules {
		f.Rules = append(f.Rules, firewallRuleFromWire(r))
	}
	for _, r := range w.AppliedTo {
		f.AppliedTo = append(f.AppliedTo, firewallResourceFromWire(r))
	}
	return f
}

// FirewallCreateOpts creates a firewall.
type FirewallCreateOpts struct {
	Name    string
	Labels  map[string]string
	Rules   []FirewallRule
	ApplyTo []FirewallResource
}

// FirewallCreateResult is the outcome of creating a firewall.
type FirewallCreateResult struct {
	Firewall Firewall
	// Actions has one apply_firewalls action per server the firewall covers.
	Actions []Action
}

// FirewallUpdateOpts changes a firewall; nil fields are not sent, and a non-nil empty Labels clears them.
type FirewallUpdateOpts struct {
	Name   *string
	Labels *map[string]string
}

// FirewallListOpts filters a list of firewalls.
type FirewallListOpts struct {
	ListOpts
	Name          string
	LabelSelector string
}

func (o FirewallListOpts) values() url.Values {
	q := o.ListOpts.values()
	setIf(q, "name", o.Name)
	setIf(q, "label_selector", o.LabelSelector)
	return q
}

type firewallCreateBody struct {
	Name    string                  `json:"name"`
	Labels  map[string]string       `json:"labels,omitempty"`
	Rules   []wire.FirewallRule     `json:"rules,omitempty"`
	ApplyTo []wire.FirewallResource `json:"apply_to,omitempty"`
}

type firewallUpdateBody struct {
	Name   *string            `json:"name,omitempty"`
	Labels *map[string]string `json:"labels,omitempty"`
}

// FirewallsService works with firewalls.
type FirewallsService struct{ c *Client }

func firewallPath(id int64, suffix string) string {
	return "/firewalls/" + strconv.FormatInt(id, 10) + suffix
}

// List returns one page of firewalls.
func (s *FirewallsService) List(ctx context.Context, opts FirewallListOpts) ([]Firewall, Meta, error) {
	return listPage(ctx, s.c, "/firewalls", opts.values(), "firewalls", firewallFromWire)
}

// All returns every firewall matching opts.
func (s *FirewallsService) All(ctx context.Context, opts FirewallListOpts) ([]Firewall, error) {
	return listAll(ctx, s.c, "/firewalls", opts.values(), "firewalls", firewallFromWire)
}

// Get returns one firewall.
func (s *FirewallsService) Get(ctx context.Context, id int64) (*Firewall, error) {
	var w wire.Firewall
	if err := s.c.do(ctx, "GET", firewallPath(id, ""), nil, nil, &w); err != nil {
		return nil, err
	}
	f := firewallFromWire(w)
	return &f, nil
}

// GetByName returns the firewall with exactly this name.
func (s *FirewallsService) GetByName(ctx context.Context, name string) (*Firewall, error) {
	all := func(ctx context.Context) ([]Firewall, error) { return s.All(ctx, FirewallListOpts{Name: name}) }
	return getByName(ctx, all, name, func(f Firewall) string { return f.Name }, "firewall")
}

// Create creates a firewall; the actions apply it to the servers it covers.
func (s *FirewallsService) Create(ctx context.Context, opts FirewallCreateOpts) (FirewallCreateResult, error) {
	var w struct {
		Firewall wire.Firewall `json:"firewall"`
		Actions  []wire.Action `json:"actions"`
	}
	body := firewallCreateBody{Name: opts.Name, Labels: opts.Labels}
	if len(opts.Rules) > 0 {
		body.Rules = firewallRulesToWire(opts.Rules)
	}
	if len(opts.ApplyTo) > 0 {
		body.ApplyTo = firewallResourcesToWire(opts.ApplyTo)
	}
	if err := s.c.do(ctx, "POST", "/firewalls", nil, body, &w); err != nil {
		return FirewallCreateResult{}, err
	}
	return FirewallCreateResult{Firewall: firewallFromWire(w.Firewall), Actions: actionsFromWire(w.Actions)}, nil
}

// Update renames a firewall or replaces its labels.
func (s *FirewallsService) Update(ctx context.Context, id int64, opts FirewallUpdateOpts) (*Firewall, error) {
	var w wire.Firewall
	if err := s.c.do(ctx, "PUT", firewallPath(id, ""), nil, firewallUpdateBody(opts), &w); err != nil {
		return nil, err
	}
	f := firewallFromWire(w)
	return &f, nil
}

// Delete deletes a firewall and returns the actions that remove it from its servers.
func (s *FirewallsService) Delete(ctx context.Context, id int64) ([]Action, error) {
	return actionsReply(ctx, s.c, "DELETE", firewallPath(id, ""), nil)
}

// Actions returns one page of the apply_firewalls actions a firewall caused.
func (s *FirewallsService) Actions(ctx context.Context, id int64, opts ActionListOpts) ([]Action, Meta, error) {
	return listPage(ctx, s.c, firewallPath(id, "/actions"), opts.values(), "actions", actionFromWire)
}

// AllActions returns every action of a firewall matching opts.
func (s *FirewallsService) AllActions(ctx context.Context, id int64, opts ActionListOpts) ([]Action, error) {
	return listAll(ctx, s.c, firewallPath(id, "/actions"), opts.values(), "actions", actionFromWire)
}

// SetRules replaces the firewall's rules.
func (s *FirewallsService) SetRules(ctx context.Context, id int64, rules []FirewallRule) ([]Action, error) {
	body := struct {
		Rules []wire.FirewallRule `json:"rules"`
	}{firewallRulesToWire(rules)}
	return actionsReply(ctx, s.c, "POST", firewallPath(id, "/actions/set_rules"), body)
}

// ApplyToResources applies the firewall to servers or label selectors.
func (s *FirewallsService) ApplyToResources(ctx context.Context, id int64, resources []FirewallResource) ([]Action, error) {
	body := struct {
		ApplyTo []wire.FirewallResource `json:"apply_to"`
	}{firewallResourcesToWire(resources)}
	return actionsReply(ctx, s.c, "POST", firewallPath(id, "/actions/apply_to_resources"), body)
}

// RemoveFromResources removes the firewall from servers or label selectors.
func (s *FirewallsService) RemoveFromResources(ctx context.Context, id int64, resources []FirewallResource) ([]Action, error) {
	body := struct {
		RemoveFrom []wire.FirewallResource `json:"remove_from"`
	}{firewallResourcesToWire(resources)}
	return actionsReply(ctx, s.c, "POST", firewallPath(id, "/actions/remove_from_resources"), body)
}

func actionsFromWire(ws []wire.Action) []Action {
	out := make([]Action, 0, len(ws))
	for _, w := range ws {
		out = append(out, actionFromWire(w))
	}
	return out
}

// actionsReply sends a request whose reply is {actions}.
func actionsReply(ctx context.Context, c *Client, method, path string, body any) ([]Action, error) {
	var w struct {
		Actions []wire.Action `json:"actions"`
	}
	if err := c.do(ctx, method, path, nil, body, &w); err != nil {
		return nil, err
	}
	return actionsFromWire(w.Actions), nil
}

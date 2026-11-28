// Copyright (C) 2026 The Nebla contributors
// SPDX-License-Identifier: Apache-2.0

package nebla

import (
	"context"
	"net/url"
	"strconv"
	"time"

	"github.com/nebla-cloud/nebla/sdk/nebla/internal/wire"
)

// IPType is an address family.
type IPType string

// Address families.
const (
	IPTypeIPv4 IPType = "ipv4"
	IPTypeIPv6 IPType = "ipv6"
)

// FloatingIPStatus is the state of a floating IP.
type FloatingIPStatus string

// Floating IP statuses.
const (
	FloatingIPStatusActive    FloatingIPStatus = "active"
	FloatingIPStatusReleasing FloatingIPStatus = "releasing"
	FloatingIPStatusDeleting  FloatingIPStatus = "deleting"
)

// FloatingIPGuestConfig holds commands to run inside servers so they use the address.
type FloatingIPGuestConfig struct {
	// Server says where the commands run, in words.
	Server  *string
	Apply   *string
	Persist *string
	// PreviousServer is the server the address moved away from.
	PreviousServer *string
	Remove         *string
}

// FloatingIP is an address that can move between servers.
type FloatingIP struct {
	ID          int64
	Name        string
	Description string
	Labels      map[string]string
	Type        IPType
	// IP is the single address, as /32 (IPv4) or /128 (IPv6).
	IP       string
	Location LocationRef
	// Server is the id of the assigned server, nil when unassigned.
	Server      *int64
	Status      FloatingIPStatus
	GuestConfig *FloatingIPGuestConfig
	Created     time.Time
}

func floatingIPFromWire(w wire.FloatingIP) FloatingIP {
	ip := FloatingIP{
		ID: w.ID, Name: w.Name, Description: w.Description, Labels: map[string]string(w.Labels),
		Type: IPType(w.Type), IP: w.IP, Location: locationRefFromWire(w.Location),
		Server: w.Server, Status: FloatingIPStatus(w.Status), Created: w.Created,
	}
	if g := w.GuestConfig; g != nil {
		ip.GuestConfig = &FloatingIPGuestConfig{Server: g.Server, Apply: g.Apply, Persist: g.Persist, PreviousServer: g.PreviousServer, Remove: g.Remove}
	}
	return ip
}

// FloatingIPCreateOpts creates a floating IP; set exactly one of Location and Server.
type FloatingIPCreateOpts struct {
	// Name is generated from the address when empty.
	Name        string
	Description string
	Labels      map[string]string
	Type        IPType
	// Location is a location id or name.
	Location string
	// Server assigns the new address to a server, by id or name.
	Server string
}

// FloatingIPCreateResult is the outcome of creating a floating IP.
type FloatingIPCreateResult struct {
	FloatingIP FloatingIP
	// Action is set only when the address was assigned to a server.
	Action *Action
}

// FloatingIPUpdateOpts changes a floating IP; nil fields are not sent, and a non-nil empty Labels clears them.
type FloatingIPUpdateOpts struct {
	Name        *string
	Description *string
	Labels      *map[string]string
}

// FloatingIPListOpts filters a list of floating IPs.
type FloatingIPListOpts struct {
	ListOpts
	Name          string
	LabelSelector string
	// Server is a server id or name.
	Server string
	// Location is a location id or name.
	Location string
}

func (o FloatingIPListOpts) values() url.Values {
	q := o.ListOpts.values()
	setIf(q, "name", o.Name)
	setIf(q, "label_selector", o.LabelSelector)
	setIf(q, "server", o.Server)
	setIf(q, "location", o.Location)
	return q
}

type floatingIPCreateBody struct {
	Name        string            `json:"name,omitempty"`
	Description string            `json:"description,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	Type        IPType            `json:"type"`
	Location    string            `json:"location,omitempty"`
	Server      string            `json:"server,omitempty"`
}

type floatingIPUpdateBody struct {
	Name        *string            `json:"name,omitempty"`
	Description *string            `json:"description,omitempty"`
	Labels      *map[string]string `json:"labels,omitempty"`
}

// FloatingIPsService works with floating IPs.
type FloatingIPsService struct{ c *Client }

func floatingIPPath(id int64, suffix string) string {
	return "/floating_ips/" + strconv.FormatInt(id, 10) + suffix
}

// List returns one page of floating IPs.
func (s *FloatingIPsService) List(ctx context.Context, opts FloatingIPListOpts) ([]FloatingIP, Meta, error) {
	return listPage(ctx, s.c, "/floating_ips", opts.values(), "floating_ips", floatingIPFromWire)
}

// All returns every floating IP matching opts.
func (s *FloatingIPsService) All(ctx context.Context, opts FloatingIPListOpts) ([]FloatingIP, error) {
	return listAll(ctx, s.c, "/floating_ips", opts.values(), "floating_ips", floatingIPFromWire)
}

// Get returns one floating IP.
func (s *FloatingIPsService) Get(ctx context.Context, id int64) (*FloatingIP, error) {
	var w wire.FloatingIP
	if err := s.c.do(ctx, "GET", floatingIPPath(id, ""), nil, nil, &w); err != nil {
		return nil, err
	}
	ip := floatingIPFromWire(w)
	return &ip, nil
}

// GetByName returns the floating IP with exactly this name.
func (s *FloatingIPsService) GetByName(ctx context.Context, name string) (*FloatingIP, error) {
	all := func(ctx context.Context) ([]FloatingIP, error) {
		return s.All(ctx, FloatingIPListOpts{Name: name})
	}
	return getByName(ctx, all, name, func(ip FloatingIP) string { return ip.Name }, "floating IP")
}

// Create creates a floating IP, optionally assigned to a server.
func (s *FloatingIPsService) Create(ctx context.Context, opts FloatingIPCreateOpts) (FloatingIPCreateResult, error) {
	var w struct {
		FloatingIP wire.FloatingIP `json:"floating_ip"`
		Action     *wire.Action    `json:"action"`
	}
	body := floatingIPCreateBody{Name: opts.Name, Description: opts.Description, Labels: opts.Labels, Type: opts.Type, Location: opts.Location, Server: opts.Server}
	if err := s.c.do(ctx, "POST", "/floating_ips", nil, body, &w); err != nil {
		return FloatingIPCreateResult{}, err
	}
	res := FloatingIPCreateResult{FloatingIP: floatingIPFromWire(w.FloatingIP)}
	if w.Action != nil {
		a := actionFromWire(*w.Action)
		res.Action = &a
	}
	return res, nil
}

// Update renames a floating IP, changes its description or replaces its labels.
func (s *FloatingIPsService) Update(ctx context.Context, id int64, opts FloatingIPUpdateOpts) (*FloatingIP, error) {
	var w wire.FloatingIP
	if err := s.c.do(ctx, "PUT", floatingIPPath(id, ""), nil, floatingIPUpdateBody(opts), &w); err != nil {
		return nil, err
	}
	ip := floatingIPFromWire(w)
	return &ip, nil
}

// Delete deletes a floating IP, unassigning it first if needed, and returns the delete action.
func (s *FloatingIPsService) Delete(ctx context.Context, id int64) (*Action, error) {
	return actionReply(ctx, s.c, "DELETE", floatingIPPath(id, ""), nil)
}

// Actions returns one page of a floating IP's actions.
func (s *FloatingIPsService) Actions(ctx context.Context, id int64, opts ActionListOpts) ([]Action, Meta, error) {
	return listPage(ctx, s.c, floatingIPPath(id, "/actions"), opts.values(), "actions", actionFromWire)
}

// AllActions returns every action of a floating IP matching opts.
func (s *FloatingIPsService) AllActions(ctx context.Context, id int64, opts ActionListOpts) ([]Action, error) {
	return listAll(ctx, s.c, floatingIPPath(id, "/actions"), opts.values(), "actions", actionFromWire)
}

// Assign assigns a floating IP to a server (id or name), moving it from its current server.
func (s *FloatingIPsService) Assign(ctx context.Context, id int64, server string) (*Action, error) {
	return actionReply(ctx, s.c, "POST", floatingIPPath(id, "/actions/assign"), wire.FloatingIPAssignInput{Server: server})
}

// Unassign unassigns a floating IP from its server.
func (s *FloatingIPsService) Unassign(ctx context.Context, id int64) (*Action, error) {
	return actionReply(ctx, s.c, "POST", floatingIPPath(id, "/actions/unassign"), nil)
}

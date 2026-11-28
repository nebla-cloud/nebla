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

// PrimaryIPStatus is the state of a primary IP.
type PrimaryIPStatus string

// Primary IP statuses.
const (
	PrimaryIPStatusActive    PrimaryIPStatus = "active"
	PrimaryIPStatusReleasing PrimaryIPStatus = "releasing"
)

// PrimaryIPServer identifies the server a primary IP is assigned to.
type PrimaryIPServer struct {
	ID   int64
	Name string
}

// PrimaryIP is a public address reserved for a server's own use.
type PrimaryIP struct {
	ID          int64
	Name        string
	Description string
	Labels      map[string]string
	Type        IPType
	// IP is the bare address.
	IP       string
	Location LocationRef
	// Server is the assigned server, nil when unassigned.
	Server *PrimaryIPServer
	// AutoDelete deletes the address when its server is deleted.
	AutoDelete bool
	Status     PrimaryIPStatus
	Created    time.Time
}

func primaryIPFromWire(w wire.PrimaryIP) PrimaryIP {
	ip := PrimaryIP{
		ID: w.ID, Name: w.Name, Description: w.Description, Labels: map[string]string(w.Labels),
		Type: IPType(w.Type), IP: w.IP, Location: locationRefFromWire(w.Location),
		AutoDelete: w.AutoDelete, Status: PrimaryIPStatus(w.Status), Created: w.Created,
	}
	if w.Server != nil {
		ip.Server = &PrimaryIPServer{ID: w.Server.ID, Name: w.Server.Name}
	}
	return ip
}

// PrimaryIPCreateOpts reserves a primary IP in a location.
type PrimaryIPCreateOpts struct {
	// Name is generated when empty.
	Name        string
	Description string
	Labels      map[string]string
	Type        IPType
	// Location is a location id or name.
	Location string
	// AutoDelete nil leaves the API default (true); Ptr(false) keeps the address after its server is deleted.
	AutoDelete *bool
}

// PrimaryIPUpdateOpts changes a primary IP; nil fields are not sent, and a non-nil empty Labels clears them.
type PrimaryIPUpdateOpts struct {
	Name        *string
	Description *string
	Labels      *map[string]string
	AutoDelete  *bool
}

// PrimaryIPListOpts filters a list of primary IPs.
type PrimaryIPListOpts struct {
	ListOpts
	Name          string
	LabelSelector string
	// Server is a server id or name.
	Server string
	// Location is a location id or name.
	Location string
	Type     IPType
}

func (o PrimaryIPListOpts) values() url.Values {
	q := o.ListOpts.values()
	setIf(q, "name", o.Name)
	setIf(q, "label_selector", o.LabelSelector)
	setIf(q, "server", o.Server)
	setIf(q, "location", o.Location)
	setIf(q, "type", string(o.Type))
	return q
}

type primaryIPCreateBody struct {
	Name        string            `json:"name,omitempty"`
	Description string            `json:"description,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	Type        IPType            `json:"type"`
	Location    string            `json:"location"`
	AutoDelete  *bool             `json:"auto_delete,omitempty"`
}

type primaryIPUpdateBody struct {
	Name        *string            `json:"name,omitempty"`
	Description *string            `json:"description,omitempty"`
	Labels      *map[string]string `json:"labels,omitempty"`
	AutoDelete  *bool              `json:"auto_delete,omitempty"`
}

// PrimaryIPsService works with primary IPs.
type PrimaryIPsService struct{ c *Client }

func primaryIPPath(id int64, suffix string) string {
	return "/primary_ips/" + strconv.FormatInt(id, 10) + suffix
}

// List returns one page of primary IPs.
func (s *PrimaryIPsService) List(ctx context.Context, opts PrimaryIPListOpts) ([]PrimaryIP, Meta, error) {
	return listPage(ctx, s.c, "/primary_ips", opts.values(), "primary_ips", primaryIPFromWire)
}

// All returns every primary IP matching opts.
func (s *PrimaryIPsService) All(ctx context.Context, opts PrimaryIPListOpts) ([]PrimaryIP, error) {
	return listAll(ctx, s.c, "/primary_ips", opts.values(), "primary_ips", primaryIPFromWire)
}

// Get returns one primary IP.
func (s *PrimaryIPsService) Get(ctx context.Context, id int64) (*PrimaryIP, error) {
	var w wire.PrimaryIP
	if err := s.c.do(ctx, "GET", primaryIPPath(id, ""), nil, nil, &w); err != nil {
		return nil, err
	}
	ip := primaryIPFromWire(w)
	return &ip, nil
}

// GetByName returns the primary IP with exactly this name.
func (s *PrimaryIPsService) GetByName(ctx context.Context, name string) (*PrimaryIP, error) {
	all := func(ctx context.Context) ([]PrimaryIP, error) {
		return s.All(ctx, PrimaryIPListOpts{Name: name})
	}
	return getByName(ctx, all, name, func(ip PrimaryIP) string { return ip.Name }, "primary IP")
}

// Create reserves a primary IP.
func (s *PrimaryIPsService) Create(ctx context.Context, opts PrimaryIPCreateOpts) (*PrimaryIP, error) {
	var w struct {
		PrimaryIP wire.PrimaryIP `json:"primary_ip"`
	}
	body := primaryIPCreateBody{Name: opts.Name, Description: opts.Description, Labels: opts.Labels, Type: opts.Type, Location: opts.Location, AutoDelete: opts.AutoDelete}
	if err := s.c.do(ctx, "POST", "/primary_ips", nil, body, &w); err != nil {
		return nil, err
	}
	ip := primaryIPFromWire(w.PrimaryIP)
	return &ip, nil
}

// Update changes a primary IP's name, description, labels or auto-delete.
func (s *PrimaryIPsService) Update(ctx context.Context, id int64, opts PrimaryIPUpdateOpts) (*PrimaryIP, error) {
	var w wire.PrimaryIP
	if err := s.c.do(ctx, "PUT", primaryIPPath(id, ""), nil, primaryIPUpdateBody(opts), &w); err != nil {
		return nil, err
	}
	ip := primaryIPFromWire(w)
	return &ip, nil
}

// Delete deletes an unassigned primary IP.
func (s *PrimaryIPsService) Delete(ctx context.Context, id int64) error {
	return s.c.do(ctx, "DELETE", primaryIPPath(id, ""), nil, nil, nil)
}

// Actions returns one page of a primary IP's actions.
func (s *PrimaryIPsService) Actions(ctx context.Context, id int64, opts ActionListOpts) ([]Action, Meta, error) {
	return listPage(ctx, s.c, primaryIPPath(id, "/actions"), opts.values(), "actions", actionFromWire)
}

// AllActions returns every action of a primary IP matching opts.
func (s *PrimaryIPsService) AllActions(ctx context.Context, id int64, opts ActionListOpts) ([]Action, error) {
	return listAll(ctx, s.c, primaryIPPath(id, "/actions"), opts.values(), "actions", actionFromWire)
}

// Assign assigns an unassigned primary IP to a stopped server (id or name); it takes effect when the server starts.
func (s *PrimaryIPsService) Assign(ctx context.Context, id int64, server string) (*Action, []string, error) {
	return s.actionWithWarnings(ctx, primaryIPPath(id, "/actions/assign"), wire.PrimaryIPAssignInput{Server: server})
}

// Unassign unassigns a primary IP from its stopped server; it takes effect when the server starts.
func (s *PrimaryIPsService) Unassign(ctx context.Context, id int64) (*Action, []string, error) {
	return s.actionWithWarnings(ctx, primaryIPPath(id, "/actions/unassign"), nil)
}

func (s *PrimaryIPsService) actionWithWarnings(ctx context.Context, path string, body any) (*Action, []string, error) {
	var w struct {
		Action   wire.Action `json:"action"`
		Warnings []string    `json:"warnings"`
	}
	if err := s.c.do(ctx, "POST", path, nil, body, &w); err != nil {
		return nil, nil, err
	}
	a := actionFromWire(w.Action)
	return &a, w.Warnings, nil
}

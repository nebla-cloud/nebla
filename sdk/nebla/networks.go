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

// NetworkServer is a server attached to a network.
type NetworkServer struct {
	// Server is the server id.
	Server int64
	Name   string
	IP     string
}

// Network is a private network.
type Network struct {
	ID       int64
	Name     string
	Labels   map[string]string
	Location LocationRef
	// IPRange is the IPv4 range in CIDR form.
	IPRange string
	Servers []NetworkServer
	Created time.Time
}

func networkFromWire(w wire.Network) Network {
	n := Network{
		ID: w.ID, Name: w.Name, Labels: map[string]string(w.Labels), Location: locationRefFromWire(w.Location),
		IPRange: w.IPRange, Created: w.Created,
	}
	for _, s := range w.Servers {
		n.Servers = append(n.Servers, NetworkServer{Server: s.Server, Name: s.Name, IP: s.IP})
	}
	return n
}

// NetworkCreateOpts creates a network.
type NetworkCreateOpts struct {
	Name string
	// Location is a location id or name.
	Location string
	// IPRange is an IPv4 CIDR; the first free 10.N.0.0/16 when empty.
	IPRange string
	Labels  map[string]string
}

// NetworkUpdateOpts changes a network; nil fields are not sent, and a non-nil empty Labels clears them.
type NetworkUpdateOpts struct {
	Name   *string
	Labels *map[string]string
}

// NetworkListOpts filters a list of networks.
type NetworkListOpts struct {
	ListOpts
	Name          string
	LabelSelector string
	// Location is a location id or name.
	Location string
}

func (o NetworkListOpts) values() url.Values {
	q := o.ListOpts.values()
	setIf(q, "name", o.Name)
	setIf(q, "label_selector", o.LabelSelector)
	setIf(q, "location", o.Location)
	return q
}

type networkCreateBody struct {
	Name     string            `json:"name"`
	Location string            `json:"location"`
	IPRange  string            `json:"ip_range,omitempty"`
	Labels   map[string]string `json:"labels,omitempty"`
}

type networkUpdateBody struct {
	Name   *string            `json:"name,omitempty"`
	Labels *map[string]string `json:"labels,omitempty"`
}

// NetworksService works with private networks.
type NetworksService struct{ c *Client }

func networkPath(id int64, suffix string) string {
	return "/networks/" + strconv.FormatInt(id, 10) + suffix
}

// List returns one page of networks.
func (s *NetworksService) List(ctx context.Context, opts NetworkListOpts) ([]Network, Meta, error) {
	return listPage(ctx, s.c, "/networks", opts.values(), "networks", networkFromWire)
}

// All returns every network matching opts.
func (s *NetworksService) All(ctx context.Context, opts NetworkListOpts) ([]Network, error) {
	return listAll(ctx, s.c, "/networks", opts.values(), "networks", networkFromWire)
}

// Get returns one network.
func (s *NetworksService) Get(ctx context.Context, id int64) (*Network, error) {
	var w wire.Network
	if err := s.c.do(ctx, "GET", networkPath(id, ""), nil, nil, &w); err != nil {
		return nil, err
	}
	n := networkFromWire(w)
	return &n, nil
}

// GetByName returns the network with exactly this name.
func (s *NetworksService) GetByName(ctx context.Context, name string) (*Network, error) {
	all := func(ctx context.Context) ([]Network, error) { return s.All(ctx, NetworkListOpts{Name: name}) }
	return getByName(ctx, all, name, func(n Network) string { return n.Name }, "network")
}

// Create creates a network.
func (s *NetworksService) Create(ctx context.Context, opts NetworkCreateOpts) (*Network, error) {
	var w struct {
		Network wire.Network `json:"network"`
	}
	body := networkCreateBody{Name: opts.Name, Location: opts.Location, IPRange: opts.IPRange, Labels: opts.Labels}
	if err := s.c.do(ctx, "POST", "/networks", nil, body, &w); err != nil {
		return nil, err
	}
	n := networkFromWire(w.Network)
	return &n, nil
}

// Update renames a network or replaces its labels.
func (s *NetworksService) Update(ctx context.Context, id int64, opts NetworkUpdateOpts) (*Network, error) {
	opts.Labels = clearable(opts.Labels)
	var w wire.Network
	if err := s.c.do(ctx, "PUT", networkPath(id, ""), nil, networkUpdateBody(opts), &w); err != nil {
		return nil, err
	}
	n := networkFromWire(w)
	return &n, nil
}

// Delete deletes a network that has no servers.
func (s *NetworksService) Delete(ctx context.Context, id int64) error {
	return s.c.do(ctx, "DELETE", networkPath(id, ""), nil, nil, nil)
}

// Actions returns one page of a network's actions.
func (s *NetworksService) Actions(ctx context.Context, id int64, opts ActionListOpts) ([]Action, Meta, error) {
	return listPage(ctx, s.c, networkPath(id, "/actions"), opts.values(), "actions", actionFromWire)
}

// AllActions returns every action of a network matching opts.
func (s *NetworksService) AllActions(ctx context.Context, id int64, opts ActionListOpts) ([]Action, error) {
	return listAll(ctx, s.c, networkPath(id, "/actions"), opts.values(), "actions", actionFromWire)
}

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

// ServerStatus is the lifecycle state of a server.
type ServerStatus string

// Server statuses.
const (
	ServerStatusInitializing ServerStatus = "initializing"
	ServerStatusStarting     ServerStatus = "starting"
	ServerStatusRunning      ServerStatus = "running"
	ServerStatusStopping     ServerStatus = "stopping"
	ServerStatusOff          ServerStatus = "off"
	ServerStatusRebuilding   ServerStatus = "rebuilding"
	ServerStatusDeleting     ServerStatus = "deleting"
	ServerStatusMissing      ServerStatus = "missing"
	ServerStatusError        ServerStatus = "error"
)

// ServerIP is an address assigned to a server.
type ServerIP struct {
	ID int64
	IP string
}

// ServerFloatingIP is a floating IP assigned to a server.
type ServerFloatingIP struct {
	ID int64
	IP string
}

// ServerPublicNet is a server's public addressing.
type ServerPublicNet struct {
	IPv4        *ServerIP
	IPv6        *ServerIP
	FloatingIPs []ServerFloatingIP
}

// NetworkGuestConfig holds commands to run inside a server so it uses a private network.
type NetworkGuestConfig struct {
	Apply   *string
	Persist *string
	Remove  *string
	Note    *string
}

// ServerPrivateNet is a private network a server is on.
type ServerPrivateNet struct {
	Network     int64
	Name        string
	IP          string
	GuestConfig *NetworkGuestConfig
}

// ServerSnapshot identifies the snapshot a server was built from.
type ServerSnapshot struct {
	ID   int64
	Name string
}

// ServerFirewall is a firewall in effect on a server and why it applies.
type ServerFirewall struct {
	ID   int64
	Name string
	// Via is "direct", or the label selector that applies the firewall.
	Via string
}

// ServerBackups is a server's backup state.
type ServerBackups struct {
	Enabled      bool
	Keep         int
	Count        int
	LastBackupAt *time.Time
	NextBackupAt *time.Time
}

// BackupSettings turns backups on or off and sets how many to keep (1-14).
type BackupSettings struct {
	Enabled bool
	Keep    int
}

// Server is a virtual machine.
type Server struct {
	ID                      int64
	Name                    string
	Status                  ServerStatus
	Location                LocationRef
	ServerType              ServerType
	Image                   *Image
	Snapshot                *ServerSnapshot
	OSFlavor                string
	OSVersion               string
	PublicNet               ServerPublicNet
	PrivateNet              []ServerPrivateNet
	Labels                  map[string]string
	DiskGB                  int
	Firewalls               []ServerFirewall
	Volumes                 []int64
	Backups                 ServerBackups
	Created                 time.Time
	IPChangeRerunsCloudInit bool
}

func guestConfigFromWire(w *wire.NetworkGuestConfig) *NetworkGuestConfig {
	if w == nil {
		return nil
	}
	return &NetworkGuestConfig{Apply: w.Apply, Persist: w.Persist, Remove: w.Remove, Note: w.Note}
}

func serverIPFromWire(w *wire.ServerIP) *ServerIP {
	if w == nil {
		return nil
	}
	return &ServerIP{ID: w.ID, IP: w.IP}
}

func serverFromWire(w wire.Server) Server {
	s := Server{
		ID: w.ID, Name: w.Name, Status: ServerStatus(w.Status),
		Location:   locationRefFromWire(w.Location),
		ServerType: serverTypeFromWire(w.ServerType),
		OSFlavor:   w.OsFlavor, OSVersion: w.OsVersion,
		PublicNet: ServerPublicNet{IPv4: serverIPFromWire(w.PublicNet.Ipv4), IPv6: serverIPFromWire(w.PublicNet.Ipv6)},
		Labels:    map[string]string(w.Labels),
		DiskGB:    w.DiskGb, Volumes: w.Volumes,
		Backups:                 ServerBackups{Enabled: w.Backups.Enabled, Keep: w.Backups.Keep, Count: w.Backups.Count, LastBackupAt: w.Backups.LastBackupAt, NextBackupAt: w.Backups.NextBackupAt},
		Created:                 w.Created,
		IPChangeRerunsCloudInit: w.IPChangeRerunsCloudInit,
	}
	if w.Image != nil {
		img := imageFromWire(*w.Image)
		s.Image = &img
	}
	if w.Snapshot != nil {
		s.Snapshot = &ServerSnapshot{ID: w.Snapshot.ID, Name: w.Snapshot.Name}
	}
	for _, f := range w.PublicNet.FloatingIps {
		s.PublicNet.FloatingIPs = append(s.PublicNet.FloatingIPs, ServerFloatingIP{ID: f.ID, IP: f.IP})
	}
	for _, p := range w.PrivateNet {
		s.PrivateNet = append(s.PrivateNet, ServerPrivateNet{Network: p.Network, Name: p.Name, IP: p.IP, GuestConfig: guestConfigFromWire(p.GuestConfig)})
	}
	for _, f := range w.Firewalls {
		s.Firewalls = append(s.Firewalls, ServerFirewall{ID: f.ID, Name: f.Name, Via: f.Via})
	}
	return s
}

// PublicIPChoice selects how a server gets a public address; nil leaves the API default.
type PublicIPChoice struct{ v any }

// NewPublicIP allocates a new address.
func NewPublicIP() *PublicIPChoice { return &PublicIPChoice{v: true} }

// NoPublicIP gives the server no address of this family.
func NoPublicIP() *PublicIPChoice { return &PublicIPChoice{v: false} }

// UsePrimaryIP uses an unassigned primary IP, by id or name.
func UsePrimaryIP(ref string) *PublicIPChoice { return &PublicIPChoice{v: ref} }

// MarshalJSON encodes the choice as a boolean or string.
func (p PublicIPChoice) MarshalJSON() ([]byte, error) { return json.Marshal(p.v) }

// ServerNetworkOpts joins a private network at creation; IP is optional.
type ServerNetworkOpts struct {
	Network, IP string
}

// ServerCreateOpts creates a server; give Image or Snapshot.
type ServerCreateOpts struct {
	Name, ServerType, Image, Snapshot, Location string
	SSHKeys                                     []string
	UserData                                    string
	PublicIPv4, PublicIPv6                      *PublicIPChoice
	Labels                                      map[string]string
	StartAfterCreate                            *bool
	Firewalls                                   []int64
	Backups                                     *BackupSettings
	Networks                                    []ServerNetworkOpts
}

// ServerCreateResult is the outcome of creating a server.
type ServerCreateResult struct {
	Server      Server
	Action      Action
	NextActions []Action
}

// ServerUpdateOpts changes a server; nil fields are not sent, and a non-nil empty Labels clears them.
type ServerUpdateOpts struct {
	Name    *string
	Labels  *map[string]string
	Backups *BackupSettings
}

// ServerListOpts filters a list of servers.
type ServerListOpts struct {
	ListOpts
	Name          string
	LabelSelector string
	Status        []ServerStatus
}

func (o ServerListOpts) values() url.Values {
	q := o.ListOpts.values()
	setIf(q, "name", o.Name)
	setIf(q, "label_selector", o.LabelSelector)
	for _, st := range o.Status {
		q.Add("status", string(st))
	}
	return q
}

type backupSettingsBody struct {
	Enabled bool `json:"enabled"`
	Keep    int  `json:"keep"`
}

func backupSettingsBodyOf(b *BackupSettings) *backupSettingsBody {
	if b == nil {
		return nil
	}
	return &backupSettingsBody{Enabled: b.Enabled, Keep: b.Keep}
}

type serverCreateBody struct {
	Name             string              `json:"name"`
	ServerType       string              `json:"server_type"`
	Image            string              `json:"image,omitempty"`
	Snapshot         string              `json:"snapshot,omitempty"`
	Location         string              `json:"location,omitempty"`
	SSHKeys          []string            `json:"ssh_keys,omitempty"`
	UserData         string              `json:"user_data,omitempty"`
	PublicNet        *serverPublicNetReq `json:"public_net,omitempty"`
	Labels           map[string]string   `json:"labels,omitempty"`
	StartAfterCreate *bool               `json:"start_after_create,omitempty"`
	Firewalls        []serverFirewallReq `json:"firewalls,omitempty"`
	Backups          *backupSettingsBody `json:"backups,omitempty"`
	Networks         []serverNetworkBody `json:"networks,omitempty"`
}

type serverPublicNetReq struct {
	IPv4 *PublicIPChoice `json:"ipv4,omitempty"`
	IPv6 *PublicIPChoice `json:"ipv6,omitempty"`
}

type serverFirewallReq struct {
	Firewall int64 `json:"firewall"`
}

type serverNetworkBody struct {
	Network string `json:"network"`
	IP      string `json:"ip,omitempty"`
}

func (o ServerCreateOpts) body() serverCreateBody {
	b := serverCreateBody{
		Name: o.Name, ServerType: o.ServerType, Image: o.Image, Snapshot: o.Snapshot, Location: o.Location,
		SSHKeys: o.SSHKeys, UserData: o.UserData, Labels: o.Labels, StartAfterCreate: o.StartAfterCreate,
		Backups: backupSettingsBodyOf(o.Backups),
	}
	if o.PublicIPv4 != nil || o.PublicIPv6 != nil {
		b.PublicNet = &serverPublicNetReq{IPv4: o.PublicIPv4, IPv6: o.PublicIPv6}
	}
	for _, id := range o.Firewalls {
		b.Firewalls = append(b.Firewalls, serverFirewallReq{Firewall: id})
	}
	for _, n := range o.Networks {
		b.Networks = append(b.Networks, serverNetworkBody{Network: n.Network, IP: n.IP})
	}
	return b
}

type serverUpdateBody struct {
	Name    *string             `json:"name,omitempty"`
	Labels  *map[string]string  `json:"labels,omitempty"`
	Backups *backupSettingsBody `json:"backups,omitempty"`
}

// ServersService works with servers.
type ServersService struct{ c *Client }

func serverPath(id int64, suffix string) string {
	return "/servers/" + strconv.FormatInt(id, 10) + suffix
}

// List returns one page of servers.
func (s *ServersService) List(ctx context.Context, opts ServerListOpts) ([]Server, Meta, error) {
	return listPage(ctx, s.c, "/servers", opts.values(), "servers", serverFromWire)
}

// All returns every server matching opts.
func (s *ServersService) All(ctx context.Context, opts ServerListOpts) ([]Server, error) {
	return listAll(ctx, s.c, "/servers", opts.values(), "servers", serverFromWire)
}

// Get returns one server.
func (s *ServersService) Get(ctx context.Context, id int64) (*Server, error) {
	var w wire.Server
	if err := s.c.do(ctx, "GET", serverPath(id, ""), nil, nil, &w); err != nil {
		return nil, err
	}
	v := serverFromWire(w)
	return &v, nil
}

// GetByName returns the server with exactly this name.
func (s *ServersService) GetByName(ctx context.Context, name string) (*Server, error) {
	all := func(ctx context.Context) ([]Server, error) { return s.All(ctx, ServerListOpts{Name: name}) }
	return getByName(ctx, all, name, func(v Server) string { return v.Name }, "server")
}

// Create creates a server; the returned actions run in the background.
func (s *ServersService) Create(ctx context.Context, opts ServerCreateOpts) (ServerCreateResult, error) {
	var w struct {
		Server      wire.Server   `json:"server"`
		Action      wire.Action   `json:"action"`
		NextActions []wire.Action `json:"next_actions"`
	}
	if err := s.c.do(ctx, "POST", "/servers", nil, opts.body(), &w); err != nil {
		return ServerCreateResult{}, err
	}
	res := ServerCreateResult{Server: serverFromWire(w.Server), Action: actionFromWire(w.Action)}
	for _, a := range w.NextActions {
		res.NextActions = append(res.NextActions, actionFromWire(a))
	}
	return res, nil
}

// Update renames a server, replaces its labels or changes its backup settings.
func (s *ServersService) Update(ctx context.Context, id int64, opts ServerUpdateOpts) (*Server, error) {
	opts.Labels = clearable(opts.Labels)
	var w wire.Server
	body := serverUpdateBody{Name: opts.Name, Labels: opts.Labels, Backups: backupSettingsBodyOf(opts.Backups)}
	if err := s.c.do(ctx, "PUT", serverPath(id, ""), nil, body, &w); err != nil {
		return nil, err
	}
	v := serverFromWire(w)
	return &v, nil
}

// Delete deletes a server and returns the delete action.
func (s *ServersService) Delete(ctx context.Context, id int64) (*Action, error) {
	return actionReply(ctx, s.c, "DELETE", serverPath(id, ""), nil)
}

// Actions returns one page of a server's actions.
func (s *ServersService) Actions(ctx context.Context, id int64, opts ActionListOpts) ([]Action, Meta, error) {
	return listPage(ctx, s.c, serverPath(id, "/actions"), opts.values(), "actions", actionFromWire)
}

// AllActions returns every action of a server matching opts.
func (s *ServersService) AllActions(ctx context.Context, id int64, opts ActionListOpts) ([]Action, error) {
	return listAll(ctx, s.c, serverPath(id, "/actions"), opts.values(), "actions", actionFromWire)
}

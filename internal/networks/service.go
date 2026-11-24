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

package networks

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/apierr"
	"github.com/nebla-cloud/nebla/internal/catalog"
	"github.com/nebla-cloud/nebla/internal/db"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/action"
	"github.com/nebla-cloud/nebla/internal/ent/ippool"
	"github.com/nebla-cloud/nebla/internal/ent/network"
	"github.com/nebla-cloud/nebla/internal/ent/predicate"
	"github.com/nebla-cloud/nebla/internal/ent/server"
	"github.com/nebla-cloud/nebla/internal/ent/servernetwork"
	"github.com/nebla-cloud/nebla/internal/ent/vnetslot"
	"github.com/nebla-cloud/nebla/internal/ext"
	"github.com/nebla-cloud/nebla/internal/jobs"
	"github.com/nebla-cloud/nebla/internal/labels"
	"github.com/nebla-cloud/nebla/internal/projects"
	"github.com/riverqueue/river"
)

const (
	MaxLabels = 64
	// firewallApply is the firewalls package's apply command; it never blocks a network change.
	firewallApply = "apply_firewalls"
)

// Publisher receives domain events; *ext.Bus implements it.
type Publisher interface {
	Publish(ctx context.Context, e ext.Event)
}

type Options struct {
	SQL      *sql.DB
	Ent      *ent.Client
	Catalog  *catalog.Service
	Projects *projects.Service
	// Jobs nil makes attach and detach fail with 503 service_unavailable.
	Jobs   *jobs.Jobs
	Logger *slog.Logger
	// Bus nil drops events.
	Bus Publisher
	// Now nil means time.Now.
	Now func() time.Time
}

type Service struct {
	o Options
	// thawed is when each location's cooling scan last ran.
	thawMu sync.Mutex
	thawed map[int64]time.Time
}

func New(o Options) *Service {
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	return &Service{o: o, thawed: map[int64]time.Time{}}
}

// Network is a private network as the project sees it.
type Network struct {
	ID       int64
	Name     string
	Labels   map[string]string
	Location catalog.LocationRef
	IPRange  netip.Prefix
	Servers  []Member
	Created  time.Time
}

// Member is a server on a network.
type Member struct {
	ServerID int64
	Name     string
	IP       netip.Addr
}

type CreateInput struct {
	Name, Location, IPRange string
	Labels                  map[string]string
}

// UpdateInput changes only the non-nil fields.
type UpdateInput struct {
	Name   *string
	Labels *map[string]string
}

type ListFilter struct {
	Name, Location, LabelSelector string
}

// CreateRef names a network, and optionally an address on it, for a server being created.
type CreateRef struct{ Network, IP string }

// PrivateNet is a server's address on one network.
type PrivateNet struct {
	NetworkID int64
	Network   string
	IP        netip.Addr
}

var nameRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

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

// ActionsOf matches the project's actions whose resources include the network.
func ActionsOf(projectID, id int64) predicate.Action {
	return resourceActions(projectID, id, ResourceType)
}

func resourceActions(projectID, id int64, typ string) predicate.Action {
	doc, _ := json.Marshal([]actions.Resource{{ID: id, Type: typ}})
	return action.And(action.ProjectID(projectID), func(s *entsql.Selector) {
		s.Where(entsql.P(func(b *entsql.Builder) {
			b.Ident(s.C(action.FieldResources)).WriteString(" @> ").Arg(string(doc)).WriteString("::jsonb")
		}))
	})
}

// withTx runs fn with a 10s lock timeout; a timeout becomes the busy error.
func (s *Service) withTx(ctx context.Context, fn func(tx *sql.Tx, etx *ent.Client) error) error {
	return lockErr(db.WithTx(ctx, s.o.SQL, func(tx *sql.Tx, etx *ent.Client) error {
		if _, err := tx.ExecContext(ctx, "SET LOCAL lock_timeout = '10s'"); err != nil {
			return err
		}
		return fn(tx, etx)
	}))
}

// poolState is the location's private network state, without looking at its slots.
func poolState(l *ent.Location) string {
	switch {
	case l.SdnZone == nil:
		return "not_set_up"
	case !l.SdnAuditOk:
		return "needs_setup"
	case l.FirewallEnabled == nil || !*l.FirewallEnabled:
		return "firewall_off"
	}
	return "ready"
}

// requireOpen refuses new networks and attaches unless the location is ready.
func requireOpen(l *ent.Location) error {
	switch poolState(l) {
	case "not_set_up", "needs_setup":
		return errNotSetUp
	case "firewall_off":
		return errPaused
	}
	return nil
}

// lockNetworks locks the project's networks in ascending id order and returns them by id.
func lockNetworks(ctx context.Context, tx *ent.Client, projectID int64, ids ...int64) (map[int64]*ent.Network, error) {
	ids = slices.Compact(slices.Sorted(slices.Values(ids)))
	rows, err := tx.Network.Query().Where(network.IDIn(ids...), network.ProjectID(projectID)).Order(network.ByID()).ForUpdate().Unique(false).All(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]*ent.Network, len(rows))
	for _, r := range rows {
		out[r.ID] = r
	}
	for _, id := range ids {
		if out[id] == nil {
			return nil, errNotFound
		}
	}
	return out, nil
}

// lockServer takes NO KEY UPDATE on the project's server; a transient status or a running action gives 409 locked.
func lockServer(ctx context.Context, tx *ent.Client, projectID, id int64) (*ent.Server, error) {
	q := tx.Server.Query().Where(server.ID(id), server.ProjectID(projectID)).Unique(false)
	q.Modify(func(s *entsql.Selector) { s.For(entsql.LockNoKeyUpdate) })
	row, err := q.Only(ctx)
	if ent.IsNotFound(err) {
		return nil, errServerNotFound
	}
	if err != nil {
		return nil, err
	}
	switch row.Status {
	case server.StatusInitializing, server.StatusRebuilding, server.StatusDeleting:
		return nil, errLocked
	}
	busy, err := tx.Action.Query().Where(resourceActions(projectID, id, jobs.ServerResource), action.StatusEQ(action.StatusRunning),
		action.CommandNEQ(firewallApply)).Exist(ctx)
	if err != nil {
		return nil, err
	}
	if busy {
		return nil, errLocked
	}
	return row, nil
}

// byRef finds the project's network by id, then by name.
func byRef(ctx context.Context, c *ent.Client, projectID int64, ref string) (*ent.Network, error) {
	if id, err := strconv.ParseInt(ref, 10, 64); err == nil {
		row, err := c.Network.Query().Where(network.ID(id), network.ProjectID(projectID)).Only(ctx)
		if err == nil {
			return row, nil
		}
		if !ent.IsNotFound(err) {
			return nil, err
		}
	}
	row, err := c.Network.Query().Where(network.Name(ref), network.ProjectID(projectID)).Only(ctx)
	return row, notFound(err)
}

// takenRanges are the location's public pool CIDRs and the project's networks there.
func takenRanges(ctx context.Context, tx *ent.Client, projectID, locationID int64) ([]netip.Prefix, error) {
	var out []netip.Prefix
	pools, err := tx.IPPool.Query().Where(ippool.LocationID(locationID)).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, p := range pools {
		if pfx, err := netip.ParsePrefix(p.Cidr); err == nil && pfx.Addr().Is4() {
			out = append(out, pfx)
		}
	}
	nets, err := tx.Network.Query().Where(network.ProjectID(projectID), network.LocationID(locationID)).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, n := range nets {
		if pfx, err := netip.ParsePrefix(n.IPRange); err == nil {
			out = append(out, pfx)
		}
	}
	return out, nil
}

// Create makes a network in a location and claims a free VNet for it.
func (s *Service) Create(ctx context.Context, projectID int64, in CreateInput) (Network, error) {
	if err := validName(in.Name); err != nil {
		return Network{}, err
	}
	if err := validLabels(in.Labels); err != nil {
		return Network{}, err
	}
	lbls := in.Labels
	if lbls == nil {
		lbls = map[string]string{}
	}
	if in.Location == "" {
		return Network{}, invalid("location is required")
	}
	var want netip.Prefix
	if in.IPRange != "" {
		var err error
		if want, err = ParseRange(in.IPRange); err != nil {
			return Network{}, invalid("ip_range must be an IPv4 range of /8 to /28 inside 10.0.0.0/8, 172.16.0.0/12 or 192.168.0.0/16")
		}
	}
	loc, err := s.o.Catalog.LocationByRef(ctx, in.Location)
	if err != nil {
		return Network{}, err
	}

	var id int64
	err = s.withTx(ctx, func(_ *sql.Tx, etx *ent.Client) error {
		if err := s.o.Projects.ReserveQuota(ctx, etx, projectID, projects.Usage{Networks: 1}); err != nil {
			return err
		}
		row, err := etx.Location.Get(ctx, loc.ID)
		if err != nil {
			return notFound(err)
		}
		if err := requireOpen(row); err != nil {
			return err
		}
		taken, err := takenRanges(ctx, etx, projectID, loc.ID)
		if err != nil {
			return err
		}
		rng := want
		if in.IPRange == "" {
			var ok bool
			if rng, ok = DefaultRange(taken); !ok {
				return errOverlap
			}
		} else if Overlaps(rng, taken) {
			return errOverlap
		}
		n, err := etx.Network.Create().SetProjectID(projectID).SetLocationID(loc.ID).SetName(in.Name).SetLabels(lbls).
			SetIPRange(rng.String()).Save(ctx)
		if err != nil {
			return writeErr(err)
		}
		id = n.ID
		return claimSlot(ctx, etx, loc.ID, n.ID)
	})
	if err != nil {
		return Network{}, err
	}
	n, err := s.Get(ctx, projectID, id)
	if err != nil {
		return Network{}, err
	}
	if s.o.Bus != nil {
		s.o.Bus.Publish(ctx, ext.Event{Kind: ext.NetworkCreated, ProjectID: projectID, ResourceType: ResourceType, ResourceID: id,
			At: time.Now(), Data: map[string]any{"location_id": n.Location.ID, "ip_range": n.IPRange.String()}})
	}
	return n, nil
}

// claimSlot gives the network the location's first free, healthy VNet; none left is the pool-empty error.
func claimSlot(ctx context.Context, etx *ent.Client, locationID, networkID int64) error {
	rows, err := etx.QueryContext(ctx, `SELECT id FROM vnet_slots WHERE location_id = $1 AND state = 'free' AND health = 'ok'
		ORDER BY vnet FOR UPDATE SKIP LOCKED LIMIT 1`, locationID)
	if err != nil {
		return err
	}
	var slotID int64
	found := rows.Next()
	if found {
		err = rows.Scan(&slotID)
	}
	if cerr := rows.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if !found {
		return errPoolEmpty
	}
	return etx.VNetSlot.UpdateOneID(slotID).SetState(vnetslot.StateInUse).SetNetworkID(networkID).Exec(ctx)
}

func (s *Service) get(ctx context.Context, projectID, id int64) (*ent.Network, error) {
	row, err := s.o.Ent.Network.Query().Where(network.ID(id), network.ProjectID(projectID)).Only(ctx)
	return row, notFound(err)
}

func (s *Service) Get(ctx context.Context, projectID, id int64) (Network, error) {
	row, err := s.get(ctx, projectID, id)
	if err != nil {
		return Network{}, err
	}
	out, err := s.hydrate(ctx, []*ent.Network{row})
	if err != nil {
		return Network{}, err
	}
	return out[0], nil
}

// List filters the project's networks; an unknown location matches nothing.
func (s *Service) List(ctx context.Context, projectID int64, f ListFilter, page, perPage int) ([]Network, int, error) {
	preds, err := LabelSelector(f.LabelSelector)
	if err != nil {
		return nil, 0, err
	}
	q := s.o.Ent.Network.Query().Where(network.ProjectID(projectID)).Where(preds...)
	if f.Name != "" {
		q = q.Where(network.Name(f.Name))
	}
	if f.Location != "" {
		loc, err := s.o.Catalog.LocationByRef(ctx, f.Location)
		var ae apierr.E
		if errors.As(err, &ae) && ae.Status == http.StatusNotFound {
			return []Network{}, 0, nil
		}
		if err != nil {
			return nil, 0, err
		}
		q = q.Where(network.LocationID(loc.ID))
	}
	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	page, perPage = max(page, 1), max(perPage, 1)
	rows, err := q.Order(network.ByID()).Offset((page - 1) * perPage).Limit(perPage).All(ctx)
	if err != nil {
		return nil, 0, err
	}
	out, err := s.hydrate(ctx, rows)
	return out, total, err
}

func (s *Service) hydrate(ctx context.Context, rows []*ent.Network) ([]Network, error) {
	ids := make([]int64, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	atts, err := s.o.Ent.ServerNetwork.Query().Where(servernetwork.NetworkIDIn(ids...)).WithServer().Order(servernetwork.ByID()).All(ctx)
	if err != nil {
		return nil, err
	}
	members := map[int64][]Member{}
	for _, a := range atts {
		ip, err := netip.ParseAddr(a.IP)
		if err != nil {
			return nil, fmt.Errorf("network %d: %w", a.NetworkID, err)
		}
		members[a.NetworkID] = append(members[a.NetworkID], Member{ServerID: a.ServerID, Name: a.Edges.Server.Name, IP: ip})
	}
	locs := map[int64]catalog.Location{}
	out := make([]Network, len(rows))
	for i, r := range rows {
		loc, ok := locs[r.LocationID]
		if !ok {
			if loc, err = s.o.Catalog.GetLocation(ctx, r.LocationID); err != nil {
				return nil, err
			}
			locs[r.LocationID] = loc
		}
		rng, err := netip.ParsePrefix(r.IPRange)
		if err != nil {
			return nil, fmt.Errorf("network %d: %w", r.ID, err)
		}
		lbls := r.Labels
		if lbls == nil {
			lbls = map[string]string{}
		}
		srv := members[r.ID]
		if srv == nil {
			srv = []Member{}
		}
		out[i] = Network{ID: r.ID, Name: r.Name, Labels: lbls, Location: loc.Ref(), IPRange: rng, Servers: srv, Created: r.CreatedAt}
	}
	return out, nil
}

// Update changes the name and labels.
func (s *Service) Update(ctx context.Context, projectID, id int64, in UpdateInput) (Network, error) {
	if in.Name != nil {
		if err := validName(*in.Name); err != nil {
			return Network{}, err
		}
	}
	var lbls map[string]string
	if in.Labels != nil {
		if lbls = *in.Labels; lbls == nil {
			lbls = map[string]string{}
		}
		if err := validLabels(lbls); err != nil {
			return Network{}, err
		}
	}
	err := s.withTx(ctx, func(_ *sql.Tx, etx *ent.Client) error {
		if err := projects.LockProject(ctx, etx, projectID); err != nil {
			return notFound(err)
		}
		if _, err := lockNetworks(ctx, etx, projectID, id); err != nil {
			return err
		}
		u := etx.Network.UpdateOneID(id)
		if in.Name != nil {
			u.SetName(*in.Name)
		}
		if lbls != nil {
			u.SetLabels(lbls)
		}
		return writeErr(u.Exec(ctx))
	})
	if err != nil {
		return Network{}, err
	}
	return s.Get(ctx, projectID, id)
}

// Delete removes a network nothing is attached to and leaves its VNet cooling.
func (s *Service) Delete(ctx context.Context, projectID, id int64) error {
	var locID int64
	err := s.withTx(ctx, func(_ *sql.Tx, etx *ent.Client) error {
		if err := projects.LockProject(ctx, etx, projectID); err != nil {
			return notFound(err)
		}
		nets, err := lockNetworks(ctx, etx, projectID, id)
		if err != nil {
			return err
		}
		locID = nets[id].LocationID
		atts, err := etx.ServerNetwork.Query().Where(servernetwork.NetworkID(id)).WithServer().All(ctx)
		if err != nil {
			return err
		}
		if len(atts) > 0 {
			names := make([]string, len(atts))
			for i, a := range atts {
				names[i] = a.Edges.Server.Name
			}
			slices.Sort(names)
			return detachFirst(names)
		}
		if err := etx.VNetSlot.Update().Where(vnetslot.NetworkID(id)).SetState(vnetslot.StateCooling).ClearNetworkID().Exec(ctx); err != nil {
			return err
		}
		return etx.Network.DeleteOneID(id).Exec(ctx)
	})
	if err != nil {
		return err
	}
	if s.o.Bus != nil {
		s.o.Bus.Publish(ctx, ext.Event{Kind: ext.NetworkDeleted, ProjectID: projectID, ResourceType: ResourceType, ResourceID: id,
			At: time.Now(), Data: map[string]any{"location_id": locID}})
	}
	return nil
}

// Actions lists the network's actions; it must exist in the project.
func (s *Service) Actions(ctx context.Context, projectID, id int64, f actions.ListFilter, page, perPage int) ([]actions.Action, int, error) {
	if _, err := s.get(ctx, projectID, id); err != nil {
		return nil, 0, err
	}
	f.Where = append(f.Where, ActionsOf(projectID, id))
	return actions.List(ctx, s.o.Ent, projectID, f, page, perPage)
}

// newMAC returns a free BC:24:11 address, the scheme servers use for net0.
func newMAC(ctx context.Context, tx *ent.Client) (string, error) {
	for range 16 {
		b := make([]byte, 3)
		if _, err := rand.Read(b); err != nil {
			return "", err
		}
		mac := fmt.Sprintf("BC:24:11:%02X:%02X:%02X", b[0], b[1], b[2])
		taken, err := tx.Server.Query().Where(server.MACEqualFold(mac)).Exist(ctx)
		if err != nil {
			return "", err
		}
		if !taken {
			if taken, err = tx.ServerNetwork.Query().Where(servernetwork.MACEqualFold(mac)).Exist(ctx); err != nil {
				return "", err
			}
		}
		if !taken {
			return mac, nil
		}
	}
	return "", errors.New("no free MAC address")
}

// addRow checks that srv can join n and inserts its attaching row on the lowest free NIC and address.
func addRow(ctx context.Context, etx *ent.Client, loc *ent.Location, n *ent.Network, srv *ent.Server, ip string) (*ent.ServerNetwork, error) {
	if srv.LocationID != n.LocationID {
		return nil, errOtherLocation
	}
	mine, err := etx.ServerNetwork.Query().Where(servernetwork.ServerID(srv.ID)).All(ctx)
	if err != nil {
		return nil, err
	}
	nics := map[int]bool{}
	for _, r := range mine {
		if r.NetworkID == n.ID {
			return nil, errAlready
		}
		nics[r.Nic] = true
	}
	if len(mine) >= MaxNICs {
		return nil, errNICLimit
	}
	slot, err := etx.VNetSlot.Query().Where(vnetslot.NetworkID(n.ID)).Only(ctx)
	if ent.IsNotFound(err) || (err == nil && (slot.State != vnetslot.StateInUse || slot.Health != vnetslot.HealthOk)) {
		return nil, errUnavailable
	}
	if err != nil {
		return nil, err
	}
	if err := requireOpen(loc); err != nil {
		return nil, err
	}
	rng, err := netip.ParsePrefix(n.IPRange)
	if err != nil {
		return nil, fmt.Errorf("network %d: %w", n.ID, err)
	}
	used, err := etx.ServerNetwork.Query().Where(servernetwork.NetworkID(n.ID)).Select(servernetwork.FieldIP).Strings(ctx)
	if err != nil {
		return nil, err
	}
	addrs := make([]netip.Addr, 0, len(used))
	for _, u := range used {
		if a, err := netip.ParseAddr(u); err == nil {
			addrs = append(addrs, a)
		}
	}
	var addr netip.Addr
	if ip != "" {
		if addr, err = netip.ParseAddr(ip); err != nil || !Usable(rng, addr) {
			return nil, errBadIP
		}
		if slices.Contains(addrs, addr) {
			return nil, errIPTaken
		}
	} else {
		var ok bool
		if addr, ok = NextFree(rng, addrs); !ok {
			return nil, errNoFreeIP
		}
	}
	nic := 1
	for nics[nic] {
		nic++
	}
	mac, err := newMAC(ctx, etx)
	if err != nil {
		return nil, err
	}
	row, err := etx.ServerNetwork.Create().SetServerID(srv.ID).SetNetworkID(n.ID).SetIP(addr.String()).SetMAC(mac).SetNic(nic).
		SetState(servernetwork.StateAttaching).Save(ctx)
	return row, writeErr(err)
}

func attachResources(serverID, networkID int64) []actions.Resource {
	return []actions.Resource{{ID: serverID, Type: jobs.ServerResource}, {ID: networkID, Type: ResourceType}}
}

// guestAt returns the guest commands for the server's NIC at index, read inside the caller's transaction.
func guestAt(ctx context.Context, etx *ent.Client, srv *ent.Server, index int, networkID int64, attach bool) (*GuestCommands, error) {
	nics, err := PrivateNICs(ctx, etx, srv.ID)
	if err != nil {
		return nil, err
	}
	flavor, err := FlavorOf(ctx, etx, srv)
	if err != nil {
		return nil, err
	}
	for _, n := range nics {
		if n.Index == index {
			return GuestFor(srv, n, networkID, flavor, attach), nil
		}
	}
	return nil, nil
}

// Attach joins the project's server to a network (by id or name) and queues the hot-plug.
func (s *Service) Attach(ctx context.Context, projectID, serverID int64, networkRef, ip string) (actions.Action, *GuestCommands, error) {
	if s.o.Jobs == nil {
		return actions.Action{}, nil, errNoJobs
	}
	n, err := byRef(ctx, s.o.Ent, projectID, networkRef)
	if err != nil {
		return actions.Action{}, nil, err
	}
	var act actions.Action
	var guest *GuestCommands
	err = s.withTx(ctx, func(tx *sql.Tx, etx *ent.Client) error {
		if err := projects.LockProject(ctx, etx, projectID); err != nil {
			return notFound(err)
		}
		nets, err := lockNetworks(ctx, etx, projectID, n.ID)
		if err != nil {
			return err
		}
		srv, err := lockServer(ctx, etx, projectID, serverID)
		if err != nil {
			return err
		}
		loc, err := etx.Location.Get(ctx, nets[n.ID].LocationID)
		if err != nil {
			return notFound(err)
		}
		row, err := addRow(ctx, etx, loc, nets[n.ID], srv, ip)
		if err != nil {
			return err
		}
		if guest, err = guestAt(ctx, etx, srv, row.Nic, n.ID, true); err != nil {
			return err
		}
		act, err = s.o.Jobs.StartAction(ctx, tx, etx, projectID, KindAttach, attachResources(serverID, n.ID), func(actionID int64) river.JobArgs {
			return AttachArgs{ActionID: actionID, ServerID: serverID, NetworkID: n.ID}
		}, nil)
		return err
	})
	return act, guest, err
}

// Detach starts removing the server from a network; it works while private networks are paused.
func (s *Service) Detach(ctx context.Context, projectID, serverID int64, networkRef string) (actions.Action, *GuestCommands, error) {
	if s.o.Jobs == nil {
		return actions.Action{}, nil, errNoJobs
	}
	n, err := byRef(ctx, s.o.Ent, projectID, networkRef)
	if err != nil {
		return actions.Action{}, nil, err
	}
	var act actions.Action
	var guest *GuestCommands
	err = s.withTx(ctx, func(tx *sql.Tx, etx *ent.Client) error {
		if err := projects.LockProject(ctx, etx, projectID); err != nil {
			return notFound(err)
		}
		if _, err := lockNetworks(ctx, etx, projectID, n.ID); err != nil {
			return err
		}
		srv, err := lockServer(ctx, etx, projectID, serverID)
		if err != nil {
			return err
		}
		row, err := etx.ServerNetwork.Query().Where(servernetwork.ServerID(serverID), servernetwork.NetworkID(n.ID)).Only(ctx)
		if ent.IsNotFound(err) {
			return errNotOn
		}
		if err != nil {
			return err
		}
		if row.State == servernetwork.StateAttaching {
			return errLocked
		}
		if guest, err = guestAt(ctx, etx, srv, row.Nic, n.ID, false); err != nil {
			return err
		}
		if err := etx.ServerNetwork.UpdateOneID(row.ID).SetState(servernetwork.StateDetaching).Exec(ctx); err != nil {
			return err
		}
		act, err = s.o.Jobs.StartAction(ctx, tx, etx, projectID, KindDetach, attachResources(serverID, n.ID), func(actionID int64) river.JobArgs {
			return DetachArgs{ActionID: actionID, ServerID: serverID, NetworkID: n.ID}
		}, nil)
		return err
	})
	return act, guest, err
}

// ReserveForCreate adds attaching rows, in order, for a server being created. The caller holds the project lock and the server row exists.
func (s *Service) ReserveForCreate(ctx context.Context, etx *ent.Client, projectID, locationID, serverID int64, refs []CreateRef) error {
	if len(refs) == 0 {
		return nil
	}
	if len(refs) > MaxNICs {
		return errNICLimit
	}
	picked := make([]*ent.Network, len(refs))
	ids := make([]int64, len(refs))
	for i, r := range refs {
		n, err := byRef(ctx, etx, projectID, r.Network)
		if err != nil {
			return err
		}
		picked[i], ids[i] = n, n.ID
	}
	locked, err := lockNetworks(ctx, etx, projectID, ids...)
	if err != nil {
		return err
	}
	srv, err := etx.Server.Get(ctx, serverID)
	if err != nil {
		return notFound(err)
	}
	if srv.LocationID != locationID {
		return errOtherLocation
	}
	loc, err := etx.Location.Get(ctx, locationID)
	if err != nil {
		return notFound(err)
	}
	for i, r := range refs {
		if _, err := addRow(ctx, etx, loc, locked[picked[i].ID], srv, r.IP); err != nil {
			return err
		}
	}
	return nil
}

// ForServers maps each server to its private networks, in NIC order.
func (s *Service) ForServers(ctx context.Context, c *ent.Client, ids []int64) (map[int64][]PrivateNet, error) {
	out := map[int64][]PrivateNet{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := c.ServerNetwork.Query().Where(servernetwork.ServerIDIn(ids...)).WithNetwork().
		Order(servernetwork.ByServerID(), servernetwork.ByNic()).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		ip, err := netip.ParseAddr(r.IP)
		if err != nil {
			return nil, fmt.Errorf("server %d: %w", r.ServerID, err)
		}
		out[r.ServerID] = append(out[r.ServerID], PrivateNet{NetworkID: r.NetworkID, Network: r.Edges.Network.Name, IP: ip})
	}
	return out, nil
}

// Available reports whether the location's private networks are open: set up, audited and firewalled.
func (s *Service) Available(ctx context.Context, locationID int64) (bool, error) {
	loc, err := s.o.Ent.Location.Get(ctx, locationID)
	if err != nil {
		return false, notFound(err)
	}
	if poolState(loc) != "ready" {
		return false, nil
	}
	return true, nil
}

// LocationInUseWith refuses the delete while the location has networks or cooling slots; otherwise it drops its idle slots.
func LocationInUseWith(next func(context.Context, *ent.Client, int64) (bool, error)) func(context.Context, *ent.Client, int64) (bool, error) {
	return func(ctx context.Context, tx *ent.Client, locationID int64) (bool, error) {
		if busy, err := next(ctx, tx, locationID); err != nil || busy {
			return busy, err
		}
		if has, err := tx.Network.Query().Where(network.LocationID(locationID)).Exist(ctx); err != nil || has {
			if err == nil {
				err = errLocationHasNetworks
			}
			return false, err
		}
		cooling, err := tx.VNetSlot.Query().Where(vnetslot.LocationID(locationID), vnetslot.StateEQ(vnetslot.StateCooling)).
			Order(vnetslot.ByID()).First(ctx)
		if err == nil {
			return false, apierr.New(http.StatusConflict, "location_in_use",
				fmt.Sprintf("Private networks in this location are still being released (%s: %s).", cooling.Vnet, cooling.Reason))
		}
		if !ent.IsNotFound(err) {
			return false, err
		}
		_, err = tx.VNetSlot.Delete().Where(vnetslot.LocationID(locationID), vnetslot.NetworkIDIsNil()).Exec(ctx)
		return false, err
	}
}

// HasWith reports next's answer, or whether the project still owns networks.
func HasWith(next projects.HasServersFunc) projects.HasServersFunc {
	return func(ctx context.Context, tx *ent.Client, projectID int64) (bool, error) {
		busy, err := next(ctx, tx, projectID)
		if err != nil || busy {
			return busy, err
		}
		return tx.Network.Query().Where(network.ProjectID(projectID)).Exist(ctx)
	}
}

// UsageWith adds the project's networks to next's usage.
func UsageWith(next projects.UsageFunc) projects.UsageFunc {
	return func(ctx context.Context, tx *ent.Client, projectID int64) (projects.Usage, error) {
		u, err := next(ctx, tx, projectID)
		if err != nil {
			return projects.Usage{}, err
		}
		n, err := tx.Network.Query().Where(network.ProjectID(projectID)).Count(ctx)
		if err != nil {
			return projects.Usage{}, err
		}
		u.Networks += n
		return u, nil
	}
}

func labelsContain(k, v string) predicate.Network {
	doc, _ := json.Marshal(map[string]string{k: v})
	return func(s *entsql.Selector) {
		s.Where(entsql.P(func(b *entsql.Builder) {
			b.Ident(s.C(network.FieldLabels)).WriteString(" @> ").Arg(string(doc)).WriteString("::jsonb")
		}))
	}
}

func labelsHave(k string) predicate.Network {
	return func(s *entsql.Selector) {
		s.Where(entsql.P(func(b *entsql.Builder) {
			b.WriteString("jsonb_exists(").Ident(s.C(network.FieldLabels)).Comma().Arg(k).WriteString(")")
		}))
	}
}

// LabelSelector converts the shared selector terms into network predicates.
func LabelSelector(sel string) ([]predicate.Network, error) {
	if strings.TrimSpace(sel) == "" {
		return nil, nil
	}
	terms, err := labels.Parse(sel)
	if err != nil {
		return nil, err
	}
	out := make([]predicate.Network, 0, len(terms))
	for _, term := range terms {
		switch term.Op {
		case labels.Has:
			out = append(out, labelsHave(term.Key))
		case labels.Lacks:
			out = append(out, network.Not(labelsHave(term.Key)))
		case labels.Eq:
			out = append(out, labelsContain(term.Key, term.Value))
		case labels.Ne:
			out = append(out, network.Not(labelsContain(term.Key, term.Value)))
		}
	}
	return out, nil
}

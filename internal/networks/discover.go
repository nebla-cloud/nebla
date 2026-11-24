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
	"database/sql"
	"errors"
	"maps"
	"net"
	"net/url"
	"slices"
	"strings"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/location"
	"github.com/nebla-cloud/nebla/internal/ent/node"
	"github.com/nebla-cloud/nebla/internal/ent/vnetslot"
	"github.com/nebla-cloud/nebla/internal/pve"
)

// PoolStatus is a location's private network pool as admins see it.
type PoolStatus struct {
	// State is not_set_up, needs_setup, firewall_off or ready.
	State                     string
	Zone                      *string
	Prefix                    string
	Available, InUse, Cooling int
	// Unusable lists slots that can't be handed out, including VNets that have gone.
	Unusable []Candidate
	// Degraded lists in-use slots that fail a check.
	Degraded []Candidate
}

// Pool reports the location's pool state and slot counts.
func (s *Service) Pool(ctx context.Context, locationID int64) (PoolStatus, error) {
	loc, err := s.o.Ent.Location.Get(ctx, locationID)
	if err != nil {
		return PoolStatus{}, notFound(err)
	}
	slots, err := s.o.Ent.VNetSlot.Query().Where(vnetslot.LocationID(locationID)).WithNetwork().Order(vnetslot.ByVnet()).All(ctx)
	if err != nil {
		return PoolStatus{}, err
	}
	st := PoolStatus{State: poolState(loc), Zone: loc.SdnZone, Prefix: loc.VnetPrefix, Unusable: []Candidate{}, Degraded: []Candidate{}}
	for _, sl := range slots {
		c := Candidate{VNet: sl.Vnet, Zone: sl.Zone, MTU: sl.Mtu, State: string(sl.State), Reason: sl.Reason}
		if sl.Edges.Network != nil {
			c.Network = sl.Edges.Network.Name
		}
		switch {
		case sl.State == vnetslot.StateFree && sl.Health == vnetslot.HealthOk:
			st.Available++
		case sl.State == vnetslot.StateInUse || (sl.State == vnetslot.StateGone && sl.NetworkID != nil):
			st.InUse++
			if sl.Health == vnetslot.HealthDegraded || sl.State == vnetslot.StateGone {
				st.Degraded = append(st.Degraded, c)
			}
		case sl.State == vnetslot.StateCooling:
			st.Cooling++
		case sl.State == vnetslot.StateUnusable || sl.State == vnetslot.StateGone:
			st.Unusable = append(st.Unusable, c)
		}
	}
	return st, nil
}

// SetLocationSDN sets the location's SDN zone (nil turns private networks off) and VNet prefix, then discovers its VNets.
func (s *Service) SetLocationSDN(ctx context.Context, locationID int64, zone *string, prefix string) (PoolStatus, error) {
	if zone != nil && strings.TrimSpace(*zone) == "" {
		zone = nil
	}
	if !ValidPrefix(prefix) {
		return PoolStatus{}, invalid("vnet_prefix must be 1 to 4 lowercase letters or digits, starting with a letter")
	}
	err := s.withTx(ctx, func(_ *sql.Tx, etx *ent.Client) error {
		own, err := etx.Location.Get(ctx, locationID)
		if err != nil {
			return notFound(err)
		}
		// Every location is locked in id order, so two admins can't take one zone together
		// and a running Discover of this location sees the change.
		loc, peers, err := lockCluster(ctx, etx, own)
		if err != nil {
			return err
		}
		for _, p := range peers {
			if zone != nil && p.SdnZone != nil && *p.SdnZone == *zone {
				return errZoneTaken
			}
		}
		sameZone := (zone == nil) == (loc.SdnZone == nil) && (zone == nil || *zone == *loc.SdnZone)
		if sameZone && prefix == loc.VnetPrefix {
			return nil
		}
		slots, err := etx.VNetSlot.Query().Where(vnetslot.LocationID(locationID)).Order(vnetslot.ByID()).ForUpdate().Unique(false).All(ctx)
		if err != nil {
			return err
		}
		for _, sl := range slots {
			if sl.State == vnetslot.StateInUse || sl.State == vnetslot.StateCooling || sl.NetworkID != nil {
				return errInUse
			}
		}
		if _, err := etx.VNetSlot.Delete().Where(vnetslot.LocationID(locationID)).Exec(ctx); err != nil {
			return err
		}
		u := etx.Location.UpdateOneID(locationID).SetVnetPrefix(prefix).SetSdnAuditOk(false)
		if zone == nil {
			u.ClearSdnZone()
		} else {
			u.SetSdnZone(*zone)
		}
		return u.Exec(ctx)
	})
	if err != nil {
		return PoolStatus{}, err
	}
	if zone != nil {
		pc, err := s.o.Catalog.ProxmoxFor(ctx, locationID)
		if err != nil {
			return PoolStatus{}, err
		}
		if err := s.Discover(ctx, pc, locationID); err != nil {
			return PoolStatus{}, err
		}
	}
	return s.Pool(ctx, locationID)
}

// Discover reads the location's SDN zone from Proxmox and brings its slots in line.
func (s *Service) Discover(ctx context.Context, pc pve.Client, locationID int64) error {
	loc, err := s.o.Ent.Location.Get(ctx, locationID)
	if err != nil {
		return notFound(err)
	}
	if loc.SdnZone == nil {
		return nil
	}
	perms, err := pc.Permissions(ctx, "/sdn")
	if err != nil {
		return err
	}
	audit := perms["/sdn"]["SDN.Audit"] == 1
	if audit != loc.SdnAuditOk {
		if err := s.o.Ent.Location.UpdateOneID(locationID).SetSdnAuditOk(audit).Exec(ctx); err != nil {
			return err
		}
	}
	if !audit {
		return nil
	}
	zones, err := pc.SDNZones(ctx)
	if err != nil {
		return err
	}
	vnets, err := pc.SDNVNets(ctx)
	if err != nil {
		return err
	}
	nodes, err := pc.Nodes(ctx)
	if err != nil {
		return err
	}
	var zone *pve.SDNZone
	for i := range zones {
		if zones[i].Zone == *loc.SdnZone {
			zone = &zones[i]
		}
	}
	seen := map[string]found{}
	if zone != nil {
		for _, v := range vnets {
			if v.Zone != zone.Zone || !strings.HasPrefix(v.VNet, loc.VnetPrefix) {
				continue
			}
			subnets, err := pc.SDNSubnets(ctx, v.VNet)
			if err != nil {
				return err
			}
			ok, reason := Check(*zone, v, subnets, len(nodes))
			c := Candidate{VNet: v.VNet, Zone: zone.Zone, MTU: MTUFor(*zone), State: string(vnetslot.StateFree), Reason: reason}
			if !ok {
				c.State = string(vnetslot.StateUnusable)
			}
			seen[v.VNet] = found{cand: c, ok: ok, zoneTyp: zone.Type}
		}
	}
	if discoverHook != nil {
		discoverHook("read")
	}
	return s.withTx(ctx, func(_ *sql.Tx, etx *ent.Client) error {
		cur, peers, err := lockCluster(ctx, etx, loc)
		if err != nil {
			return err
		}
		if cur.SdnZone == nil || *cur.SdnZone != *loc.SdnZone || cur.VnetPrefix != loc.VnetPrefix {
			return nil
		}
		slots, err := etx.VNetSlot.Query().Where(vnetslot.LocationID(locationID)).Order(vnetslot.ByID()).All(ctx)
		if err != nil {
			return err
		}
		peerUsed, err := peerVNets(ctx, etx, peers, seen)
		if err != nil {
			return err
		}
		have := map[string]bool{}
		mine := map[string]*ent.VNetSlot{}
		for _, sl := range slots {
			mine[sl.Vnet] = sl
		}
		for name, by := range peerUsed {
			if sl := mine[name]; sl != nil && sl.NetworkID != nil {
				continue
			}
			f := seen[name]
			f.cand.State, f.cand.Reason, f.ok = string(vnetslot.StateUnusable), "used by location "+by, false
			seen[name] = f
		}
		var dirty []int64
		for _, sl := range slots {
			have[sl.Vnet] = true
			if _, ok := wanted(sl, seen[sl.Vnet]); ok {
				dirty = append(dirty, sl.ID)
			}
		}
		for _, name := range slices.Sorted(maps.Keys(seen)) {
			if have[name] {
				continue
			}
			f := seen[name]
			err := etx.VNetSlot.Create().SetLocationID(locationID).SetVnet(name).SetZone(f.cand.Zone).SetZoneType(f.zoneTyp).
				SetMtu(f.cand.MTU).SetState(vnetslot.State(f.cand.State)).SetReason(f.cand.Reason).
				OnConflictColumns(vnetslot.FieldLocationID, vnetslot.FieldVnet).DoNothing().Exec(ctx)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}
		if len(dirty) == 0 {
			return nil
		}
		// Only rows that change are locked, in id order, and re-read so a claim made since isn't overwritten.
		fresh, err := etx.VNetSlot.Query().Where(vnetslot.IDIn(dirty...)).Order(vnetslot.ByID()).ForUpdate().Unique(false).All(ctx)
		if err != nil {
			return err
		}
		for _, sl := range fresh {
			up, ok := wanted(sl, seen[sl.Vnet])
			if !ok {
				continue
			}
			if err := up(etx.VNetSlot.UpdateOneID(sl.ID)).Exec(ctx); err != nil {
				return err
			}
		}
		if discoverHook != nil {
			discoverHook("write")
		}
		return nil
	})
}

// lockCluster locks every location in id order and returns the one with own's id and the others on its cluster.
func lockCluster(ctx context.Context, etx *ent.Client, own *ent.Location) (*ent.Location, []*ent.Location, error) {
	all, err := etx.Location.Query().Order(location.ByID()).Unique(false).Modify(noKeyUpdate).All(ctx)
	if err != nil {
		return nil, nil, err
	}
	rows, err := etx.Node.Query().Select(node.FieldLocationID, node.FieldName).All(ctx)
	if err != nil {
		return nil, nil, err
	}
	names := map[int64]map[string]bool{}
	for _, r := range rows {
		if names[r.LocationID] == nil {
			names[r.LocationID] = map[string]bool{}
		}
		names[r.LocationID][r.Name] = true
	}
	var cur *ent.Location
	for _, l := range all {
		if l.ID == own.ID {
			cur = l
		}
	}
	if cur == nil {
		return nil, nil, errNotFound
	}
	var peers []*ent.Location
	for _, l := range all {
		if l.ID != cur.ID && sameCluster(cur, l, names[cur.ID], names[l.ID]) {
			peers = append(peers, l)
		}
	}
	return cur, peers, nil
}

// sameCluster reports whether two locations look like one Proxmox cluster: the same address, the same CA, or a shared node name.
func sameCluster(a, b *ent.Location, nodesA, nodesB map[string]bool) bool {
	if hostPort(a.PveURL) == hostPort(b.PveURL) {
		return true
	}
	if fa := normFingerprint(a.CaFingerprint); fa != "" && fa == normFingerprint(b.CaFingerprint) {
		return true
	}
	for n := range nodesA {
		if nodesB[n] {
			return true
		}
	}
	return false
}

// hostPort is the URL's lowercase host and port (8006 when absent), without scheme, path or trailing slash.
func hostPort(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Hostname() == "" {
		return strings.ToLower(strings.TrimRight(strings.TrimSpace(raw), "/"))
	}
	port := u.Port()
	if port == "" {
		port = "8006"
	}
	return net.JoinHostPort(strings.ToLower(u.Hostname()), port)
}

func normFingerprint(fp string) string {
	return strings.ToUpper(strings.NewReplacer(":", "", " ", "").Replace(fp))
}

// peerVNets maps each VNet in seen that a peer location already holds a slot for to that location's name.
// A slot a peer marked unusable only because of a third holder doesn't count, so two locations can't shut each other out.
func peerVNets(ctx context.Context, etx *ent.Client, peers []*ent.Location, seen map[string]found) (map[string]string, error) {
	out := map[string]string{}
	if len(peers) == 0 || len(seen) == 0 {
		return out, nil
	}
	byID := map[int64]string{}
	ids := make([]int64, len(peers))
	for i, p := range peers {
		ids[i], byID[p.ID] = p.ID, p.Name
	}
	rows, err := etx.VNetSlot.Query().Where(vnetslot.LocationIDIn(ids...), vnetslot.VnetIn(slices.Collect(maps.Keys(seen))...)).
		Order(vnetslot.ByID()).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		if r.State == vnetslot.StateUnusable && strings.HasPrefix(r.Reason, "used by location ") {
			continue
		}
		if _, ok := out[r.Vnet]; !ok {
			out[r.Vnet] = byID[r.LocationID]
		}
	}
	return out, nil
}

// noKeyUpdate locks rows without blocking inserts that reference them.
func noKeyUpdate(s *entsql.Selector) { s.For(entsql.LockNoKeyUpdate) }

// discoverHook lets tests act between Discover's read and write phases and before its commit.
var discoverHook func(phase string)

type found struct {
	cand    Candidate
	ok      bool
	zoneTyp string
}

// wanted returns the update that brings an existing slot in line with what Proxmox shows (f is zero when the VNet is gone).
func wanted(sl *ent.VNetSlot, f found) (func(*ent.VNetSlotUpdateOne) *ent.VNetSlotUpdateOne, bool) {
	if sl.State == vnetslot.StateCooling {
		return nil, false
	}
	if f.cand.VNet == "" {
		if sl.State == vnetslot.StateGone {
			return nil, false
		}
		return func(u *ent.VNetSlotUpdateOne) *ent.VNetSlotUpdateOne {
			return u.SetState(vnetslot.StateGone).SetReason("not found in Proxmox")
		}, true
	}
	if sl.NetworkID != nil {
		// A handed-out VNet's state stays in_use; zone, zone type and MTU follow Proxmox, and health follows the checks.
		health := vnetslot.HealthOk
		if !f.ok {
			health = vnetslot.HealthDegraded
		}
		if sl.State == vnetslot.StateInUse && sl.Health == health && sl.Reason == f.cand.Reason && sl.Zone == f.cand.Zone &&
			sl.ZoneType == f.zoneTyp && sl.Mtu == f.cand.MTU {
			return nil, false
		}
		return func(u *ent.VNetSlotUpdateOne) *ent.VNetSlotUpdateOne {
			return u.SetZone(f.cand.Zone).SetZoneType(f.zoneTyp).SetMtu(f.cand.MTU).SetState(vnetslot.StateInUse).SetHealth(health).SetReason(f.cand.Reason)
		}, true
	}
	st := vnetslot.State(f.cand.State)
	if sl.State == st && sl.Health == vnetslot.HealthOk && sl.Reason == f.cand.Reason && sl.Zone == f.cand.Zone &&
		sl.ZoneType == f.zoneTyp && sl.Mtu == f.cand.MTU {
		return nil, false
	}
	return func(u *ent.VNetSlotUpdateOne) *ent.VNetSlotUpdateOne {
		return u.SetZone(f.cand.Zone).SetZoneType(f.zoneTyp).SetMtu(f.cand.MTU).SetState(st).SetHealth(vnetslot.HealthOk).SetReason(f.cand.Reason)
	}, true
}

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
	"strings"

	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/location"
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
		case sl.State == vnetslot.StateInUse:
			st.InUse++
			if sl.Health == vnetslot.HealthDegraded {
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
		loc, err := etx.Location.Get(ctx, locationID)
		if err != nil {
			return notFound(err)
		}
		if zone != nil {
			// Locking every location on the cluster keeps two admins from taking one zone together.
			peers, err := etx.Location.Query().Where(location.PveURL(loc.PveURL)).Order(location.ByID()).ForUpdate().Unique(false).All(ctx)
			if err != nil {
				return err
			}
			for _, p := range peers {
				if p.ID != locationID && p.SdnZone != nil && *p.SdnZone == *zone {
					return errZoneTaken
				}
			}
		}
		slots, err := etx.VNetSlot.Query().Where(vnetslot.LocationID(locationID)).Order(vnetslot.ByID()).ForUpdate().Unique(false).All(ctx)
		if err != nil {
			return err
		}
		sameZone := (zone == nil) == (loc.SdnZone == nil) && (zone == nil || *zone == *loc.SdnZone)
		if sameZone && prefix == loc.VnetPrefix {
			return nil
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
	type found struct {
		cand    Candidate
		ok      bool
		zoneTyp string
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
	return s.withTx(ctx, func(_ *sql.Tx, etx *ent.Client) error {
		slots, err := etx.VNetSlot.Query().Where(vnetslot.LocationID(locationID)).Order(vnetslot.ByID()).ForUpdate().Unique(false).All(ctx)
		if err != nil {
			return err
		}
		have := map[string]*ent.VNetSlot{}
		for _, sl := range slots {
			have[sl.Vnet] = sl
		}
		for name, f := range seen {
			sl := have[name]
			switch {
			case sl == nil:
				err = etx.VNetSlot.Create().SetLocationID(locationID).SetVnet(name).SetZone(f.cand.Zone).SetZoneType(f.zoneTyp).
					SetMtu(f.cand.MTU).SetState(vnetslot.State(f.cand.State)).SetReason(f.cand.Reason).Exec(ctx)
			case sl.State == vnetslot.StateCooling:
			case sl.NetworkID != nil:
				// A handed-out VNet keeps its state and MTU; only its health follows the checks.
				health := vnetslot.HealthOk
				if !f.ok {
					health = vnetslot.HealthDegraded
				}
				err = etx.VNetSlot.UpdateOneID(sl.ID).SetState(vnetslot.StateInUse).SetHealth(health).SetReason(f.cand.Reason).Exec(ctx)
			default:
				err = etx.VNetSlot.UpdateOneID(sl.ID).SetZone(f.cand.Zone).SetZoneType(f.zoneTyp).SetMtu(f.cand.MTU).
					SetState(vnetslot.State(f.cand.State)).SetHealth(vnetslot.HealthOk).SetReason(f.cand.Reason).Exec(ctx)
			}
			if err != nil {
				return err
			}
		}
		for _, sl := range slots {
			if _, ok := seen[sl.Vnet]; ok || sl.State == vnetslot.StateCooling || sl.State == vnetslot.StateGone {
				continue
			}
			if err := etx.VNetSlot.UpdateOneID(sl.ID).SetState(vnetslot.StateGone).SetReason("not found in Proxmox").Exec(ctx); err != nil {
				return err
			}
		}
		return nil
	})
}

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

// Package ipam manages IP pools and hands out addresses.
package ipam

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/netip"
	"regexp"
	"slices"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/nebla-cloud/nebla/internal/apierr"
	"github.com/nebla-cloud/nebla/internal/db"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/ipaddress"
	"github.com/nebla-cloud/nebla/internal/ent/ipallocation"
	"github.com/nebla-cloud/nebla/internal/ent/ippool"
	"github.com/nebla-cloud/nebla/internal/ent/location"
)

const (
	FamilyIPv4 = "ipv4"
	FamilyIPv6 = "ipv6"
)

var bridgeRE = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_.-]{0,14}$`)

var errNotFound = apierr.New(http.StatusNotFound, "not_found", "not found")

func invalid(format string, a ...any) error {
	return apierr.New(http.StatusUnprocessableEntity, "invalid_input", fmt.Sprintf(format, a...))
}

func conflict(code, msg string) error { return apierr.New(http.StatusConflict, code, msg) }

// Assignment is what a server needs to configure one address.
type Assignment struct {
	PoolID  int64
	Family  string
	IP      netip.Addr
	Prefix  int
	Gateway netip.Addr
	DNS     []netip.Addr
	Bridge  string
	VLAN    *int
}

// PoolInput is the writable part of a pool; addresses are textual.
type PoolInput struct {
	LocationID int64
	Family     string
	Bridge     string
	VLANTag    *int
	CIDR       string
	Gateway    string
	DNS        []string
	RangeStart string
	RangeEnd   string
}

type Pool struct {
	PoolInput
	ID        int64
	Allocated int
	CreatedAt time.Time
}

type Service struct {
	db  *ent.Client
	sql *sql.DB
}

// New builds the service; sqlDB must be the *sql.DB client was opened on.
func New(sqlDB *sql.DB, client *ent.Client) *Service { return &Service{db: client, sql: sqlDB} }

// parsed is a validated PoolInput.
type parsed struct {
	prefix       netip.Prefix
	gateway      netip.Addr
	start, end   netip.Addr
	dns          []string
	normalizedIn PoolInput
}

func parseAddr(name, s string) (netip.Addr, error) {
	a, err := netip.ParseAddr(s)
	if err != nil || a.Is4In6() || a.Zone() != "" {
		return netip.Addr{}, invalid("%s is not a valid IP address", name)
	}
	return a, nil
}

func validate(in PoolInput) (parsed, error) {
	var p parsed
	if in.Family != FamilyIPv4 && in.Family != FamilyIPv6 {
		return p, invalid("family must be ipv4 or ipv6")
	}
	if !bridgeRE.MatchString(in.Bridge) {
		return p, invalid("bridge is not a valid Linux bridge name")
	}
	if in.VLANTag != nil && (*in.VLANTag < 1 || *in.VLANTag > 4094) {
		return p, invalid("vlan_tag must be between 1 and 4094")
	}
	prefix, err := netip.ParsePrefix(in.CIDR)
	if err != nil || prefix.Addr().Is4In6() || prefix.Addr().Zone() != "" {
		return p, invalid("cidr is not a valid network")
	}
	if prefix != prefix.Masked() {
		return p, invalid("cidr has host bits set")
	}
	if prefix.Addr().Is4() != (in.Family == FamilyIPv4) {
		return p, invalid("cidr does not match family %s", in.Family)
	}
	var addrs [3]netip.Addr
	for i, f := range []struct{ name, val string }{{"gateway", in.Gateway}, {"range_start", in.RangeStart}, {"range_end", in.RangeEnd}} {
		if addrs[i], err = parseAddr(f.name, f.val); err != nil {
			return p, err
		}
		if !prefix.Contains(addrs[i]) {
			return p, invalid("%s is outside cidr", f.name)
		}
	}
	if addrs[1].Compare(addrs[2]) > 0 {
		return p, invalid("range_start is after range_end")
	}
	dns := make([]string, len(in.DNS))
	for i, d := range in.DNS {
		a, err := parseAddr("dns entry", d)
		if err != nil {
			return p, err
		}
		dns[i] = a.String()
	}
	p = parsed{prefix: prefix, gateway: addrs[0], start: addrs[1], end: addrs[2], dns: dns}
	p.normalizedIn = in
	p.normalizedIn.CIDR, p.normalizedIn.Gateway = prefix.String(), addrs[0].String()
	p.normalizedIn.RangeStart, p.normalizedIn.RangeEnd, p.normalizedIn.DNS = addrs[1].String(), addrs[2].String(), dns
	return p, nil
}

func toPool(p *ent.IPPool, allocated int) Pool {
	return Pool{ID: p.ID, Allocated: allocated, CreatedAt: p.CreatedAt, PoolInput: PoolInput{
		LocationID: p.LocationID, Family: p.Family, Bridge: p.Bridge, VLANTag: p.VlanTag, CIDR: p.Cidr, Gateway: p.Gateway,
		DNS: p.DNS, RangeStart: p.RangeStart, RangeEnd: p.RangeEnd}}
}

func notFound(err error) error {
	if ent.IsNotFound(err) {
		return errNotFound
	}
	return err
}

func (s *Service) withTx(ctx context.Context, fn func(tx *ent.Client) error) error {
	return db.WithTx(ctx, s.sql, func(_ *sql.Tx, tx *ent.Client) error { return fn(tx) })
}

// allocatedAddrs returns a pool's allocated addresses, sorted.
func allocatedAddrs(ctx context.Context, tx *ent.Client, poolID int64) ([]netip.Addr, error) {
	ips, err := tx.IPAllocation.Query().Where(ipallocation.PoolID(poolID)).Select(ipallocation.FieldIP).Strings(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]netip.Addr, 0, len(ips))
	for _, s := range ips {
		a, err := netip.ParseAddr(s)
		if err != nil {
			return nil, fmt.Errorf("stored ip %q: %w", s, err)
		}
		out = append(out, a)
	}
	slices.SortFunc(out, netip.Addr.Compare)
	return out, nil
}

// checkOverlap rejects a range that intersects another pool's range in the same location.
func checkOverlap(ctx context.Context, tx *ent.Client, locationID, skipID int64, start, end netip.Addr) error {
	others, err := tx.IPPool.Query().Where(ippool.LocationID(locationID), ippool.IDNEQ(skipID)).All(ctx)
	if err != nil {
		return err
	}
	for _, o := range others {
		os, err1 := netip.ParseAddr(o.RangeStart)
		oe, err2 := netip.ParseAddr(o.RangeEnd)
		if err1 != nil || err2 != nil {
			return fmt.Errorf("stored range of pool %d", o.ID)
		}
		if start.Compare(oe) <= 0 && os.Compare(end) <= 0 {
			return conflict("pool_overlap", fmt.Sprintf("range overlaps pool %d", o.ID))
		}
	}
	return nil
}

// lockLocation takes FOR NO KEY UPDATE so it does not block FK KEY SHARE locks from inserts referencing the location.
func lockLocation(ctx context.Context, tx *ent.Client, id int64) error {
	_, err := tx.Location.Query().Where(location.ID(id)).
		Modify(func(s *entsql.Selector) { s.For(entsql.LockNoKeyUpdate) }).Only(ctx)
	return err
}

func (s *Service) Create(ctx context.Context, in PoolInput) (Pool, error) {
	v, err := validate(in)
	if err != nil {
		return Pool{}, err
	}
	var out Pool
	err = s.withTx(ctx, func(tx *ent.Client) error {
		// The location lock serializes overlap checks for one location.
		if err := lockLocation(ctx, tx, in.LocationID); err != nil {
			if ent.IsNotFound(err) {
				return invalid("location_id does not exist")
			}
			return err
		}
		if err := checkOverlap(ctx, tx, in.LocationID, 0, v.start, v.end); err != nil {
			return err
		}
		n := v.normalizedIn
		c := tx.IPPool.Create().SetLocationID(n.LocationID).SetFamily(n.Family).SetBridge(n.Bridge).SetCidr(n.CIDR).
			SetGateway(n.Gateway).SetDNS(n.DNS).SetRangeStart(n.RangeStart).SetRangeEnd(n.RangeEnd)
		if n.VLANTag != nil {
			c.SetVlanTag(*n.VLANTag)
		}
		p, err := c.Save(ctx)
		if err != nil {
			return err
		}
		out = toPool(p, 0)
		return nil
	})
	return out, err
}

func (s *Service) Get(ctx context.Context, id int64) (Pool, error) {
	p, err := s.db.IPPool.Get(ctx, id)
	if err != nil {
		return Pool{}, notFound(err)
	}
	n, err := s.db.IPAllocation.Query().Where(ipallocation.PoolID(id)).Count(ctx)
	if err != nil {
		return Pool{}, err
	}
	return toPool(p, n), nil
}

// List returns one page of pools by id; locationID 0 means every location.
func (s *Service) List(ctx context.Context, locationID int64, page, perPage int) ([]Pool, int, error) {
	q := s.db.IPPool.Query()
	if locationID != 0 {
		q = q.Where(ippool.LocationID(locationID))
	}
	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	ps, err := q.Order(ent.Asc(ippool.FieldID)).Offset((page - 1) * perPage).Limit(perPage).All(ctx)
	if err != nil {
		return nil, 0, err
	}
	out := make([]Pool, len(ps))
	for i, p := range ps {
		n, err := s.db.IPAllocation.Query().Where(ipallocation.PoolID(p.ID)).Count(ctx)
		if err != nil {
			return nil, 0, err
		}
		out[i] = toPool(p, n)
	}
	return out, total, nil
}

// Update replaces a pool's settings; its location is fixed and live allocations must stay valid.
func (s *Service) Update(ctx context.Context, id int64, in PoolInput) (Pool, error) {
	v, err := validate(in)
	if err != nil {
		return Pool{}, err
	}
	var out Pool
	err = s.withTx(ctx, func(tx *ent.Client) error {
		// location_id is immutable, so read it unlocked and take the location lock before the pool's.
		pre, err := tx.IPPool.Get(ctx, id)
		if err != nil {
			return notFound(err)
		}
		locID := pre.LocationID
		if in.LocationID != locID {
			return invalid("location_id cannot change")
		}
		if err := lockLocation(ctx, tx, locID); err != nil {
			return err
		}
		cur, err := tx.IPPool.Query().Where(ippool.ID(id)).ForUpdate().Only(ctx)
		if err != nil {
			return notFound(err)
		}
		if err := checkOverlap(ctx, tx, cur.LocationID, id, v.start, v.end); err != nil {
			return err
		}
		used, err := allocatedAddrs(ctx, tx, id)
		if err != nil {
			return err
		}
		for _, a := range used {
			if !v.prefix.Contains(a) || a.Compare(v.start) < 0 || a.Compare(v.end) > 0 || a == v.gateway {
				return conflict("pool_in_use", fmt.Sprintf("allocated address %s would fall outside the pool", a))
			}
		}
		n := v.normalizedIn
		u := tx.IPPool.UpdateOneID(id).SetFamily(n.Family).SetBridge(n.Bridge).SetCidr(n.CIDR).SetGateway(n.Gateway).
			SetDNS(n.DNS).SetRangeStart(n.RangeStart).SetRangeEnd(n.RangeEnd)
		if n.VLANTag != nil {
			u.SetVlanTag(*n.VLANTag)
		} else {
			u.ClearVlanTag()
		}
		p, err := u.Save(ctx)
		if err != nil {
			return err
		}
		out = toPool(p, len(used))
		return nil
	})
	return out, err
}

// Delete removes a pool that has no allocations.
func (s *Service) Delete(ctx context.Context, id int64) error {
	return s.withTx(ctx, func(tx *ent.Client) error {
		if _, err := tx.IPPool.Query().Where(ippool.ID(id)).ForUpdate().Only(ctx); err != nil {
			return notFound(err)
		}
		busy, err := tx.IPAllocation.Query().Where(ipallocation.PoolID(id)).Exist(ctx)
		if err != nil {
			return err
		}
		if busy {
			return conflict("pool_in_use", "pool still has allocated addresses")
		}
		return tx.IPPool.DeleteOneID(id).Exec(ctx)
	})
}

// lowestFree returns the first address in [start, end] that is neither used nor the gateway; used must be sorted.
func lowestFree(start, end, gateway netip.Addr, used []netip.Addr) (netip.Addr, bool) {
	taken := slices.Clone(used)
	if gateway.Compare(start) >= 0 && gateway.Compare(end) <= 0 {
		taken = append(taken, gateway)
		slices.SortFunc(taken, netip.Addr.Compare)
	}
	cand := start
	for _, a := range taken {
		switch c := a.Compare(cand); {
		case c < 0:
		case c == 0:
			cand = cand.Next()
		default:
			return cand, cand.IsValid() && cand.Compare(end) <= 0
		}
	}
	return cand, cand.IsValid() && cand.Compare(end) <= 0
}

// freeAddr returns the lowest free address of a pool.
func freeAddr(ctx context.Context, tx *ent.Client, p *ent.IPPool) (netip.Addr, bool, error) {
	start, err1 := netip.ParseAddr(p.RangeStart)
	end, err2 := netip.ParseAddr(p.RangeEnd)
	gw, err3 := netip.ParseAddr(p.Gateway)
	if err := firstErr(err1, err2, err3); err != nil {
		return netip.Addr{}, false, fmt.Errorf("pool %d addresses: %w", p.ID, err)
	}
	used, err := allocatedAddrs(ctx, tx, p.ID)
	if err != nil {
		return netip.Addr{}, false, err
	}
	ip, ok := lowestFree(start, end, gw, used)
	return ip, ok, nil
}

// Available reports per family whether a pool in the location has a free address; it takes no locks.
func Available(ctx context.Context, c *ent.Client, locationID int64) (ipv4, ipv6 bool, err error) {
	pools, err := c.IPPool.Query().Where(ippool.LocationID(locationID)).Order(ent.Asc(ippool.FieldID)).All(ctx)
	if err != nil {
		return false, false, err
	}
	for _, p := range pools {
		if (p.Family == FamilyIPv4 && ipv4) || (p.Family == FamilyIPv6 && ipv6) {
			continue
		}
		_, ok, err := freeAddr(ctx, c, p)
		if err != nil {
			return false, false, err
		}
		if !ok {
			continue
		}
		if p.Family == FamilyIPv4 {
			ipv4 = true
		} else {
			ipv6 = true
		}
	}
	return ipv4, ipv6, nil
}

// Segment is the bridge and VLAN a floating address is reachable on.
type Segment struct {
	Bridge string
	VLAN   *int
}

// SegmentOf returns the segment an assignment sits on.
func SegmentOf(a Assignment) Segment { return Segment{Bridge: a.Bridge, VLAN: a.VLAN} }

func (s Segment) matches(p *ent.IPPool) bool {
	if p.Bridge != s.Bridge {
		return false
	}
	if s.VLAN == nil || p.VlanTag == nil {
		return s.VLAN == nil && p.VlanTag == nil
	}
	return *s.VLAN == *p.VlanTag
}

// AllocateFloating takes the lowest free address of the family on a matching segment (nil means any) and returns its allocation id; call it inside a transaction.
func AllocateFloating(ctx context.Context, tx *ent.Client, locationID int64, family string, seg *Segment) (Assignment, int64, error) {
	return allocate(ctx, tx, locationID, family, seg, ipallocation.OwnerFloating)
}

// AllocatePrimary is AllocateFloating for a primary IP.
func AllocatePrimary(ctx context.Context, tx *ent.Client, locationID int64, family string, seg *Segment) (Assignment, int64, error) {
	return allocate(ctx, tx, locationID, family, seg, ipallocation.OwnerPrimary)
}

// allocate locks the location's pools of family in id order and takes the first free address.
func allocate(ctx context.Context, tx *ent.Client, locationID int64, family string, seg *Segment, owner ipallocation.Owner) (Assignment, int64, error) {
	pools, err := tx.IPPool.Query().Where(ippool.LocationID(locationID), ippool.Family(family)).
		Order(ent.Asc(ippool.FieldID)).ForUpdate().All(ctx)
	if err != nil {
		return Assignment{}, 0, err
	}
	for _, p := range pools {
		if seg != nil && !seg.matches(p) {
			continue
		}
		ip, ok, err := freeAddr(ctx, tx, p)
		if err != nil {
			return Assignment{}, 0, err
		}
		if !ok {
			continue
		}
		row, err := tx.IPAllocation.Create().SetPoolID(p.ID).SetIP(ip.String()).SetOwner(owner).Save(ctx)
		if err != nil {
			return Assignment{}, 0, err
		}
		a, err := assignment(p, ip)
		return a, row.ID, err
	}
	return Assignment{}, 0, conflict("ip_pool_exhausted", "no free "+family+" address in this location")
}

// FreeAllocation deletes one floating or primary allocation; it is idempotent.
func FreeAllocation(ctx context.Context, tx *ent.Client, id int64) error {
	_, err := tx.IPAllocation.Delete().Where(ipallocation.ID(id),
		ipallocation.OwnerIn(ipallocation.OwnerFloating, ipallocation.OwnerPrimary)).Exec(ctx)
	return err
}

func assignment(p *ent.IPPool, ip netip.Addr) (Assignment, error) {
	prefix, err1 := netip.ParsePrefix(p.Cidr)
	gw, err2 := netip.ParseAddr(p.Gateway)
	if err := firstErr(err1, err2); err != nil {
		return Assignment{}, fmt.Errorf("pool %d: %w", p.ID, err)
	}
	dns := make([]netip.Addr, 0, len(p.DNS))
	for _, d := range p.DNS {
		a, err := netip.ParseAddr(d)
		if err != nil {
			return Assignment{}, fmt.Errorf("pool %d dns: %w", p.ID, err)
		}
		dns = append(dns, a)
	}
	return Assignment{PoolID: p.ID, Family: p.Family, IP: ip, Prefix: prefix.Bits(), Gateway: gw, DNS: dns, Bridge: p.Bridge, VLAN: p.VlanTag}, nil
}

// Assignments returns the addresses of a server's primary IPs with their pool settings, ipv4 first.
func Assignments(ctx context.Context, c *ent.Client, serverID int64) ([]Assignment, error) {
	rows, err := c.IPAddress.Query().Where(ipaddress.KindEQ(ipaddress.KindPrimary), ipaddress.ServerID(serverID)).
		WithAllocation(func(q *ent.IPAllocationQuery) { q.WithPool() }).All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Assignment, 0, len(rows))
	for _, r := range rows {
		ip, err := netip.ParseAddr(r.IP)
		if err != nil {
			return nil, fmt.Errorf("primary IP %d: %w", r.ID, err)
		}
		a, err := assignment(r.Edges.Allocation.Edges.Pool, ip)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	slices.SortFunc(out, func(a, b Assignment) int {
		if a.Family != b.Family {
			if a.Family == FamilyIPv4 {
				return -1
			}
			return 1
		}
		return a.IP.Compare(b.IP)
	})
	return out, nil
}

func firstErr(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

package ipam_test

import (
	"context"
	"database/sql"
	"errors"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/Hexeption/nebla/internal/apierr"
	"github.com/Hexeption/nebla/internal/db"
	"github.com/Hexeption/nebla/internal/ent"
	"github.com/Hexeption/nebla/internal/ipam"
	"github.com/Hexeption/nebla/internal/testutil"
	"github.com/stretchr/testify/require"
)

type env struct {
	svc *ipam.Service
	ent *ent.Client
	sql *sql.DB
	loc int64
}

func setup(t *testing.T) env {
	t.Helper()
	c, sqlDB := testutil.NewEntSQL(t)
	return env{svc: ipam.New(sqlDB, c), ent: c, sql: sqlDB, loc: newLocation(t, c, "eu-1")}
}

func newLocation(t *testing.T, c *ent.Client, name string) int64 {
	t.Helper()
	l, err := c.Location.Create().SetName(name).SetPveURL("https://pve.example:8006").SetTokenID("t").
		SetTokenSecret([]byte("s")).SetDiskStorage("ceph").SetIsoStorage("local").Save(context.Background())
	require.NoError(t, err)
	return l.ID
}

func v4(loc int64, start, end string) ipam.PoolInput {
	return ipam.PoolInput{LocationID: loc, Family: "ipv4", Bridge: "vmbr0", CIDR: "10.0.0.0/24", Gateway: "10.0.0.1",
		DNS: []string{"1.1.1.1", "2606:4700:4700::1111"}, RangeStart: start, RangeEnd: end}
}

func requireCode(t *testing.T, err error, status int, code string) {
	t.Helper()
	var e apierr.E
	require.True(t, errors.As(err, &e), "want apierr, got %v", err)
	require.Equal(t, status, e.Status, e.Msg)
	require.Equal(t, code, e.Code, e.Msg)
}

func (e env) allocate(ctx context.Context, family string, server int64) (ipam.Assignment, error) {
	var a ipam.Assignment
	err := db.WithTx(ctx, e.sql, func(_ *sql.Tx, tx *ent.Client) (err error) {
		a, err = ipam.Allocate(ctx, tx, e.loc, family, server)
		return err
	})
	return a, err
}

func (e env) release(ctx context.Context, server int64) error {
	return db.WithTx(ctx, e.sql, func(_ *sql.Tx, tx *ent.Client) error { return ipam.Release(ctx, tx, server) })
}

func TestAllocateLowestFree(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	_, err := e.svc.Create(ctx, v4(e.loc, "10.0.0.10", "10.0.0.12"))
	require.NoError(t, err)
	for i, want := range []string{"10.0.0.10", "10.0.0.11", "10.0.0.12"} {
		a, err := e.allocate(ctx, "ipv4", int64(i+1))
		require.NoError(t, err)
		require.Equal(t, netip.MustParseAddr(want), a.IP)
		require.Equal(t, 24, a.Prefix)
		require.Equal(t, netip.MustParseAddr("10.0.0.1"), a.Gateway)
		require.Equal(t, "vmbr0", a.Bridge)
		require.Nil(t, a.VLAN)
		require.Len(t, a.DNS, 2)
	}
	_, err = e.allocate(ctx, "ipv4", 4)
	requireCode(t, err, 409, "ip_pool_exhausted")
	_, err = e.allocate(ctx, "ipv6", 4)
	requireCode(t, err, 409, "ip_pool_exhausted")
}

func TestAllocateSkipsGateway(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	_, err := e.svc.Create(ctx, v4(e.loc, "10.0.0.1", "10.0.0.3"))
	require.NoError(t, err)
	a, err := e.allocate(ctx, "ipv4", 1)
	require.NoError(t, err)
	require.Equal(t, netip.MustParseAddr("10.0.0.2"), a.IP)
}

func TestAllocateAcrossPoolsLowestIDFirst(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	_, err := e.svc.Create(ctx, v4(e.loc, "10.0.0.50", "10.0.0.50"))
	require.NoError(t, err)
	in := v4(e.loc, "10.0.0.10", "10.0.0.10")
	in.CIDR, in.Gateway, in.RangeStart, in.RangeEnd = "10.0.1.0/24", "10.0.1.1", "10.0.1.5", "10.0.1.6"
	in.VLANTag = new(100)
	_, err = e.svc.Create(ctx, in)
	require.NoError(t, err)
	a, err := e.allocate(ctx, "ipv4", 1)
	require.NoError(t, err)
	require.Equal(t, netip.MustParseAddr("10.0.0.50"), a.IP)
	a, err = e.allocate(ctx, "ipv4", 2)
	require.NoError(t, err)
	require.Equal(t, netip.MustParseAddr("10.0.1.5"), a.IP)
	require.Equal(t, 100, *a.VLAN)
}

func TestAllocateIPv6HugeRange(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	_, err := e.svc.Create(ctx, ipam.PoolInput{LocationID: e.loc, Family: "ipv6", Bridge: "vmbr1", CIDR: "2001:db8::/32",
		Gateway: "2001:db8::1", DNS: []string{}, RangeStart: "2001:db8::2", RangeEnd: "2001:db8:ffff::"})
	require.NoError(t, err)
	a, err := e.allocate(ctx, "ipv6", 1)
	require.NoError(t, err)
	require.Equal(t, netip.MustParseAddr("2001:db8::2"), a.IP)
	require.Equal(t, 32, a.Prefix)
}

func TestReleaseMakesIPReusable(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	_, err := e.svc.Create(ctx, v4(e.loc, "10.0.0.10", "10.0.0.12"))
	require.NoError(t, err)
	for i := range 3 {
		_, err := e.allocate(ctx, "ipv4", int64(i+1))
		require.NoError(t, err)
	}
	require.NoError(t, e.release(ctx, 2))
	require.NoError(t, e.release(ctx, 2))
	a, err := e.allocate(ctx, "ipv4", 9)
	require.NoError(t, err)
	require.Equal(t, netip.MustParseAddr("10.0.0.11"), a.IP)
}

func TestAllocateConcurrentLastIP(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	_, err := e.svc.Create(ctx, v4(e.loc, "10.0.0.10", "10.0.0.10"))
	require.NoError(t, err)

	// A allocates and holds its tx open; B issues its Allocate while A still holds the pool lock.
	allocated := make(chan struct{})
	errs := make([]error, 2)
	var wg sync.WaitGroup
	wg.Go(func() {
		errs[0] = db.WithTx(ctx, e.sql, func(_ *sql.Tx, tx *ent.Client) error {
			_, err := ipam.Allocate(ctx, tx, e.loc, "ipv4", 1)
			close(allocated)
			if err != nil {
				return err
			}
			time.Sleep(300 * time.Millisecond)
			return nil
		})
	})
	wg.Go(func() {
		<-allocated
		errs[1] = db.WithTx(ctx, e.sql, func(_ *sql.Tx, tx *ent.Client) error {
			_, err := ipam.Allocate(ctx, tx, e.loc, "ipv4", 2)
			return err
		})
	})
	wg.Wait()

	var ok, exhausted int
	for _, err := range errs {
		var ae apierr.E
		switch {
		case err == nil:
			ok++
		case errors.As(err, &ae) && ae.Code == "ip_pool_exhausted" && ae.Status == 409:
			exhausted++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	require.Equal(t, 1, ok)
	require.Equal(t, 1, exhausted)
}

func TestPoolValidation(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	cases := map[string]func(*ipam.PoolInput){
		"gateway outside cidr":  func(p *ipam.PoolInput) { p.Gateway = "10.0.1.1" },
		"range start outside":   func(p *ipam.PoolInput) { p.RangeStart = "9.255.255.255" },
		"range end outside":     func(p *ipam.PoolInput) { p.RangeEnd = "10.0.1.0" },
		"start after end":       func(p *ipam.PoolInput) { p.RangeStart, p.RangeEnd = "10.0.0.20", "10.0.0.10" },
		"family mismatch":       func(p *ipam.PoolInput) { p.Family = "ipv6" },
		"bad family":            func(p *ipam.PoolInput) { p.Family = "ipx" },
		"bad cidr":              func(p *ipam.PoolInput) { p.CIDR = "nope" },
		"bad dns":               func(p *ipam.PoolInput) { p.DNS = []string{"dns.example"} },
		"bad bridge":            func(p *ipam.PoolInput) { p.Bridge = "1; rm" },
		"vlan zero":             func(p *ipam.PoolInput) { p.VLANTag = new(0) },
		"vlan too high":         func(p *ipam.PoolInput) { p.VLANTag = new(4095) },
		"unknown location":      func(p *ipam.PoolInput) { p.LocationID = 9999 },
		"gateway not an ip":     func(p *ipam.PoolInput) { p.Gateway = "gw" },
		"range start not an ip": func(p *ipam.PoolInput) { p.RangeStart = "x" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			in := v4(e.loc, "10.0.0.10", "10.0.0.20")
			mutate(&in)
			_, err := e.svc.Create(ctx, in)
			requireCode(t, err, 422, "invalid_input")
		})
	}
}

func TestPoolGatewayOutsideCIDR(t *testing.T) {
	e := setup(t)
	in := v4(e.loc, "10.0.0.10", "10.0.0.20")
	in.Gateway = "192.168.0.1"
	_, err := e.svc.Create(context.Background(), in)
	requireCode(t, err, 422, "invalid_input")
}

func TestPoolOverlap(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	_, err := e.svc.Create(ctx, v4(e.loc, "10.0.0.10", "10.0.0.20"))
	require.NoError(t, err)
	_, err = e.svc.Create(ctx, v4(e.loc, "10.0.0.20", "10.0.0.30"))
	requireCode(t, err, 409, "pool_overlap")
	other, err := e.svc.Create(ctx, v4(e.loc, "10.0.0.21", "10.0.0.30"))
	require.NoError(t, err)
	_, err = e.svc.Update(ctx, other.ID, v4(e.loc, "10.0.0.15", "10.0.0.30"))
	requireCode(t, err, 409, "pool_overlap")
	_, err = e.svc.Update(ctx, other.ID, v4(e.loc, "10.0.0.21", "10.0.0.40"))
	require.NoError(t, err)
	// Another location may reuse the same range.
	loc2 := newLocation(t, e.ent, "eu-2")
	_, err = e.svc.Create(ctx, v4(loc2, "10.0.0.10", "10.0.0.20"))
	require.NoError(t, err)
}

func TestPoolInUse(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	p, err := e.svc.Create(ctx, v4(e.loc, "10.0.0.10", "10.0.0.20"))
	require.NoError(t, err)
	_, err = e.allocate(ctx, "ipv4", 1)
	require.NoError(t, err)
	_, err = e.allocate(ctx, "ipv4", 2)
	require.NoError(t, err)

	requireCode(t, e.svc.Delete(ctx, p.ID), 409, "pool_in_use")
	// .10 and .11 are allocated; shrinking past them is refused.
	_, err = e.svc.Update(ctx, p.ID, v4(e.loc, "10.0.0.12", "10.0.0.20"))
	requireCode(t, err, 409, "pool_in_use")
	_, err = e.svc.Update(ctx, p.ID, v4(e.loc, "10.0.0.10", "10.0.0.11"))
	require.NoError(t, err)
	moved := v4(e.loc, "10.0.0.10", "10.0.0.11")
	moved.Gateway = "10.0.0.10"
	_, err = e.svc.Update(ctx, p.ID, moved)
	requireCode(t, err, 409, "pool_in_use")

	got, err := e.svc.Get(ctx, p.ID)
	require.NoError(t, err)
	require.Equal(t, 2, got.Allocated)

	require.NoError(t, e.release(ctx, 1))
	require.NoError(t, e.release(ctx, 2))
	require.NoError(t, e.svc.Delete(ctx, p.ID))
	_, err = e.svc.Get(ctx, p.ID)
	requireCode(t, err, 404, "not_found")
}

func TestListPools(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	for _, r := range [][2]string{{"10.0.0.10", "10.0.0.11"}, {"10.0.0.20", "10.0.0.21"}, {"10.0.0.30", "10.0.0.31"}} {
		_, err := e.svc.Create(ctx, v4(e.loc, r[0], r[1]))
		require.NoError(t, err)
	}
	ps, total, err := e.svc.List(ctx, 0, 1, 2)
	require.NoError(t, err)
	require.Equal(t, 3, total)
	require.Len(t, ps, 2)
	ps, _, err = e.svc.List(ctx, e.loc, 2, 2)
	require.NoError(t, err)
	require.Len(t, ps, 1)
	ps, total, err = e.svc.List(ctx, 9999, 1, 2)
	require.NoError(t, err)
	require.Zero(t, total)
	require.Empty(t, ps)
}

func TestAssignmentsMatchAllocate(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	_, err := e.svc.Create(ctx, v4(e.loc, "10.0.0.10", "10.0.0.12"))
	require.NoError(t, err)
	vlan := 7
	_, err = e.svc.Create(ctx, ipam.PoolInput{LocationID: e.loc, Family: "ipv6", Bridge: "vmbr1", VLANTag: &vlan, CIDR: "2001:db8::/64",
		Gateway: "2001:db8::1", DNS: []string{"2001:db8::53"}, RangeStart: "2001:db8::10", RangeEnd: "2001:db8::20"})
	require.NoError(t, err)
	a4, err := e.allocate(ctx, "ipv4", 5)
	require.NoError(t, err)
	a6, err := e.allocate(ctx, "ipv6", 5)
	require.NoError(t, err)
	_, err = e.allocate(ctx, "ipv4", 6)
	require.NoError(t, err)

	got, err := ipam.Assignments(ctx, e.ent, 5)
	require.NoError(t, err)
	require.Equal(t, []ipam.Assignment{a4, a6}, got, "ipv4 first, then ipv6")
	got, err = ipam.Assignments(ctx, e.ent, 99)
	require.NoError(t, err)
	require.Empty(t, got)
}

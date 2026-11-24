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

package db_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nebla-cloud/nebla/internal/db"
	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
)

type primaryMigration struct {
	pool               *pgxpool.Pool
	goose              *goose.Provider
	prev, schema, data int64
	ctx                context.Context
	macSeq             int
}

// newPrimaryMigration returns a database migrated to the version before primary_ips.
func newPrimaryMigration(t *testing.T) *primaryMigration {
	t.Helper()
	ctx := context.Background()
	pool := testutil.NewDB(t)
	sqlDB := db.SQL(pool)
	t.Cleanup(func() { sqlDB.Close() })
	p, err := goose.NewProvider(goose.DialectPostgres, sqlDB, os.DirFS("migrations"))
	require.NoError(t, err)
	m := &primaryMigration{pool: pool, goose: p, ctx: ctx}
	var last int64
	for _, s := range p.ListSources() {
		switch {
		case strings.HasSuffix(s.Path, "_primary_ips.sql"):
			m.prev, m.schema = last, s.Version
		case strings.HasSuffix(s.Path, "_primary_ips_data.sql"):
			m.data = s.Version
		}
		last = s.Version
	}
	require.NotZero(t, m.schema, "primary_ips migration missing")
	require.NotZero(t, m.data, "primary_ips_data migration missing")
	require.Greater(t, m.data, m.schema)
	_, err = p.DownTo(ctx, m.prev)
	require.NoError(t, err)
	return m
}

func (m *primaryMigration) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	_, err := m.pool.Exec(m.ctx, sql, args...)
	require.NoError(t, err)
}

func (m *primaryMigration) id(t *testing.T, sql string, args ...any) int64 {
	t.Helper()
	var id int64
	require.NoError(t, m.pool.QueryRow(m.ctx, sql, args...).Scan(&id))
	return id
}

func (m *primaryMigration) count(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, m.pool.QueryRow(m.ctx, sql, args...).Scan(&n))
	return n
}

func (m *primaryMigration) version(t *testing.T) int64 {
	t.Helper()
	v, err := m.goose.GetDBVersion(m.ctx)
	require.NoError(t, err)
	return v
}

// rows renders a query's rows as sorted strings, for set comparisons.
func (m *primaryMigration) rows(t *testing.T, sql string) []string {
	t.Helper()
	rs, err := m.pool.Query(m.ctx, sql)
	require.NoError(t, err)
	defer rs.Close()
	var out []string
	for rs.Next() {
		vals, err := rs.Values()
		require.NoError(t, err)
		out = append(out, fmt.Sprint(vals...))
	}
	require.NoError(t, rs.Err())
	return out
}

type primarySeed struct {
	loc, projA, projB, v4, v6 int64
}

func (m *primaryMigration) seedBase(t *testing.T) primarySeed {
	t.Helper()
	var s primarySeed
	s.loc = m.id(t, `insert into locations (name, pve_url, token_id, token_secret, disk_storage, iso_storage)
		values ('eu-1', 'https://pve', 't', '\x00', 'local', 'local') returning id`)
	s.projA = m.id(t, `insert into projects (name) values ('a') returning id`)
	s.projB = m.id(t, `insert into projects (name) values ('b') returning id`)
	s.v4 = m.id(t, `insert into ip_pools (family, bridge, cidr, gateway, dns, range_start, range_end, location_id)
		values ('ipv4', 'vmbr0', '10.0.0.0/24', '10.0.0.1', '[]', '10.0.0.10', '10.0.0.200', $1) returning id`, s.loc)
	s.v6 = m.id(t, `insert into ip_pools (family, bridge, cidr, gateway, dns, range_start, range_end, location_id)
		values ('ipv6', 'vmbr0', 'fd00::/64', 'fd00::1', '[]', 'fd00::10', 'fd00::ffff', $1) returning id`, s.loc)
	m.exec(t, `insert into server_types (name, cores, memory_mb, disk_gb) values ('small', 1, 1024, 10)`)
	m.exec(t, `insert into images (name, os_flavor, os_version) values ('debian-12', 'debian', '12')`)
	return s
}

func (m *primaryMigration) server(t *testing.T, s primarySeed, project int64, name, status string, isoGen int) int64 {
	t.Helper()
	m.macSeq++
	return m.id(t, `insert into servers (name, template_vmid, status, mac, labels, disk_gb, ssh_keys, project_id, location_id,
			server_type_id, image_id, cores, memory_mb, iso_generation)
		values ($1, 9000, $2, $3, '{}', 10, '[]', $4, $5, (select id from server_types), (select id from images), 1, 1024, $6)
		returning id`, name, status, fmt.Sprintf("BC:24:11:00:00:%02X", m.macSeq), project, s.loc, isoGen)
}

func (m *primaryMigration) alloc(t *testing.T, pool int64, ip string, server int64) int64 {
	t.Helper()
	return m.id(t, `insert into ip_allocations (pool_id, ip, server_id, owner) values ($1, $2, $3, 'server') returning id`,
		pool, ip, server)
}

type primaryFixture struct {
	primarySeed
	webA, dbA, webB, long             int64
	webA4, webA6, dbA4, webB4, longA4 int64
	longName                          string
}

func (m *primaryMigration) seedFixture(t *testing.T) primaryFixture {
	t.Helper()
	f := primaryFixture{primarySeed: m.seedBase(t)}
	f.webA = m.server(t, f.primarySeed, f.projA, "web1", "running", 1)
	f.dbA = m.server(t, f.primarySeed, f.projA, "db", "deleting", 3)
	f.webB = m.server(t, f.primarySeed, f.projB, "web1", "off", 5)
	f.longName = strings.Repeat("a", 57) + "-bbbbb"
	f.long = m.server(t, f.primarySeed, f.projA, f.longName, "running", 2)
	f.webA4 = m.alloc(t, f.v4, "10.0.0.10", f.webA)
	f.webA6 = m.alloc(t, f.v6, "fd00::10", f.webA)
	f.dbA4 = m.alloc(t, f.v4, "10.0.0.11", f.dbA)
	f.webB4 = m.alloc(t, f.v4, "10.0.0.12", f.webB)
	f.longA4 = m.alloc(t, f.v4, "10.0.0.13", f.long)
	fa := m.id(t, `insert into ip_allocations (pool_id, ip, owner) values ($1, '10.0.0.50', 'floating') returning id`, f.v4)
	m.exec(t, `insert into ip_addresses (kind, family, ip, name, labels, project_id, location_id, allocation_id)
		values ('floating', 'ipv4', '10.0.0.50', 'web1-ipv4', '{}', $1, $2, $3)`, f.projA, f.loc, fa)
	return f
}

func TestPrimaryIPsMigrationMovesAllocations(t *testing.T) {
	m := newPrimaryMigration(t)
	f := m.seedFixture(t)
	_, err := m.goose.Up(m.ctx)
	require.NoError(t, err)

	type row struct {
		project, server int64
		family, ip      string
		autoDelete      bool
	}
	got := map[string]row{}
	rs, err := m.pool.Query(m.ctx, `select x.name, x.project_id, x.server_id, x.family, host(x.ip), x.auto_delete, x.allocation_id
		from ip_addresses x where x.kind = 'primary'`)
	require.NoError(t, err)
	allocs := map[string]int64{}
	for rs.Next() {
		var name string
		var r row
		var server *int64
		var alloc int64
		require.NoError(t, rs.Scan(&name, &r.project, &server, &r.family, &r.ip, &r.autoDelete, &alloc))
		require.NotNil(t, server, name)
		r.server = *server
		got[fmt.Sprintf("%d/%s", r.project, name)] = r
		allocs[fmt.Sprintf("%d/%s", r.project, name)] = alloc
	}
	require.NoError(t, rs.Err())
	rs.Close()

	cut := strings.Repeat("a", 57) + "-ipv4"
	require.Len(t, cut, 62)
	want := map[string]row{
		fmt.Sprintf("%d/web1-ipv4-%d", f.projA, f.webA4): {f.projA, f.webA, "ipv4", "10.0.0.10", true},
		fmt.Sprintf("%d/web1-ipv6", f.projA):             {f.projA, f.webA, "ipv6", "fd00::10", true},
		fmt.Sprintf("%d/db-ipv4", f.projA):               {f.projA, f.dbA, "ipv4", "10.0.0.11", true},
		fmt.Sprintf("%d/web1-ipv4", f.projB):             {f.projB, f.webB, "ipv4", "10.0.0.12", true},
		fmt.Sprintf("%d/%s", f.projA, cut):               {f.projA, f.long, "ipv4", "10.0.0.13", true},
	}
	require.Equal(t, want, got)
	require.Equal(t, f.webA4, allocs[fmt.Sprintf("%d/web1-ipv4-%d", f.projA, f.webA4)])
	require.Equal(t, f.longA4, allocs[fmt.Sprintf("%d/%s", f.projA, cut)])

	require.Zero(t, m.count(t, `select count(*) from ip_allocations where owner = 'server'`))
	require.Equal(t, 5, m.count(t, `select count(*) from ip_allocations where owner = 'primary' and server_id is null`))
	require.Equal(t, 1, m.count(t, `select count(*) from ip_addresses where kind = 'floating' and name = 'web1-ipv4'`))
	require.Zero(t, m.count(t, `select count(*) from servers where instance_gen <> iso_generation`))
	require.Equal(t, 5, m.count(t, `select instance_gen from servers where id = $1`, f.webB))
	require.Zero(t, m.count(t, `select count(*) from servers where net_refresh or iso_net_hash <> ''`))
}

func TestPrimaryIPsMigrationCutCollision(t *testing.T) {
	m := newPrimaryMigration(t)
	s := m.seedBase(t)
	prefix := strings.Repeat("c", 58)
	one := m.server(t, s, s.projA, prefix+"one", "running", 1)
	two := m.server(t, s, s.projA, prefix+"two", "running", 1)
	a1 := m.alloc(t, s.v4, "10.0.0.20", one)
	a2 := m.alloc(t, s.v4, "10.0.0.21", two)
	_, err := m.goose.Up(m.ctx)
	require.NoError(t, err)
	names := m.rows(t, `select name from ip_addresses where kind = 'primary' order by allocation_id`)
	cut := func(id int64) string {
		n := fmt.Sprint(id)
		return prefix[:57-len(n)] + "-ipv4-" + n
	}
	require.Equal(t, []string{cut(a1), cut(a2)}, names)
	for _, n := range names {
		require.LessOrEqual(t, len(n), 63)
	}
}

const allocSet = `select a.pool_id, host(a.ip), coalesce(a.server_id, x.server_id)
	from ip_allocations a left join ip_addresses x on x.allocation_id = a.id and x.kind = 'primary'
	order by 1, 2`

func TestPrimaryIPsMigrationRoundTrip(t *testing.T) {
	m := newPrimaryMigration(t)
	m.seedFixture(t)
	before := m.rows(t, allocSet)

	_, err := m.goose.Up(m.ctx)
	require.NoError(t, err)
	up1 := m.rows(t, allocSet)
	n1 := m.count(t, `select count(*) from ip_addresses where kind = 'primary'`)
	require.Equal(t, before, up1)

	_, err = m.goose.DownTo(m.ctx, m.prev)
	require.NoError(t, err)
	require.Equal(t, m.prev, m.version(t))
	require.Equal(t, before, m.rows(t, `select pool_id, host(ip), server_id from ip_allocations order by 1, 2`))
	require.Zero(t, m.count(t, `select count(*) from ip_allocations where owner = 'server' and server_id is null`))
	require.Equal(t, 1, m.count(t, `select count(*) from ip_addresses`))

	_, err = m.goose.Up(m.ctx)
	require.NoError(t, err)
	require.Equal(t, up1, m.rows(t, allocSet))
	require.Equal(t, n1, m.count(t, `select count(*) from ip_addresses where kind = 'primary'`))
}

func TestPrimaryIPsDownRefuses(t *testing.T) {
	cases := map[string]struct {
		mutate string
		msg    string
	}{
		"unassigned": {
			`update ip_addresses set server_id = null where kind = 'primary' and name = 'db-ipv4'`,
			"1 unassigned primary IPs exist; delete or assign them before rolling back",
		},
		"keep": {
			`update ip_addresses set auto_delete = false where kind = 'primary' and name = 'db-ipv4'`,
			"1 primary IPs marked keep exist; set auto_delete or delete them before rolling back",
		},
		"releasing": {
			`update ip_addresses set releasing_from_id = server_id where kind = 'primary' and name = 'db-ipv4'`,
			"1 primary IPs have a running action; wait for it to finish before rolling back",
		},
		"pending": {
			`update ip_addresses set pending_gen = 7 where kind = 'primary' and name = 'db-ipv4'`,
			"1 primary IPs have a running action; wait for it to finish before rolling back",
		},
		"deleting": {
			`update ip_addresses set deleting = true where kind = 'primary' and name = 'db-ipv4'`,
			"1 primary IPs have a running action; wait for it to finish before rolling back",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			m := newPrimaryMigration(t)
			m.seedFixture(t)
			_, err := m.goose.Up(m.ctx)
			require.NoError(t, err)
			m.exec(t, tc.mutate)
			const snap = `select x.id, x.name, x.server_id, x.auto_delete, x.releasing_from_id, a.owner, a.server_id
				from ip_addresses x join ip_allocations a on a.id = x.allocation_id order by x.id`
			before := m.rows(t, snap)
			v := m.version(t)

			_, err = m.goose.Down(m.ctx)
			require.ErrorContains(t, err, tc.msg)
			require.Equal(t, before, m.rows(t, snap))
			require.Equal(t, v, m.version(t))
			require.Equal(t, 1, m.count(t,
				`select count(*) from information_schema.columns where table_name = 'servers' and column_name = 'instance_gen'`))
		})
	}
}

func TestPrimaryIPsDownRefusesOrphanAllocation(t *testing.T) {
	m := newPrimaryMigration(t)
	f := m.seedFixture(t)
	_, err := m.goose.Up(m.ctx)
	require.NoError(t, err)
	m.exec(t, `insert into ip_allocations (pool_id, ip, owner) values ($1, '10.0.0.98', 'primary')`, f.v4)
	const allocs = `select id, owner, server_id from ip_allocations order by id`
	const addrs = `select id, kind, name, server_id from ip_addresses order by id`
	beforeAllocs, beforeAddrs := m.rows(t, allocs), m.rows(t, addrs)
	v := m.version(t)

	_, err = m.goose.Down(m.ctx)
	require.ErrorContains(t, err, "1 primary allocations have no primary IP row; cannot roll back")
	require.Equal(t, beforeAllocs, m.rows(t, allocs))
	require.Equal(t, beforeAddrs, m.rows(t, addrs))
	require.Equal(t, v, m.version(t))
}

func TestPrimaryIPsMigrationCountCheck(t *testing.T) {
	m := newPrimaryMigration(t)
	f := m.seedFixture(t)
	// ip_allocations.server_id has no foreign key, so a dangling id needs no FK bypass.
	m.alloc(t, f.v4, "10.0.0.99", 999999)
	const snap = `select a.id, a.owner, a.server_id from ip_allocations a order by a.id`
	before := m.rows(t, snap)

	_, err := m.goose.Up(m.ctx)
	require.ErrorContains(t, err, "primary IP migration count mismatch")
	require.Equal(t, before, m.rows(t, snap))
	require.Zero(t, m.count(t, `select count(*) from ip_addresses where kind = 'primary'`))
	require.Equal(t, m.schema, m.version(t))
}

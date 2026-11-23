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

package floatingips_test

import (
	"context"
	"database/sql"
	"fmt"
	"net/netip"
	"testing"

	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/ipallocation"
	"github.com/nebla-cloud/nebla/internal/floatingips"
	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/stretchr/testify/require"
)

type seeded struct {
	c      *ent.Client
	sql    *sql.DB
	s1, s2 int64
	add    func(ip string, srv int64, releasing *int64)
}

func seed(t *testing.T) seeded {
	ctx := context.Background()
	c, sqlDB := testutil.NewEntSQL(t)
	p := c.Project.Create().SetName("p").SaveX(ctx)
	loc := c.Location.Create().SetName("eu-1").SetPveURL("https://pve.invalid").SetTokenID("t").SetTokenSecret([]byte("s")).
		SetDiskStorage("local-lvm").SetIsoStorage("local").SaveX(ctx)
	st := c.ServerType.Create().SetName("small").SetCores(1).SetMemoryMB(512).SetDiskGB(10).SaveX(ctx)
	img := c.Image.Create().SetName("ubuntu").SetOsFlavor("ubuntu").SetOsVersion("24.04").SaveX(ctx)
	pool := c.IPPool.Create().SetLocationID(loc.ID).SetFamily("ipv4").SetBridge("vmbr0").SetCidr("203.0.113.0/24").
		SetGateway("203.0.113.1").SetDNS([]string{"1.1.1.1"}).SetRangeStart("203.0.113.2").SetRangeEnd("203.0.113.200").SaveX(ctx)
	server := func(n int) int64 {
		return c.Server.Create().SetProjectID(p.ID).SetName(fmt.Sprintf("s%d", n)).SetLocationID(loc.ID).SetTemplateVmid(9000).
			SetServerTypeID(st.ID).SetImageID(img.ID).SetMAC(fmt.Sprintf("bc:24:11:00:00:%02x", n)).SetLabels(map[string]string{}).
			SetCores(1).SetMemoryMB(512).SetDiskGB(10).SetSSHKeys([]string{}).SaveX(ctx).ID
	}
	s1, s2 := server(1), server(2)
	add := func(ip string, srv int64, releasing *int64) {
		a := c.IPAllocation.Create().SetPoolID(pool.ID).SetIP(ip).SetOwner(ipallocation.OwnerFloating).SaveX(ctx)
		cr := c.IPAddress.Create().SetProjectID(p.ID).SetLocationID(loc.ID).SetAllocationID(a.ID).SetFamily("ipv4").SetIP(ip).
			SetName("f" + ip).SetLabels(map[string]string{}).SetServerID(srv)
		if releasing != nil {
			cr.SetReleasingFromID(*releasing)
		}
		cr.SaveX(ctx)
	}
	return seeded{c: c, sql: sqlDB, s1: s1, s2: s2, add: add}
}

func TestAllowedIPsSkipsReleasing(t *testing.T) {
	ctx := context.Background()
	e := seed(t)
	c, s1, s2 := e.c, e.s1, e.s2
	e.add("203.0.113.10", s1, nil)
	e.add("203.0.113.11", s1, &s2)
	e.add("203.0.113.12", s2, &s1)

	got, err := floatingips.AllowedIPs(ctx, c, s1)
	require.NoError(t, err)
	require.Equal(t, []netip.Addr{netip.MustParseAddr("203.0.113.10")}, got)
	got, err = floatingips.AllowedIPs(ctx, c, s2)
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestLockAssignedTakesShareLock(t *testing.T) {
	ctx := context.Background()
	e := seed(t)
	e.add("203.0.113.10", e.s1, nil)
	e.add("203.0.113.12", e.s2, nil)

	tx, err := e.c.Tx(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })
	require.NoError(t, floatingips.LockAssigned(ctx, tx.Client(), e.s1))

	try := func(clause string, server int64) error {
		other, err := e.sql.BeginTx(ctx, nil)
		require.NoError(t, err)
		defer other.Rollback()
		_, err = other.ExecContext(ctx, "SELECT id FROM ip_addresses WHERE server_id = $1 "+clause, server)
		return err
	}
	require.Error(t, try("FOR UPDATE NOWAIT", e.s1))
	require.NoError(t, try("FOR SHARE NOWAIT", e.s1))
	require.NoError(t, try("FOR UPDATE NOWAIT", e.s2))
}

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

package floatingips

import (
	"context"
	"net/netip"
	"slices"

	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/ipaddress"
	"github.com/nebla-cloud/nebla/internal/ent/predicate"
)

// AllowedIPs lists the floating IPs the server may use, sorted; ones still releasing from it are excluded.
func AllowedIPs(ctx context.Context, c *ent.Client, serverID int64) ([]netip.Addr, error) {
	return assigned(ctx, c, ipaddress.ServerID(serverID), ipaddress.ReleasingFromIDIsNil(), floating)
}

// GuestIPs lists every floating IP assigned to the server, sorted, including ones still releasing to it; for the guest's own config.
func GuestIPs(ctx context.Context, c *ent.Client, serverID int64) ([]netip.Addr, error) {
	return assigned(ctx, c, ipaddress.ServerID(serverID), floating)
}

func assigned(ctx context.Context, c *ent.Client, where ...predicate.IPAddress) ([]netip.Addr, error) {
	rows, err := c.IPAddress.Query().Where(where...).All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]netip.Addr, 0, len(rows))
	for _, r := range rows {
		a, err := netip.ParseAddr(r.IP)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	slices.SortFunc(out, netip.Addr.Compare)
	return out, nil
}

// LockAssigned takes FOR SHARE on the server's IP rows, floating and primary; ipset writers hold it for the length of their write.
func LockAssigned(ctx context.Context, tx *ent.Client, serverID int64) error {
	_, err := tx.IPAddress.Query().Where(ipaddress.ServerID(serverID)).ForShare().IDs(ctx)
	return err
}

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
	"fmt"
	"net/netip"

	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/servernetwork"
	"github.com/nebla-cloud/nebla/internal/ent/vnetslot"
	"github.com/nebla-cloud/nebla/internal/firewalls"
)

// NIC is one private NIC of a server.
type NIC struct {
	Index int
	IP    netip.Addr
	MAC   string
	VNet  string
	MTU   int
	State string
}

// PrivateNICs lists the server's private NICs in every state, by index.
func PrivateNICs(ctx context.Context, c *ent.Client, serverID int64) ([]NIC, error) {
	atts, err := c.ServerNetwork.Query().Where(servernetwork.ServerID(serverID)).Order(servernetwork.ByNic()).All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]NIC, 0, len(atts))
	for _, a := range atts {
		ip, err := netip.ParseAddr(a.IP)
		if err != nil {
			return nil, fmt.Errorf("server network %d has a bad address: %w", a.ID, err)
		}
		n := NIC{Index: a.Nic, IP: ip, MAC: a.MAC, State: string(a.State)}
		if sl, err := c.VNetSlot.Query().Where(vnetslot.NetworkID(a.NetworkID)).Only(ctx); err == nil {
			n.VNet, n.MTU = sl.Vnet, sl.Mtu
		} else if !ent.IsNotFound(err) {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}

// LockNICs share-locks the server's attachment rows so none changes mid-apply.
func LockNICs(ctx context.Context, tx *ent.Client, serverID int64) error {
	_, err := tx.ServerNetwork.Query().Where(servernetwork.ServerID(serverID)).ForShare().IDs(ctx)
	return err
}

// VXLANLocation reports whether any of the location's VNets sits in a vxlan zone.
func VXLANLocation(ctx context.Context, c *ent.Client, locationID int64) (bool, error) {
	return c.VNetSlot.Query().Where(vnetslot.LocationID(locationID), vnetslot.ZoneType("vxlan")).Exist(ctx)
}

// SystemFor is the firewall renderer's Nebla-owned input for a server.
func SystemFor(ctx context.Context, c *ent.Client, serverID int64) (firewalls.System, error) {
	srv, err := c.Server.Get(ctx, serverID)
	if err != nil {
		return firewalls.System{}, err
	}
	nics, err := PrivateNICs(ctx, c, serverID)
	if err != nil {
		return firewalls.System{}, err
	}
	var sys firewalls.System
	for _, n := range nics {
		sys.PrivateNICs = append(sys.PrivateNICs, n.Index)
	}
	sys.VXLANGuard, err = VXLANLocation(ctx, c, srv.LocationID)
	return sys, err
}

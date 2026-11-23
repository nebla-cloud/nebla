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

package servers

import (
	"context"
	"net/netip"

	"github.com/nebla-cloud/nebla/internal/cloudinit"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/servernetwork"
	"github.com/nebla-cloud/nebla/internal/floatingips"
	"github.com/nebla-cloud/nebla/internal/ipam"
	"github.com/nebla-cloud/nebla/internal/networks"
)

// InstanceGeneration is the cloud-init instance-id generation; 0 means the ISO generation.
func InstanceGeneration(row *ent.Server) int {
	if row.InstanceGeneration > 0 {
		return row.InstanceGeneration
	}
	return row.IsoGeneration
}

// seedInput is the seed ISO input minus the instance-id; the build and the fingerprint both use it.
func seedInput(ctx context.Context, c *ent.Client, row *ent.Server, assign []ipam.Assignment) (cloudinit.Input, error) {
	fips, err := floatingips.GuestIPs(ctx, c, row.ID)
	if err != nil {
		return cloudinit.Input{}, err
	}
	priv, err := privateSeed(ctx, c, row.ID)
	if err != nil {
		return cloudinit.Input{}, err
	}
	return cloudinit.Input{
		Hostname: row.Name, SSHKeys: row.SSHKeys, UserData: row.UserData, MAC: row.MAC, IPs: assign, Floating: fips,
		Private: priv,
	}, nil
}

// privateSeed lists the server's private NICs for cloud-init, leaving out ones being detached.
func privateSeed(ctx context.Context, c *ent.Client, serverID int64) ([]cloudinit.PrivateNIC, error) {
	nics, err := networks.PrivateNICs(ctx, c, serverID)
	if err != nil {
		return nil, err
	}
	var out []cloudinit.PrivateNIC
	for _, n := range nics {
		if n.State == string(servernetwork.StateDetaching) {
			continue
		}
		out = append(out, cloudinit.PrivateNIC{MAC: n.MAC, Index: n.Index, Addr: netip.PrefixFrom(n.IP, n.Bits), MTU: n.MTU})
	}
	return out, nil
}

// NetworkFingerprint hashes the network-config the server's seed ISO would carry now.
func NetworkFingerprint(ctx context.Context, c *ent.Client, row *ent.Server) (string, error) {
	assign, err := ipam.Assignments(ctx, c, row.ID)
	if err != nil {
		return "", err
	}
	in, err := seedInput(ctx, c, row, assign)
	if err != nil {
		return "", err
	}
	return cloudinit.NetworkFingerprint(in)
}

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
	"net/netip"

	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/server"
)

// netRefresh is true once servers carry net_refresh; it is the single place the flag is read.
func netRefresh(srv *ent.Server) bool { return false }

var netRefreshFn = netRefresh

// FlavorOf returns the OS flavor of the server's image, or "" when it has none.
func FlavorOf(ctx context.Context, c *ent.Client, srv *ent.Server) (string, error) {
	if srv.Edges.Image != nil {
		return srv.Edges.Image.OsFlavor, nil
	}
	img, err := srv.QueryImage().Only(ctx)
	if ent.IsNotFound(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return img.OsFlavor, nil
}

// GuestFor returns the commands to show a member after attaching or detaching nic, or nil when none apply.
func GuestFor(srv *ent.Server, n NIC, networkID int64, flavor string, attach bool) *GuestCommands {
	refresh := netRefreshFn(srv)
	if !attach {
		if refresh {
			return nil
		}
		rm := Removal(flavor, networkID, true)
		if rm == "" {
			return nil
		}
		return &GuestCommands{Remove: rm}
	}
	running := srv.Status == server.StatusRunning
	if !running && refresh {
		return nil
	}
	g := Guest(flavor, networkID, n.MAC, netip.PrefixFrom(n.IP, n.Bits), n.MTU, !refresh)
	if !running {
		g.Note = "Run these after starting the server."
	}
	return &g
}

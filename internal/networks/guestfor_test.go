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

package networks_test

import (
	"net/netip"
	"testing"

	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/server"
	"github.com/nebla-cloud/nebla/internal/networks"
	"github.com/stretchr/testify/require"
)

func TestGuestForMatrix(t *testing.T) {
	nic := networks.NIC{IP: netip.MustParseAddr("10.0.0.5"), MAC: mac, Bits: 24, MTU: 1450}
	run := &ent.Server{Status: server.StatusRunning}
	off := &ent.Server{Status: server.StatusOff}
	cases := []struct {
		name                   string
		srv                    *ent.Server
		refresh, attach        bool
		nilWant                bool
		apply, persist, remove bool
		note                   string
	}{
		{"attach running refresh", run, true, true, false, true, false, false, ""},
		{"attach running no refresh", run, false, true, false, true, true, true, ""},
		{"attach stopped refresh", off, true, true, true, false, false, false, ""},
		{"attach stopped no refresh", off, false, true, false, true, true, true, "Run these after starting the server."},
		{"detach no refresh", run, false, false, false, false, false, true, ""},
		{"detach refresh", run, true, false, true, false, false, false, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer networks.SetNetRefresh(c.refresh)()
			g := networks.GuestFor(c.srv, nic, 7, "ubuntu", c.attach)
			if c.nilWant {
				require.Nil(t, g)
				return
			}
			require.NotNil(t, g)
			require.Equal(t, c.apply, g.Apply != "")
			require.Equal(t, c.persist, g.Persist != "")
			require.Equal(t, c.remove, g.Remove != "")
			require.Equal(t, c.note, g.Note)
		})
	}
	defer networks.SetNetRefresh(false)()
	require.Nil(t, networks.GuestFor(run, nic, 7, "generic", false))
}

func TestGuestForFollowsNetRefresh(t *testing.T) {
	nic := networks.NIC{IP: netip.MustParseAddr("10.0.0.5"), MAC: mac, Bits: 24, MTU: 1450}
	old := &ent.Server{Status: server.StatusRunning}
	g := networks.GuestFor(old, nic, 7, "ubuntu", true)
	require.NotNil(t, g)
	require.NotEmpty(t, g.Persist, "a server without net_refresh persists the NIC itself")
	require.NotEmpty(t, networks.GuestFor(old, nic, 7, "ubuntu", false).Remove)

	fresh := &ent.Server{Status: server.StatusRunning, NetRefresh: true}
	g = networks.GuestFor(fresh, nic, 7, "ubuntu", true)
	require.NotNil(t, g)
	require.NotEmpty(t, g.Apply)
	require.Empty(t, g.Persist, "cloud-init writes the NIC at the next boot")
	require.Nil(t, networks.GuestFor(fresh, nic, 7, "ubuntu", false))
	require.Nil(t, networks.GuestFor(&ent.Server{Status: server.StatusOff, NetRefresh: true}, nic, 7, "ubuntu", true))

	// The test hook still wins over the row.
	defer networks.SetNetRefresh(false)()
	require.NotEmpty(t, networks.GuestFor(fresh, nic, 7, "ubuntu", true).Persist)
}

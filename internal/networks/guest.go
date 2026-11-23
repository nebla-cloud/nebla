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
	"fmt"
	"net/netip"
	"strings"
)

// GuestCommands are the commands a member runs inside the server to use a private network.
type GuestCommands struct{ Apply, Persist, Remove string }

func netplanFlavor(flavor string) bool { return flavor == "ubuntu" || flavor == "debian" }

func netplanFile(networkID int64) string {
	return fmt.Sprintf("/etc/netplan/60-nebla-net-%d.yaml", networkID)
}

// Guest returns the apply, persist and remove commands for a private NIC on a guest of the given OS flavor.
func Guest(flavor string, networkID int64, mac string, addr netip.Prefix, mtu int, persist bool) GuestCommands {
	apply := []string{fmt.Sprintf("IF=$(ip -br link | awk '$3 == \"%s\" {print $1}')", mac)}
	if mtu > 0 && mtu != 1500 {
		apply = append(apply, fmt.Sprintf("sudo ip link set \"$IF\" mtu %d", mtu))
	}
	apply = append(apply,
		"sudo ip link set \"$IF\" up",
		fmt.Sprintf("sudo ip addr add %s dev \"$IF\"", addr),
	)
	g := GuestCommands{Apply: strings.Join(apply, "\n")}
	if !persist {
		return g
	}
	if !netplanFlavor(flavor) {
		g.Persist = fmt.Sprintf("To keep it after a reboot, add %s to the interface with MAC %s in your network configuration.", addr, mac)
		return g
	}
	cfg := fmt.Sprintf("nebla-net-%d: {match: {macaddress: \"%s\"}, addresses: [\"%s\"]", networkID, mac, addr)
	if mtu > 0 && mtu != 1500 {
		cfg += fmt.Sprintf(", mtu: %d", mtu)
	}
	file := netplanFile(networkID)
	g.Persist = fmt.Sprintf("echo 'network: {version: 2, ethernets: {%s}}}' | sudo tee %s >/dev/null && sudo chmod 600 %s && sudo netplan apply", cfg, file, file)
	g.Remove = Removal(flavor, networkID, persist)
	return g
}

// Removal returns the command that deletes the persisted configuration, or "" when there is none.
func Removal(flavor string, networkID int64, persist bool) string {
	if !persist || !netplanFlavor(flavor) {
		return ""
	}
	return fmt.Sprintf("sudo rm -f %s && sudo netplan apply", netplanFile(networkID))
}

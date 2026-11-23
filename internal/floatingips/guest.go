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
	"fmt"
	"net/netip"
	"strings"
)

// GuestCommands are the commands a member runs inside the server to use a floating IP.
type GuestCommands struct{ Apply, Persist string }

func netplanFlavor(flavor string) bool { return flavor == "ubuntu" || flavor == "debian" }

func prefix(ip netip.Addr) string {
	if ip.Is4() {
		return fmt.Sprintf("%s/32", ip)
	}
	return fmt.Sprintf("%s/128", ip)
}

func netplanFile(ip netip.Addr) string {
	r := strings.NewReplacer(".", "-", ":", "-")
	return "/etc/netplan/60-nebla-floating-" + r.Replace(ip.String()) + ".yaml"
}

// Guest returns the apply and persist commands for ip on a guest of the given OS flavor.
func Guest(flavor string, ip netip.Addr) GuestCommands {
	var apply []string
	if ip.Is4() {
		install := "# Install arping from iputils with your package manager."
		if netplanFlavor(flavor) {
			install = "sudo apt-get install -y iputils-arping"
		}
		apply = []string{
			fmt.Sprintf("sudo ip addr add %s dev eth0", prefix(ip)),
			install,
			fmt.Sprintf("sudo arping -U -c 3 -I eth0 %s", ip),
		}
	} else {
		apply = []string{
			"sudo sysctl -w net.ipv6.conf.eth0.ndisc_notify=1",
			fmt.Sprintf("sudo ip -6 addr add %s dev eth0 preferred_lft 0", prefix(ip)),
		}
	}
	g := GuestCommands{Apply: strings.Join(apply, "\n")}
	if !netplanFlavor(flavor) {
		g.Persist = fmt.Sprintf("To keep it after a reboot, add %s to eth0 in your network configuration.", prefix(ip))
		return g
	}
	addr := fmt.Sprintf(`"%s"`, prefix(ip))
	if ip.Is6() {
		addr = fmt.Sprintf(`{"%s": {lifetime: 0}}`, prefix(ip))
	}
	file := netplanFile(ip)
	g.Persist = fmt.Sprintf("echo 'network: {version: 2, ethernets: {eth0: {addresses: [%s]}}}' | sudo tee %s >/dev/null && sudo chmod 600 %s && sudo netplan apply", addr, file, file)
	return g
}

// Removal returns the commands that take ip off the guest again.
func Removal(flavor string, ip netip.Addr) string {
	del := fmt.Sprintf("sudo ip addr del %s dev eth0", prefix(ip))
	if ip.Is6() {
		del = fmt.Sprintf("sudo ip -6 addr del %s dev eth0", prefix(ip))
	}
	if !netplanFlavor(flavor) {
		return del
	}
	return del + fmt.Sprintf("\nsudo rm -f %s && sudo netplan apply", netplanFile(ip))
}

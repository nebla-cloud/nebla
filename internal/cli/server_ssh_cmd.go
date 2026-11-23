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

package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

// sshUser maps an image os_flavor to its default cloud-init user.
func sshUser(osFlavor string) string {
	switch f := strings.ToLower(osFlavor); f {
	case "debian", "ubuntu", "rocky", "fedora":
		return f
	case "almalinux", "alma":
		return "almalinux"
	case "centos":
		return "cloud-user"
	}
	return "root"
}

func serverSSHCmd(a *app) *cobra.Command {
	var user string
	var port int
	var v6 bool
	cmd := &cobra.Command{
		Use:   "ssh <ref> [-- ssh args...]",
		Short: "Open an SSH session to a server",
		Args:  cobra.MinimumNArgs(1),
	}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		extra := []string(nil)
		if d := cmd.ArgsLenAtDash(); d >= 0 {
			extra = args[d:]
			args = args[:d]
			if len(extra) > 0 && !strings.HasPrefix(extra[0], "-") {
				return errors.New("arguments after -- are ssh options; to run a remote command use ssh directly")
			}
		}
		if len(args) != 1 {
			return fmt.Errorf("expected one server, got %d", len(args))
		}
		if port < 1 || port > 65535 {
			return fmt.Errorf("invalid port %d", port)
		}
		api, err := a.api()
		if err != nil {
			return err
		}
		srv, err := api.Server(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		ip := srv.PublicNet.Ipv4
		has4 := ip != nil && ip.Ip != ""
		if v6 || !has4 {
			ip = srv.PublicNet.Ipv6
		}
		if ip == nil || ip.Ip == "" {
			if v6 && has4 {
				return fmt.Errorf("server %s has no IPv6 address", srv.Name)
			}
			return fmt.Errorf("server %s has no public IP", srv.Name)
		}
		if user == "" {
			user = sshUser(srv.OsFlavor)
		}
		look := a.env.LookPath
		if look == nil {
			look = exec.LookPath
		}
		path, err := look("ssh")
		if err != nil {
			return fmt.Errorf("ssh not found: %w", err)
		}
		argv := []string{"ssh"}
		if port != 22 {
			argv = append(argv, "-p", fmt.Sprint(port))
		}
		argv = append(argv, extra...)
		argv = append(argv, user+"@"+ip.Ip)
		return a.env.Exec(path, argv, os.Environ())
	}
	f := cmd.Flags()
	f.StringVarP(&user, "user", "u", "", "login user (default depends on the image)")
	f.IntVarP(&port, "port", "p", 22, "SSH port")
	f.BoolVar(&v6, "ipv6", false, "connect over IPv6")
	return cmd
}

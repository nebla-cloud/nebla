package cli

import (
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
		}
		if len(args) != 1 {
			return fmt.Errorf("expected one server, got %d", len(args))
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
		if v6 || ip == nil || ip.Ip == "" {
			ip = srv.PublicNet.Ipv6
		}
		if ip == nil || ip.Ip == "" {
			return fmt.Errorf("server %s has no public IP", srv.Name)
		}
		if user == "" {
			user = sshUser(srv.Image.OsFlavor)
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

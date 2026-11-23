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
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/nebla-cloud/nebla/internal/cli/apiclient"
	"github.com/spf13/cobra"
)

// ipString returns the address or "-" when the server has none.
func ipString(ip *apiclient.ServerIp) string {
	if ip == nil || ip.Ip == "" {
		return "-"
	}
	return ip.Ip
}

// ipWithName adds the primary IP's name to its address when it is known.
func ipWithName(ip *apiclient.ServerIp, names map[int64]string) string {
	if n, ok := names[idOf(ip)]; ok && ip != nil {
		return ip.Ip + " (" + n + ")"
	}
	return ipString(ip)
}

func idOf(ip *apiclient.ServerIp) int64 {
	if ip == nil {
		return 0
	}
	return ip.Id
}

func labelString(l apiclient.Labels) string {
	if len(l) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(l))
	for k, v := range l {
		parts = append(parts, k+"="+v)
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

func serverFirewalls(fws []apiclient.ServerFirewall) string {
	if len(fws) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(fws))
	for _, f := range fws {
		via := f.Via
		if via != "direct" {
			via = "via " + via
		}
		parts = append(parts, fmt.Sprintf("%s (%s)", f.Name, via))
	}
	return strings.Join(parts, ", ")
}

func serverPrivateNets(s apiclient.Server) string {
	if len(s.PrivateNet) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(s.PrivateNet))
	for _, n := range s.PrivateNet {
		parts = append(parts, fmt.Sprintf("%s (%s)", n.Name, n.Ip))
	}
	return strings.Join(parts, ", ")
}

func parseLabels(in []string) (apiclient.Labels, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := apiclient.Labels{}
	for _, s := range in {
		k, v, ok := strings.Cut(s, "=")
		if !ok {
			return nil, fmt.Errorf("invalid label %q: want key=value", s)
		}
		out[k] = v
	}
	return out, nil
}

func newServerCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{Use: "server", Short: "Manage servers"}
	cmd.AddCommand(serverCreateCmd(a), serverListCmd(a), serverDescribeCmd(a), serverDeleteCmd(a), serverRenameCmd(a))
	cmd.AddCommand(serverSSHCmd(a), newBackupCmd(a))
	cmd.AddCommand(serverActionCmds(a)...)
	return cmd
}

// ipFlagValue sets a public_net union from an --ipv4/--ipv6 value: a bool, or else a primary IP ref.
func ipFlagValue(v string, fromBool func(bool) error, fromRef func(string) error) error {
	switch v {
	case "true", "false":
		return fromBool(v == "true")
	case "":
		return errors.New("an empty IP value: use true, false or a primary IP id or name")
	}
	return fromRef(v)
}

func serverCreateCmd(a *app) *cobra.Command {
	var (
		name, typ, image, location, userDataFile string
		sshKeys, labels, firewalls, networks     []string
		withoutIPv4, withIPv6, start             bool
		backups                                  bool
		backupKeep                               int
		ipv4, ipv6                               string
	)
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a server",
		Args:  cobra.NoArgs,
	}
	wo := addWaitFlags(cmd)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		lbl, err := parseLabels(labels)
		if err != nil {
			return err
		}
		if cmd.Flags().Changed("backup-keep") && !backups {
			return errors.New("--backup-keep needs --backups")
		}
		body := apiclient.PostServersJSONRequestBody{
			Name: name, ServerType: typ, Image: &image, Location: location,
			StartAfterCreate: &start,
		}
		fl := cmd.Flags()
		if fl.Changed("ipv4") && fl.Changed("without-ipv4") {
			return errors.New("use either --ipv4 or --without-ipv4, not both")
		}
		if fl.Changed("ipv6") && fl.Changed("with-ipv6") {
			return errors.New("use either --ipv6 or --with-ipv6, not both")
		}
		if !fl.Changed("ipv4") && withoutIPv4 {
			ipv4 = "false"
		}
		if !fl.Changed("ipv6") && withIPv6 {
			ipv6 = "true"
		}
		var u4 apiclient.ServerInput_PublicNet_Ipv4
		var u6 apiclient.ServerInput_PublicNet_Ipv6
		if err := ipFlagValue(ipv4, u4.FromServerInputPublicNetIpv40, u4.FromServerInputPublicNetIpv41); err != nil {
			return err
		}
		if err := ipFlagValue(ipv6, u6.FromServerInputPublicNetIpv60, u6.FromServerInputPublicNetIpv61); err != nil {
			return err
		}
		body.PublicNet = &struct {
			Ipv4 *apiclient.ServerInput_PublicNet_Ipv4 `json:"ipv4,omitempty"`
			Ipv6 *apiclient.ServerInput_PublicNet_Ipv6 `json:"ipv6,omitempty"`
		}{Ipv4: &u4, Ipv6: &u6}
		if lbl != nil {
			body.Labels = &lbl
		}
		if backups {
			body.Backups = &apiclient.BackupSettings{Enabled: true, Keep: backupKeep}
		}
		if len(sshKeys) > 0 {
			body.SshKeys = &sshKeys
		}
		api, err := a.api()
		if err != nil {
			return err
		}
		if len(firewalls) > 0 {
			var fws []struct {
				Firewall int64 `json:"firewall"`
			}
			for _, ref := range firewalls {
				f, err := api.Firewall(cmd.Context(), ref)
				if err != nil {
					return err
				}
				fws = append(fws, struct {
					Firewall int64 `json:"firewall"`
				}{f.Id})
			}
			body.Firewalls = &fws
		}
		if len(networks) > 0 {
			var nets []struct {
				Ip      *string `json:"ip,omitempty"`
				Network string  `json:"network"`
			}
			for _, v := range networks {
				ref, ip, hasIP := strings.Cut(v, "=")
				n := struct {
					Ip      *string `json:"ip,omitempty"`
					Network string  `json:"network"`
				}{Network: ref}
				if hasIP {
					n.Ip = &ip
				}
				nets = append(nets, n)
			}
			body.Networks = &nets
		}
		if userDataFile != "" {
			b, err := os.ReadFile(userDataFile)
			if err != nil {
				return err
			}
			s := string(b)
			body.UserData = &s
		}
		res, err := api.C.PostServersWithResponse(cmd.Context(), body)
		if err != nil {
			return err
		}
		created, err := result(res.StatusCode(), res.Body, res.JSON201)
		if err != nil {
			return err
		}
		out := a.env.Stdout
		if wo.NoWait {
			if a.flags.output != "table" {
				return a.printer().Object(created)
			}
			fmt.Fprintf(out, "Server %s is being created (action %s)\n", id64(created.Server.Id), id64(created.Action.Id))
			return nil
		}
		ids := []int64{created.Action.Id}
		for _, n := range created.NextActions {
			ids = append(ids, n.Id)
		}
		after := func(err error) error {
			return fmt.Errorf("server %s was created, but: %w", id64(created.Server.Id), err)
		}
		api.Timeout = wo.Timeout
		done, err := api.Wait(cmd.Context(), a.env.Stderr, a.stderrTTY(), ids...)
		if err != nil {
			return after(err)
		}
		sres, err := api.C.GetServerWithResponse(cmd.Context(), created.Server.Id)
		if err != nil {
			return after(err)
		}
		srv, err := result(sres.StatusCode(), sres.Body, sres.JSON200)
		if err != nil {
			return after(err)
		}
		if a.flags.output != "table" {
			return a.printer().Object(map[string]any{"server": srv, "action": done[0], "next_actions": done[1:]})
		}
		fmt.Fprintf(out, "Server %s created\n", id64(srv.Id))
		if srv.PublicNet.Ipv4 != nil {
			fmt.Fprintf(out, "IPv4: %s\n", ipString(srv.PublicNet.Ipv4))
		}
		if srv.PublicNet.Ipv6 != nil {
			fmt.Fprintf(out, "IPv6: %s\n", ipString(srv.PublicNet.Ipv6))
		}
		return nil
	}
	f := cmd.Flags()
	f.StringVar(&name, "name", "", "server name")
	f.StringVar(&typ, "type", "", "server type id or name")
	f.StringVar(&image, "image", "", "image id or name")
	f.StringVar(&location, "location", "", "location id or name")
	f.StringArrayVar(&sshKeys, "ssh-key", nil, "SSH key id or name (repeatable)")
	f.StringArrayVar(&firewalls, "firewall", nil, "firewall id or name to apply (repeatable)")
	f.StringArrayVar(&networks, "network", nil, "private network id or name, optionally name=ip (repeatable)")
	f.StringVar(&userDataFile, "user-data-from-file", "", "read cloud-init user data from a file")
	f.StringArrayVar(&labels, "label", nil, "label as key=value (repeatable)")
	f.StringVar(&ipv4, "ipv4", "true", "IPv4 address: true for a new one, false for none, or a primary IP id or name")
	f.StringVar(&ipv6, "ipv6", "false", "IPv6 address: true for a new one, false for none, or a primary IP id or name")
	f.BoolVar(&withoutIPv4, "without-ipv4", false, "same as --ipv4=false")
	f.BoolVar(&withIPv6, "with-ipv6", false, "same as --ipv6=true")
	f.BoolVar(&start, "start-after-create", true, "start the server once it is created")
	f.BoolVar(&backups, "backups", false, "turn on scheduled backups")
	f.IntVar(&backupKeep, "backup-keep", defaultBackupKeep, "backups to keep, 1-14 (with --backups)")
	for _, n := range []string{"name", "type", "image", "location"} {
		_ = cmd.MarkFlagRequired(n)
	}
	return cmd
}

func serverListCmd(a *app) *cobra.Command {
	var selector string
	var statuses []string
	return listCmd(a, "list", "List servers",
		[]string{"ID", "NAME", "STATUS", "IPV4", "IPV6", "TYPE", "IMAGE", "LOCATION", "AGE"},
		func(c *cobra.Command) {
			c.Flags().StringVar(&selector, "selector", "", "label selector, e.g. env=prod")
			c.Flags().StringArrayVar(&statuses, "status", nil, "only servers in this status (repeatable)")
		},
		func(c *cobra.Command, api *API) ([]apiclient.Server, error) {
			var p apiclient.GetServersParams
			if selector != "" {
				p.LabelSelector = &selector
			}
			if len(statuses) > 0 {
				p.Status = &statuses
			}
			return api.Servers(c.Context(), p)
		},
		func(s apiclient.Server) []string {
			return []string{id64(s.Id), s.Name, string(s.Status), ipString(s.PublicNet.Ipv4), ipString(s.PublicNet.Ipv6),
				s.ServerType.Name, serverImageName(s), s.Location.Name, age(s.Created, time.Now())}
		})
}

func serverDescribeCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "describe <ref>",
		Short: "Show a server by id or name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			api, err := a.api()
			if err != nil {
				return err
			}
			s, err := api.Server(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			p := a.printer()
			if a.flags.output != "table" {
				return p.Object(s)
			}
			t := s.ServerType
			names := serverIPNames(cmd.Context(), api, s.Id)
			p.KV([][2]string{
				{"ID", id64(s.Id)},
				{"Name", s.Name},
				{"Status", string(s.Status)},
				{"Created", s.Created.Format(time.RFC3339)},
				{"IPv4", ipWithName(s.PublicNet.Ipv4, names)},
				{"IPv6", ipWithName(s.PublicNet.Ipv6, names)},
				{"Type", fmt.Sprintf("%s (%d cores, %s, %d GB)", t.Name, t.Cores, memoryString(t.MemoryMb), t.DiskGb)},
				{"Image", serverImageName(s)},
				{"Location", s.Location.Name},
				{"Disk", fmt.Sprintf("%d GB", s.DiskGb)},
				{"Labels", labelString(s.Labels)},
				{"Firewalls", serverFirewalls(s.Firewalls)},
				{"Private networks", serverPrivateNets(s)},
			})
			return nil
		},
	}
}

func serverDeleteCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <ref>",
		Short: "Delete a server by id or name",
		Args:  cobra.ExactArgs(1),
	}
	var keepAll bool
	var keep []string
	cmd.Flags().BoolVar(&keepAll, "keep-ips", false, "keep all of the server's primary IPs in the project")
	cmd.Flags().StringArrayVar(&keep, "keep-ip", nil, "keep this primary IP id or name (repeatable)")
	wo := addWaitFlags(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		api, err := a.api()
		if err != nil {
			return err
		}
		s, err := api.Server(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		plan, err := keepPrimaryIPs(cmd.Context(), api, s, keepAll, keep)
		if err != nil {
			return err
		}
		kept := plan.kept
		if a.flags.output == "table" && len(plan.survivors) > 0 {
			fmt.Fprintf(a.env.Stderr, "These primary IPs stay in the project after the delete: %s\n", pipNames(plan.survivors))
		}
		res, err := api.C.DeleteServerWithResponse(cmd.Context(), s.Id)
		var v *struct {
			Action apiclient.Action `json:"action"`
		}
		if err == nil {
			v, err = result(res.StatusCode(), res.Body, res.JSON200)
		}
		if err != nil {
			if len(plan.changed) > 0 {
				fmt.Fprintf(a.env.Stderr, "The server was not deleted, but auto-delete is now off for: %s. Turn it back on with `nebla primary-ip update <ref> --auto-delete=true`.\n", pipNames(plan.changed))
			}
			return err
		}
		act, err := a.waitAction(cmd, api, wo, v.Action)
		if err != nil {
			return err
		}
		if a.flags.output != "table" {
			return a.printer().Object(act)
		}
		if wo.NoWait {
			fmt.Fprintf(a.env.Stdout, "Server %s is being deleted (action %s)\n", s.Name, id64(act.Id))
			return nil
		}
		fmt.Fprintf(a.env.Stdout, "Server %s deleted\n", s.Name)
		for _, p := range kept {
			fmt.Fprintf(a.env.Stdout, "Kept %s (%s); it stays in the project and counts toward quota\n", p.Name, p.Ip)
		}
		return nil
	}
	return cmd
}

// keepPlan is what keepPrimaryIPs did: the IPs asked to be kept, the ones whose auto-delete it turned off, and every IP that survives.
type keepPlan struct {
	kept, changed, survivors []apiclient.PrimaryIp
}

func pipNames(ps []apiclient.PrimaryIp) string {
	names := make([]string, len(ps))
	for i, p := range ps {
		names[i] = fmt.Sprintf("%s (%s)", p.Name, p.Ip)
	}
	return strings.Join(names, ", ")
}

// keepPrimaryIPs turns off auto-delete on the server's IPs that should outlive it, before the delete.
func keepPrimaryIPs(ctx context.Context, api *API, s apiclient.Server, all bool, refs []string) (keepPlan, error) {
	var plan keepPlan
	if all && len(refs) > 0 {
		return plan, errors.New("use either --keep-ips or --keep-ip, not both")
	}
	sid := id64(s.Id)
	own, err := api.PrimaryIPs(ctx, apiclient.GetPrimaryIpsParams{Server: &sid})
	if err != nil {
		return plan, err
	}
	if all {
		plan.kept = own
	}
	for _, ref := range refs {
		p, err := api.PrimaryIP(ctx, ref)
		if err != nil {
			return plan, err
		}
		if p.Server == nil || p.Server.Id != s.Id {
			return plan, fmt.Errorf("primary IP %q is not assigned to server %s", ref, s.Name)
		}
		plan.kept = append(plan.kept, p)
	}
	off := false
	for _, p := range plan.kept {
		if !p.AutoDelete {
			continue
		}
		if _, err := putPrimaryIP(ctx, api, p.Id, apiclient.PutPrimaryIpJSONRequestBody{AutoDelete: &off}); err != nil {
			return plan, err
		}
		plan.changed = append(plan.changed, p)
	}
	// Survivors: the ones just kept plus the ones already stored with auto-delete off.
	for _, p := range own {
		if !p.AutoDelete || slices.ContainsFunc(plan.kept, func(k apiclient.PrimaryIp) bool { return k.Id == p.Id }) {
			plan.survivors = append(plan.survivors, p)
		}
	}
	return plan, nil
}

func serverRenameCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "rename <ref> <new-name>",
		Short: "Rename a server",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			newName := args[1]
			if newName == "" {
				return errors.New("new name is empty")
			}
			api, err := a.api()
			if err != nil {
				return err
			}
			s, err := api.Server(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			res, err := api.C.PutServerWithResponse(cmd.Context(), s.Id, apiclient.PutServerJSONRequestBody{Name: &newName})
			if err != nil {
				return err
			}
			v, err := result(res.StatusCode(), res.Body, res.JSON200)
			if err != nil {
				return err
			}
			if a.flags.output != "table" {
				return a.printer().Object(v)
			}
			fmt.Fprintf(a.env.Stdout, "Server %s renamed to %s\n", s.Name, v.Name)
			return nil
		},
	}
}

// serverImageName is the server's image, or "-" when it was built from a snapshot whose image is gone.
func serverImageName(s apiclient.Server) string {
	if s.Image == nil {
		return "-"
	}
	return s.Image.Name
}

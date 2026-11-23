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
	"io"
	"strings"
	"time"

	"github.com/nebla-cloud/nebla/internal/cli/apiclient"
	"github.com/spf13/cobra"
)

func newFloatingIPCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{Use: "floating-ip", Short: "Manage floating IPs"}
	cmd.AddCommand(fipListCmd(a), fipDescribeCmd(a), fipCreateCmd(a), fipUpdateCmd(a),
		fipAssignCmd(a), fipUnassignCmd(a), fipDeleteCmd(a))
	return cmd
}

func fipServerNames(ctx context.Context, api *API, fips ...apiclient.FloatingIp) map[int64]string {
	names := map[int64]string{}
	for _, f := range fips {
		if f.Server != nil {
			if list, err := api.Servers(ctx, apiclient.GetServersParams{}); err == nil {
				for _, s := range list {
					names[s.Id] = s.Name
				}
			}
			break
		}
	}
	return names
}

func fipServer(f apiclient.FloatingIp, names map[int64]string) string {
	if f.Server == nil {
		return "-"
	}
	if n, ok := names[*f.Server]; ok {
		return n
	}
	return id64(*f.Server)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// printGuestConfig writes the commands to run inside the servers; it prints nothing when there are none.
func printGuestConfig(w io.Writer, g *apiclient.GuestConfig, fallback string) {
	if g == nil {
		return
	}
	server := deref(g.Server)
	if server == "" {
		server = fallback
	}
	if apply := deref(g.Apply); apply != "" {
		fmt.Fprintf(w, "\nRun this on %s:\n%s\n", server, strings.TrimRight(apply, "\n"))
	}
	if persist := deref(g.Persist); persist != "" {
		fmt.Fprintf(w, "\nTo keep it after a reboot:\n%s\n", strings.TrimRight(persist, "\n"))
	}
	if remove := deref(g.Remove); remove != "" {
		fmt.Fprintf(w, "\nRun this on %s to stop using the address:\n%s\n", deref(g.PreviousServer), strings.TrimRight(remove, "\n"))
	}
}

func fipListCmd(a *app) *cobra.Command {
	var selector, server, location string
	var names map[int64]string
	return listCmd(a, "list", "List floating IPs",
		[]string{"ID", "NAME", "IP", "LOCATION", "SERVER", "STATUS"},
		func(c *cobra.Command) {
			c.Flags().StringVarP(&selector, "selector", "l", "", "label selector, e.g. env=prod")
			c.Flags().StringVar(&server, "server", "", "only floating IPs assigned to this server id or name")
			c.Flags().StringVar(&location, "location", "", "only floating IPs in this location id or name")
		},
		func(c *cobra.Command, api *API) ([]apiclient.FloatingIp, error) {
			var p apiclient.GetFloatingIpsParams
			if selector != "" {
				p.LabelSelector = &selector
			}
			if server != "" {
				p.Server = &server
			}
			if location != "" {
				p.Location = &location
			}
			list, err := api.FloatingIPs(c.Context(), p)
			if err != nil {
				return nil, err
			}
			if a.flags.output == "table" {
				names = fipServerNames(c.Context(), api, list...)
			}
			return list, nil
		},
		func(f apiclient.FloatingIp) []string {
			return []string{id64(f.Id), f.Name, f.Ip, f.Location.Name, fipServer(f, names), string(f.Status)}
		})
}

func fipDescribeCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "describe <ref>",
		Short: "Show a floating IP by id, name or address",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			api, err := a.api()
			if err != nil {
				return err
			}
			f, err := api.FloatingIP(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			p := a.printer()
			if a.flags.output != "table" {
				return p.Object(f)
			}
			srv := fipServer(f, fipServerNames(cmd.Context(), api, f))
			p.KV([][2]string{
				{"ID", id64(f.Id)},
				{"Name", f.Name},
				{"Description", f.Description},
				{"Type", string(f.Type)},
				{"IP", f.Ip},
				{"Status", string(f.Status)},
				{"Created", f.Created.Format(time.RFC3339)},
				{"Location", f.Location.Name},
				{"Server", srv},
				{"Labels", labelString(f.Labels)},
			})
			printGuestConfig(a.env.Stdout, f.GuestConfig, srv)
			return nil
		},
	}
}

// fipFinish waits for act unless --no-wait, then prints the result and, for table output, the guest config.
func fipFinish(a *app, cmd *cobra.Command, api *API, wo *waitOpts, f apiclient.FloatingIp, act apiclient.Action, started, done string) error {
	if !wo.NoWait {
		api.Timeout = wo.Timeout
		fin, err := api.Wait(cmd.Context(), a.env.Stderr, a.stderrTTY(), act.Id)
		if err != nil {
			return err
		}
		act = fin[0]
		res, err := api.C.GetFloatingIpWithResponse(cmd.Context(), f.Id)
		if err != nil {
			return err
		}
		cur, err := result(res.StatusCode(), res.Body, res.JSON200)
		if err != nil {
			return err
		}
		f = *cur
	}
	if a.flags.output != "table" {
		if wo.NoWait {
			return a.printer().Object(map[string]any{"floating_ip": f, "action": act})
		}
		return a.printer().Object(f)
	}
	out := a.env.Stdout
	if wo.NoWait {
		fmt.Fprintf(out, "Floating IP %s %s (action %s)\n", f.Name, started, id64(act.Id))
		return nil
	}
	fmt.Fprintf(out, "Floating IP %s %s\n", f.Name, done)
	printGuestConfig(out, f.GuestConfig, "")
	return nil
}

func fipCreateCmd(a *app) *cobra.Command {
	var typ, name, description, location, server string
	var labels []string
	cmd := &cobra.Command{Use: "create", Short: "Create a floating IP", Args: cobra.NoArgs}
	wo := addWaitFlags(cmd)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if (location == "") == (server == "") {
			return errors.New("set exactly one of --location and --server")
		}
		lbl, err := parseLabels(labels)
		if err != nil {
			return err
		}
		api, err := a.api()
		if err != nil {
			return err
		}
		body := apiclient.PostFloatingIpsJSONRequestBody{Type: apiclient.FloatingIpInputType(typ)}
		if name != "" {
			body.Name = &name
		}
		if description != "" {
			body.Description = &description
		}
		if lbl != nil {
			body.Labels = &lbl
		}
		if location != "" {
			body.Location = &location
		}
		if server != "" {
			body.Server = &server
		}
		res, err := api.C.PostFloatingIpsWithResponse(cmd.Context(), body)
		if err != nil {
			return err
		}
		created, err := result(res.StatusCode(), res.Body, res.JSON201)
		if err != nil {
			return err
		}
		f := created.FloatingIp
		if created.Action == nil {
			if a.flags.output != "table" {
				return a.printer().Object(f)
			}
			fmt.Fprintf(a.env.Stdout, "Floating IP %s created (%s)\n", f.Name, f.Ip)
			return nil
		}
		return fipFinish(a, cmd, api, wo, f, *created.Action, "is being created", "created ("+f.Ip+")")
	}
	fl := cmd.Flags()
	fl.StringVar(&typ, "type", "", "address family: ipv4 or ipv6")
	fl.StringVar(&name, "name", "", "name (generated from the address when omitted)")
	fl.StringVar(&description, "description", "", "description")
	fl.StringVar(&location, "location", "", "location id or name")
	fl.StringVar(&server, "server", "", "server id or name to assign to")
	fl.StringArrayVar(&labels, "label", nil, "label as key=value (repeatable)")
	_ = cmd.MarkFlagRequired("type")
	return cmd
}

func fipUpdateCmd(a *app) *cobra.Command {
	var name, description string
	var labels []string
	cmd := &cobra.Command{Use: "update <ref>", Short: "Change a floating IP's name, description or labels", Args: cobra.ExactArgs(1)}
	cmd.Flags().StringVar(&name, "name", "", "new name")
	cmd.Flags().StringVar(&description, "description", "", "new description")
	cmd.Flags().StringArrayVar(&labels, "label", nil, "label as key=value (repeatable); replaces all labels")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		fl := cmd.Flags()
		if !fl.Changed("name") && !fl.Changed("description") && len(labels) == 0 {
			return errors.New("nothing to update: pass --name, --description or --label")
		}
		lbl, err := parseLabels(labels)
		if err != nil {
			return err
		}
		api, err := a.api()
		if err != nil {
			return err
		}
		f, err := api.FloatingIP(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		var body apiclient.PutFloatingIpJSONRequestBody
		if fl.Changed("name") {
			body.Name = &name
		}
		if fl.Changed("description") {
			body.Description = &description
		}
		if lbl != nil {
			body.Labels = &lbl
		}
		res, err := api.C.PutFloatingIpWithResponse(cmd.Context(), f.Id, body)
		if err != nil {
			return err
		}
		upd, err := result(res.StatusCode(), res.Body, res.JSON200)
		if err != nil {
			return err
		}
		if a.flags.output != "table" {
			return a.printer().Object(upd)
		}
		fmt.Fprintf(a.env.Stdout, "Floating IP %s updated\n", upd.Name)
		return nil
	}
	return cmd
}

func fipAssignCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{Use: "assign <ref> <server>", Short: "Assign a floating IP to a server", Args: cobra.ExactArgs(2)}
	wo := addWaitFlags(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		api, err := a.api()
		if err != nil {
			return err
		}
		f, err := api.FloatingIP(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		res, err := api.C.AssignFloatingIpWithResponse(cmd.Context(), f.Id, apiclient.FloatingIpAssignInput{Server: args[1]})
		if err != nil {
			return err
		}
		created, err := result(res.StatusCode(), res.Body, res.JSON201)
		if err != nil {
			return err
		}
		return fipFinish(a, cmd, api, wo, f, created.Action, "is being assigned", "assigned to "+args[1])
	}
	return cmd
}

func fipUnassignCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{Use: "unassign <ref>", Short: "Unassign a floating IP from its server", Args: cobra.ExactArgs(1)}
	wo := addWaitFlags(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		api, err := a.api()
		if err != nil {
			return err
		}
		f, err := api.FloatingIP(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		res, err := api.C.UnassignFloatingIpWithResponse(cmd.Context(), f.Id)
		if err != nil {
			return err
		}
		created, err := result(res.StatusCode(), res.Body, res.JSON201)
		if err != nil {
			return err
		}
		return fipFinish(a, cmd, api, wo, f, created.Action, "is being unassigned", "unassigned")
	}
	return cmd
}

func fipDeleteCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{Use: "delete <ref>", Short: "Delete a floating IP", Args: cobra.ExactArgs(1)}
	wo := addWaitFlags(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		api, err := a.api()
		if err != nil {
			return err
		}
		f, err := api.FloatingIP(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		res, err := api.C.DeleteFloatingIpWithResponse(cmd.Context(), f.Id)
		if err != nil {
			return err
		}
		out, err := result(res.StatusCode(), res.Body, res.JSON200)
		if err != nil {
			return err
		}
		act := out.Action
		if !wo.NoWait {
			api.Timeout = wo.Timeout
			done, err := api.Wait(cmd.Context(), a.env.Stderr, a.stderrTTY(), act.Id)
			if err != nil {
				return err
			}
			act = done[0]
		}
		if a.flags.output != "table" {
			return a.printer().Object(act)
		}
		if wo.NoWait {
			fmt.Fprintf(a.env.Stdout, "Floating IP %s is being deleted (action %s)\n", f.Name, id64(act.Id))
			return nil
		}
		fmt.Fprintf(a.env.Stdout, "Floating IP %s deleted\n", f.Name)
		return nil
	}
	return cmd
}

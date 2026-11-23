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
	"strconv"
	"strings"
	"time"

	"github.com/nebla-cloud/nebla/internal/cli/apiclient"
	"github.com/spf13/cobra"
)

func newNetworkCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{Use: "network", Short: "Manage private networks"}
	cmd.AddCommand(netListCmd(a), netDescribeCmd(a), netCreateCmd(a), netUpdateCmd(a), netDeleteCmd(a))
	return cmd
}

func netListCmd(a *app) *cobra.Command {
	var selector, location string
	return listCmd(a, "list", "List private networks",
		[]string{"ID", "NAME", "LOCATION", "RANGE", "SERVERS"},
		func(c *cobra.Command) {
			c.Flags().StringVarP(&selector, "selector", "l", "", "label selector, e.g. env=prod")
			c.Flags().StringVar(&location, "location", "", "only networks in this location id or name")
		},
		func(c *cobra.Command, api *API) ([]apiclient.Network, error) {
			var p apiclient.GetNetworksParams
			if selector != "" {
				p.LabelSelector = &selector
			}
			if location != "" {
				p.Location = &location
			}
			return api.Networks(c.Context(), p)
		},
		func(n apiclient.Network) []string {
			return []string{id64(n.Id), n.Name, n.Location.Name, n.IpRange, strconv.Itoa(len(n.Servers))}
		})
}

func netServers(n apiclient.Network) string {
	if len(n.Servers) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(n.Servers))
	for _, s := range n.Servers {
		parts = append(parts, fmt.Sprintf("%s (%s)", s.Name, s.Ip))
	}
	return strings.Join(parts, ", ")
}

func netDescribeCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "describe <ref>",
		Short: "Show a private network by id or name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			api, err := a.api()
			if err != nil {
				return err
			}
			n, err := api.Network(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			p := a.printer()
			if a.flags.output != "table" {
				return p.Object(n)
			}
			p.KV([][2]string{
				{"ID", id64(n.Id)},
				{"Name", n.Name},
				{"Range", n.IpRange},
				{"Created", n.Created.Format(time.RFC3339)},
				{"Location", n.Location.Name},
				{"Servers", netServers(n)},
				{"Labels", labelString(n.Labels)},
			})
			return nil
		},
	}
}

func netCreateCmd(a *app) *cobra.Command {
	var name, location, ipRange string
	var labels []string
	cmd := &cobra.Command{Use: "create", Short: "Create a private network", Args: cobra.NoArgs}
	f := cmd.Flags()
	f.StringVar(&name, "name", "", "network name")
	f.StringVar(&location, "location", "", "location id or name")
	f.StringVar(&ipRange, "ip-range", "", "IPv4 range in CIDR form (the first free 10.N.0.0/16 when omitted)")
	f.StringArrayVar(&labels, "label", nil, "label as key=value (repeatable)")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("location")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		lbl, err := parseLabels(labels)
		if err != nil {
			return err
		}
		api, err := a.api()
		if err != nil {
			return err
		}
		body := apiclient.PostNetworksJSONRequestBody{Name: name, Location: location}
		if ipRange != "" {
			body.IpRange = &ipRange
		}
		if lbl != nil {
			body.Labels = &lbl
		}
		res, err := api.C.PostNetworksWithResponse(cmd.Context(), body)
		if err != nil {
			return err
		}
		created, err := result(res.StatusCode(), res.Body, res.JSON201)
		if err != nil {
			return err
		}
		if a.flags.output != "table" {
			return a.printer().Object(created.Network)
		}
		fmt.Fprintf(a.env.Stdout, "Network %s created (%s)\n", created.Network.Name, created.Network.IpRange)
		return nil
	}
	return cmd
}

func netUpdateCmd(a *app) *cobra.Command {
	var name string
	var labels []string
	cmd := &cobra.Command{Use: "update <ref>", Short: "Change a private network's name or labels", Args: cobra.ExactArgs(1)}
	cmd.Flags().StringVar(&name, "name", "", "new name")
	cmd.Flags().StringArrayVar(&labels, "label", nil, "label as key=value (repeatable); replaces all labels")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if !cmd.Flags().Changed("name") && len(labels) == 0 {
			return errors.New("nothing to update: pass --name or --label")
		}
		lbl, err := parseLabels(labels)
		if err != nil {
			return err
		}
		api, err := a.api()
		if err != nil {
			return err
		}
		n, err := api.Network(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		var body apiclient.PutNetworkJSONRequestBody
		if cmd.Flags().Changed("name") {
			body.Name = &name
		}
		if lbl != nil {
			body.Labels = &lbl
		}
		res, err := api.C.PutNetworkWithResponse(cmd.Context(), n.Id, body)
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
		fmt.Fprintf(a.env.Stdout, "Network %s updated\n", upd.Name)
		return nil
	}
	return cmd
}

func netDeleteCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "delete <ref>",
		Short: "Delete a private network",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			api, err := a.api()
			if err != nil {
				return err
			}
			n, err := api.Network(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			res, err := api.C.DeleteNetworkWithResponse(cmd.Context(), n.Id)
			if err != nil {
				return err
			}
			if res.StatusCode() < 200 || res.StatusCode() > 299 {
				return apiErr(res.StatusCode(), res.Body)
			}
			if a.flags.output != "table" {
				return nil
			}
			fmt.Fprintf(a.env.Stdout, "Network %s deleted\n", n.Name)
			return nil
		},
	}
}

// printNetworkGuestConfig writes the commands to run inside the server; it prints nothing when there are none.
func printNetworkGuestConfig(a *app, g *apiclient.NetworkGuestConfig, server string) {
	if g == nil {
		return
	}
	w := a.env.Stdout
	if apply := deref(g.Apply); apply != "" {
		if note := deref(g.Note); note != "" {
			fmt.Fprintf(w, "\n%s\n", note)
			fmt.Fprintf(w, "Run this on %s after starting it:\n%s\n", server, strings.TrimRight(apply, "\n"))
		} else {
			fmt.Fprintf(w, "\nRun this on %s:\n%s\n", server, strings.TrimRight(apply, "\n"))
		}
	} else if note := deref(g.Note); note != "" {
		fmt.Fprintf(w, "\n%s\n", note)
	}
	if persist := deref(g.Persist); persist != "" {
		fmt.Fprintf(w, "\nTo keep it after a reboot:\n%s\n", strings.TrimRight(persist, "\n"))
	}
	if remove := deref(g.Remove); remove != "" {
		fmt.Fprintf(w, "\nRun this on %s to remove the saved configuration:\n%s\n", server, strings.TrimRight(remove, "\n"))
	}
}

func serverNetworkCmd(a *app, use, short, past string, attach bool) *cobra.Command {
	var network, ip string
	cmd := &cobra.Command{Use: use + " <server>", Short: short, Args: cobra.ExactArgs(1)}
	wo := addWaitFlags(cmd)
	cmd.Flags().StringVar(&network, "network", "", "network id or name")
	_ = cmd.MarkFlagRequired("network")
	if attach {
		cmd.Flags().StringVar(&ip, "ip", "", "address on the network (the lowest free one when omitted)")
	}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		api, err := a.api()
		if err != nil {
			return err
		}
		s, err := api.Server(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		var created *apiclient.NetworkActionCreated
		if attach {
			body := apiclient.NetworkAttachInput{Network: network}
			if ip != "" {
				body.Ip = &ip
			}
			res, err := api.C.AttachServerToNetworkWithResponse(cmd.Context(), s.Id, body)
			if err != nil {
				return err
			}
			created, err = result(res.StatusCode(), res.Body, res.JSON201)
			if err != nil {
				return err
			}
		} else {
			res, err := api.C.DetachServerFromNetworkWithResponse(cmd.Context(), s.Id, apiclient.NetworkDetachInput{Network: network})
			if err != nil {
				return err
			}
			created, err = result(res.StatusCode(), res.Body, res.JSON201)
			if err != nil {
				return err
			}
		}
		out := *created
		if !wo.NoWait {
			api.Timeout = wo.Timeout
			fin, err := api.Wait(cmd.Context(), a.env.Stderr, a.stderrTTY(), out.Action.Id)
			if err != nil {
				return err
			}
			out.Action = fin[0]
		}
		if a.flags.output != "table" {
			return a.printer().Object(out)
		}
		if wo.NoWait {
			fmt.Fprintf(a.env.Stdout, "Server %s: action %s started\n", s.Name, id64(out.Action.Id))
		} else {
			fmt.Fprintf(a.env.Stdout, "Server %s %s network %s\n", s.Name, past, network)
		}
		printNetworkGuestConfig(a, out.GuestConfig, s.Name)
		return nil
	}
	return cmd
}

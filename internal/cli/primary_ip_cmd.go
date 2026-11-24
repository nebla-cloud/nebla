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
	"strconv"
	"time"

	"github.com/nebla-cloud/nebla/internal/cli/apiclient"
	"github.com/spf13/cobra"
)

const ipTakesEffect = "The new address takes effect when the server starts."

func newPrimaryIPCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{Use: "primary-ip", Short: "Manage primary IPs"}
	cmd.AddCommand(pipListCmd(a), pipDescribeCmd(a), pipCreateCmd(a), pipUpdateCmd(a),
		pipAssignCmd(a), pipUnassignCmd(a), pipDeleteCmd(a))
	return cmd
}

func pipServer(p apiclient.PrimaryIp) string {
	if p.Server == nil {
		return "-"
	}
	return p.Server.Name
}

func pipListCmd(a *app) *cobra.Command {
	var selector, server, location, typ string
	return listCmd(a, "list", "List primary IPs",
		[]string{"ID", "NAME", "IP", "TYPE", "LOCATION", "SERVER", "AUTO-DELETE"},
		func(c *cobra.Command) {
			c.Flags().StringVarP(&selector, "selector", "l", "", "label selector, e.g. env=prod")
			c.Flags().StringVar(&server, "server", "", "only primary IPs assigned to this server id or name")
			c.Flags().StringVar(&location, "location", "", "only primary IPs in this location id or name")
			c.Flags().StringVar(&typ, "type", "", "only this address family: ipv4 or ipv6")
		},
		func(c *cobra.Command, api *API) ([]apiclient.PrimaryIp, error) {
			var p apiclient.GetPrimaryIpsParams
			if selector != "" {
				p.LabelSelector = &selector
			}
			if server != "" {
				p.Server = &server
			}
			if location != "" {
				p.Location = &location
			}
			if typ != "" {
				t := apiclient.GetPrimaryIpsParamsType(typ)
				p.Type = &t
			}
			return api.PrimaryIPs(c.Context(), p)
		},
		func(p apiclient.PrimaryIp) []string {
			return []string{id64(p.Id), p.Name, p.Ip, string(p.Type), p.Location.Name, pipServer(p), strconv.FormatBool(p.AutoDelete)}
		})
}

func pipDescribeCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "describe <ref>",
		Short: "Show a primary IP by id, name or address",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			api, err := a.api()
			if err != nil {
				return err
			}
			p, err := api.PrimaryIP(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if a.flags.output != "table" {
				return a.printer().Object(p)
			}
			a.printer().KV([][2]string{
				{"ID", id64(p.Id)},
				{"Name", p.Name},
				{"Description", p.Description},
				{"Type", string(p.Type)},
				{"IP", p.Ip},
				{"Status", string(p.Status)},
				{"Auto-delete", strconv.FormatBool(p.AutoDelete)},
				{"Created", p.Created.Format(time.RFC3339)},
				{"Location", p.Location.Name},
				{"Server", pipServer(p)},
				{"Labels", labelString(p.Labels)},
			})
			return nil
		},
	}
}

func pipCreateCmd(a *app) *cobra.Command {
	var typ, name, description, location string
	var labels []string
	var keep bool
	cmd := &cobra.Command{Use: "create", Short: "Reserve a primary IP", Args: cobra.NoArgs}
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		lbl, err := parseLabels(labels)
		if err != nil {
			return err
		}
		api, err := a.api()
		if err != nil {
			return err
		}
		body := apiclient.PostPrimaryIpsJSONRequestBody{Type: apiclient.PrimaryIpInputType(typ), Location: location}
		if name != "" {
			body.Name = &name
		}
		if description != "" {
			body.Description = &description
		}
		if lbl != nil {
			body.Labels = &lbl
		}
		if keep {
			off := false
			body.AutoDelete = &off
		}
		res, err := api.C.PostPrimaryIpsWithResponse(cmd.Context(), body)
		if err != nil {
			return err
		}
		created, err := result(res.StatusCode(), res.Body, res.JSON201)
		if err != nil {
			return err
		}
		if a.flags.output != "table" {
			return a.printer().Object(created.PrimaryIp)
		}
		fmt.Fprintf(a.env.Stdout, "Primary IP %s created (%s)\n", created.PrimaryIp.Name, created.PrimaryIp.Ip)
		return nil
	}
	fl := cmd.Flags()
	fl.StringVar(&typ, "type", "", "address family: ipv4 or ipv6")
	fl.StringVar(&location, "location", "", "location id or name")
	fl.StringVar(&name, "name", "", "name (generated when omitted)")
	fl.StringVar(&description, "description", "", "description")
	fl.StringArrayVar(&labels, "label", nil, "label as key=value (repeatable)")
	fl.BoolVar(&keep, "keep", false, "keep the address when its server is deleted (auto-delete off)")
	_ = cmd.MarkFlagRequired("type")
	_ = cmd.MarkFlagRequired("location")
	return cmd
}

func pipUpdateCmd(a *app) *cobra.Command {
	var name, description string
	var labels []string
	var autoDelete bool
	cmd := &cobra.Command{Use: "update <ref>", Short: "Change a primary IP's name, description, labels or auto-delete", Args: cobra.ExactArgs(1)}
	fl := cmd.Flags()
	fl.StringVar(&name, "name", "", "new name")
	fl.StringVar(&description, "description", "", "new description")
	fl.StringArrayVar(&labels, "label", nil, "label as key=value (repeatable); replaces all labels")
	fl.BoolVar(&autoDelete, "auto-delete", false, "delete the address with its server: true or false")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if !fl.Changed("name") && !fl.Changed("description") && !fl.Changed("auto-delete") && len(labels) == 0 {
			return errors.New("nothing to update: pass --name, --description, --label or --auto-delete")
		}
		lbl, err := parseLabels(labels)
		if err != nil {
			return err
		}
		api, err := a.api()
		if err != nil {
			return err
		}
		p, err := api.PrimaryIP(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		var body apiclient.PutPrimaryIpJSONRequestBody
		if fl.Changed("name") {
			body.Name = &name
		}
		if fl.Changed("description") {
			body.Description = &description
		}
		if fl.Changed("auto-delete") {
			body.AutoDelete = &autoDelete
		}
		if lbl != nil {
			body.Labels = &lbl
		}
		upd, err := putPrimaryIP(cmd.Context(), api, p.Id, body)
		if err != nil {
			return err
		}
		if a.flags.output != "table" {
			return a.printer().Object(upd)
		}
		fmt.Fprintf(a.env.Stdout, "Primary IP %s updated\n", upd.Name)
		return nil
	}
	return cmd
}

func putPrimaryIP(ctx context.Context, api *API, id int64, body apiclient.PutPrimaryIpJSONRequestBody) (*apiclient.PrimaryIp, error) {
	res, err := api.C.PutPrimaryIpWithResponse(ctx, id, body)
	if err != nil {
		return nil, err
	}
	return result(res.StatusCode(), res.Body, res.JSON200)
}

// pipFinish waits for the assign or unassign action, then prints the result; warnings go to stderr in table output.
func pipFinish(a *app, cmd *cobra.Command, api *API, wo *waitOpts, p apiclient.PrimaryIp, created *apiclient.PrimaryIpActionCreated, started, done string) error {
	act, err := a.waitAction(cmd, api, wo, created.Action)
	if err != nil {
		return err
	}
	if !wo.NoWait {
		res, err := api.C.GetPrimaryIpWithResponse(cmd.Context(), p.Id)
		if err != nil {
			return err
		}
		cur, err := result(res.StatusCode(), res.Body, res.JSON200)
		if err != nil {
			return err
		}
		p = *cur
	}
	if a.flags.output != "table" {
		return a.printer().Object(map[string]any{"primary_ip": p, "action": act, "warnings": created.Warnings})
	}
	for _, w := range created.Warnings {
		fmt.Fprintf(a.env.Stderr, "Warning: %s\n", w)
	}
	if wo.NoWait {
		fmt.Fprintf(a.env.Stdout, "Primary IP %s %s (action %s)\n", p.Name, started, id64(act.Id))
		return nil
	}
	fmt.Fprintf(a.env.Stdout, "Primary IP %s %s\n%s\n", p.Name, done, ipTakesEffect)
	return nil
}

func pipAssignCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{Use: "assign <ref> <server>", Short: "Assign a primary IP to a stopped server", Args: cobra.ExactArgs(2)}
	wo := addWaitFlags(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		api, err := a.api()
		if err != nil {
			return err
		}
		p, err := api.PrimaryIP(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		res, err := api.C.AssignPrimaryIpWithResponse(cmd.Context(), p.Id, apiclient.PrimaryIpAssignInput{Server: args[1]})
		if err != nil {
			return err
		}
		created, err := result(res.StatusCode(), res.Body, res.JSON201)
		if err != nil {
			return err
		}
		return pipFinish(a, cmd, api, wo, p, created, "is being assigned", "assigned to "+args[1])
	}
	return cmd
}

func pipUnassignCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{Use: "unassign <ref>", Short: "Unassign a primary IP from its stopped server", Args: cobra.ExactArgs(1)}
	wo := addWaitFlags(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		api, err := a.api()
		if err != nil {
			return err
		}
		p, err := api.PrimaryIP(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		res, err := api.C.UnassignPrimaryIpWithResponse(cmd.Context(), p.Id)
		if err != nil {
			return err
		}
		created, err := result(res.StatusCode(), res.Body, res.JSON201)
		if err != nil {
			return err
		}
		return pipFinish(a, cmd, api, wo, p, created, "is being unassigned", "unassigned")
	}
	return cmd
}

func pipDeleteCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{Use: "delete <ref>", Short: "Delete an unassigned primary IP", Args: cobra.ExactArgs(1)}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		api, err := a.api()
		if err != nil {
			return err
		}
		p, err := api.PrimaryIP(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		res, err := api.C.DeletePrimaryIpWithResponse(cmd.Context(), p.Id)
		if err != nil {
			return err
		}
		if res.StatusCode() != 204 {
			return apiErr(res.StatusCode(), res.Body)
		}
		if a.flags.output != "table" {
			return a.printer().Object(p)
		}
		fmt.Fprintf(a.env.Stdout, "Primary IP %s deleted\n", p.Name)
		return nil
	}
	return cmd
}

// serverIPNames maps a server's primary IP ids to their names with one list call.
func serverIPNames(ctx context.Context, api *API, serverID int64) map[int64]string {
	ref := id64(serverID)
	names := map[int64]string{}
	if list, err := api.PrimaryIPs(ctx, apiclient.GetPrimaryIpsParams{Server: &ref}); err == nil {
		for _, p := range list {
			names[p.Id] = p.Name
		}
	}
	return names
}

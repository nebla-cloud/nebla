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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/nebla-cloud/nebla/internal/cli/apiclient"
	"github.com/spf13/cobra"
)

func newFirewallCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{Use: "firewall", Short: "Manage firewalls"}
	cmd.AddCommand(firewallListCmd(a), firewallDescribeCmd(a), firewallCreateCmd(a), firewallUpdateCmd(a), firewallDeleteCmd(a),
		firewallAddRuleCmd(a), firewallDeleteRuleCmd(a), firewallReplaceRulesCmd(a))
	cmd.AddCommand(firewallResourceCmd(a, true), firewallResourceCmd(a, false))
	return cmd
}

func firewallAppliedTo(f apiclient.Firewall) string {
	if len(f.AppliedTo) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(f.AppliedTo))
	for _, r := range f.AppliedTo {
		switch {
		case r.LabelSelector != nil:
			parts = append(parts, "label "+r.LabelSelector.Selector)
		case r.Server != nil:
			parts = append(parts, "server "+id64(r.Server.Id))
		}
	}
	return strings.Join(parts, ", ")
}

func firewallListCmd(a *app) *cobra.Command {
	return listCmd(a, "list", "List firewalls", []string{"ID", "NAME", "RULES", "APPLIED TO", "AGE"}, nil,
		func(c *cobra.Command, api *API) ([]apiclient.Firewall, error) { return api.Firewalls(c.Context()) },
		func(f apiclient.Firewall) []string {
			return []string{id64(f.Id), f.Name, strconv.Itoa(len(f.Rules)), firewallAppliedTo(f), age(f.Created, time.Now())}
		})
}

func ipsString(p *[]string) string {
	if p == nil || len(*p) == 0 {
		return "-"
	}
	return strings.Join(*p, ", ")
}

// ruleRows lists one direction's rules; # is the rule's position among all rules.
func ruleRows(rules []apiclient.FirewallRule, dir apiclient.FirewallRuleDirection) [][]string {
	var rows [][]string
	for i, r := range rules {
		if r.Direction != dir {
			continue
		}
		ips, port, desc := r.SourceIps, "-", ""
		if dir == apiclient.Out {
			ips = r.DestinationIps
		}
		if r.Port != nil {
			port = *r.Port
		}
		if r.Description != nil {
			desc = *r.Description
		}
		rows = append(rows, []string{strconv.Itoa(i + 1), string(r.Protocol), port, ipsString(ips), desc})
	}
	return rows
}

// ruleSummary reads like "inbound tcp 22 from 0.0.0.0/0".
func ruleSummary(r apiclient.FirewallRule) string {
	dir, ips, word := "inbound", r.SourceIps, "from"
	if r.Direction == apiclient.Out {
		dir, ips, word = "outbound", r.DestinationIps, "to"
	}
	out := dir + " " + string(r.Protocol)
	if r.Port != nil {
		out += " " + *r.Port
	}
	if ips != nil && len(*ips) > 0 {
		out += " " + word + " " + strings.Join(*ips, ", ")
	}
	return out
}

func firewallDescribeCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "describe <ref>",
		Short: "Show a firewall by id or name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			api, err := a.api()
			if err != nil {
				return err
			}
			f, err := api.Firewall(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			p := a.printer()
			if a.flags.output != "table" {
				return p.Object(f)
			}
			p.KV([][2]string{
				{"ID", id64(f.Id)},
				{"Name", f.Name},
				{"Created", f.Created.Format(time.RFC3339)},
				{"Labels", labelString(f.Labels)},
				{"Applied to", firewallAppliedTo(f)},
			})
			for _, s := range []struct {
				title, col string
				dir        apiclient.FirewallRuleDirection
			}{{"Inbound", "Sources", apiclient.In}, {"Outbound", "Destinations", apiclient.Out}} {
				rows := ruleRows(f.Rules, s.dir)
				if len(rows) == 0 {
					continue
				}
				fmt.Fprintf(a.env.Stdout, "\n%s rules:\n", s.title)
				p.Table([]string{"#", "Protocol", "Port", s.col, "Description"}, rows)
			}
			if len(f.Rules) > 0 {
				fmt.Fprintln(a.env.Stdout, "\n# is the rule's position across all rules.")
			}
			return nil
		},
	}
}

func readRulesFile(a *app, path string) ([]apiclient.FirewallRule, error) {
	var b []byte
	var err error
	if path == "-" {
		b, err = io.ReadAll(a.env.Stdin)
	} else {
		b, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, err
	}
	var rules []apiclient.FirewallRule
	if err := json.Unmarshal(b, &rules); err != nil {
		return nil, fmt.Errorf("invalid rules file: %w", err)
	}
	return rules, nil
}

// finishActions waits on the actions unless --no-wait, then prints done or the actions.
func finishActions(a *app, cmd *cobra.Command, api *API, wo *waitOpts, acts []apiclient.Action, started, done string) error {
	if !wo.NoWait && len(acts) > 0 {
		ids := make([]int64, len(acts))
		for i, x := range acts {
			ids[i] = x.Id
		}
		api.Timeout = wo.Timeout
		fin, err := api.Wait(cmd.Context(), a.env.Stderr, a.stderrTTY(), ids...)
		if err != nil {
			return err
		}
		acts = fin
	}
	if a.flags.output != "table" {
		if acts == nil {
			acts = []apiclient.Action{}
		}
		return a.printer().Object(acts)
	}
	if wo.NoWait && len(acts) > 0 {
		ids := make([]string, len(acts))
		for i, x := range acts {
			ids[i] = id64(x.Id)
		}
		fmt.Fprintf(a.env.Stdout, "%s (actions %s)\n", started, strings.Join(ids, ", "))
		return nil
	}
	fmt.Fprintln(a.env.Stdout, done)
	return nil
}

func firewallCreateCmd(a *app) *cobra.Command {
	var name, rulesFile string
	var labels []string
	cmd := &cobra.Command{Use: "create", Short: "Create a firewall", Args: cobra.NoArgs}
	wo := addWaitFlags(cmd)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		lbl, err := parseLabels(labels)
		if err != nil {
			return err
		}
		body := apiclient.PostFirewallsJSONRequestBody{Name: name}
		if lbl != nil {
			body.Labels = &lbl
		}
		if rulesFile != "" {
			rules, err := readRulesFile(a, rulesFile)
			if err != nil {
				return err
			}
			body.Rules = &rules
		}
		api, err := a.api()
		if err != nil {
			return err
		}
		res, err := api.C.PostFirewallsWithResponse(cmd.Context(), body)
		if err != nil {
			return err
		}
		v, err := result(res.StatusCode(), res.Body, res.JSON201)
		if err != nil {
			return err
		}
		if !wo.NoWait && len(v.Actions) > 0 {
			ids := make([]int64, len(v.Actions))
			for i, x := range v.Actions {
				ids[i] = x.Id
			}
			api.Timeout = wo.Timeout
			if _, err := api.Wait(cmd.Context(), a.env.Stderr, a.stderrTTY(), ids...); err != nil {
				return fmt.Errorf("firewall %s was created, but: %w", id64(v.Firewall.Id), err)
			}
		}
		if a.flags.output != "table" {
			return a.printer().Object(v.Firewall)
		}
		fmt.Fprintf(a.env.Stdout, "Firewall %s created\n", id64(v.Firewall.Id))
		return nil
	}
	cmd.Flags().StringVar(&name, "name", "", "firewall name")
	cmd.Flags().StringArrayVar(&labels, "label", nil, "label as key=value (repeatable)")
	cmd.Flags().StringVar(&rulesFile, "rules-file", "", "JSON file with the rules array (- for stdin)")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

func firewallUpdateCmd(a *app) *cobra.Command {
	var name string
	var labels []string
	cmd := &cobra.Command{
		Use:   "update <ref>",
		Short: "Rename a firewall or replace its labels",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if name == "" && len(labels) == 0 {
				return errors.New("nothing to update; use --name or --label")
			}
			lbl, err := parseLabels(labels)
			if err != nil {
				return err
			}
			api, err := a.api()
			if err != nil {
				return err
			}
			f, err := api.Firewall(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			var body apiclient.PutFirewallJSONRequestBody
			if name != "" {
				body.Name = &name
			}
			if lbl != nil {
				body.Labels = &lbl
			}
			res, err := api.C.PutFirewallWithResponse(cmd.Context(), f.Id, body)
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
			fmt.Fprintf(a.env.Stdout, "Firewall %s updated\n", v.Name)
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "new name")
	cmd.Flags().StringArrayVar(&labels, "label", nil, "label as key=value (repeatable; replaces all labels)")
	return cmd
}

func firewallDeleteCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{Use: "delete <ref>", Short: "Delete a firewall by id or name", Args: cobra.ExactArgs(1)}
	wo := addWaitFlags(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		api, err := a.api()
		if err != nil {
			return err
		}
		f, err := api.Firewall(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		res, err := api.C.DeleteFirewallWithResponse(cmd.Context(), f.Id)
		if err != nil {
			return err
		}
		v, err := result(res.StatusCode(), res.Body, res.JSON200)
		if err != nil {
			return err
		}
		return finishActions(a, cmd, api, wo, v.Actions, "Firewall "+f.Name+" is being deleted", "Firewall "+f.Name+" deleted")
	}
	return cmd
}

func setRules(a *app, cmd *cobra.Command, api *API, wo *waitOpts, f apiclient.Firewall, rules []apiclient.FirewallRule, done string) error {
	if rules == nil {
		rules = []apiclient.FirewallRule{}
	}
	res, err := api.C.SetFirewallRulesWithResponse(cmd.Context(), f.Id, apiclient.SetFirewallRulesJSONRequestBody{Rules: rules})
	if err != nil {
		return err
	}
	v, err := result(res.StatusCode(), res.Body, res.JSON201)
	if err != nil {
		return err
	}
	return finishActions(a, cmd, api, wo, v.Actions, "Firewall "+f.Name+" rules are being updated", done)
}

func firewallAddRuleCmd(a *app) *cobra.Command {
	var dir, proto, port, desc string
	var src, dst []string
	cmd := &cobra.Command{Use: "add-rule <ref>", Short: "Add a rule to a firewall", Args: cobra.ExactArgs(1)}
	wo := addWaitFlags(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if dir != "in" && dir != "out" {
			return errors.New("--direction must be in or out")
		}
		rule := apiclient.FirewallRule{Direction: apiclient.FirewallRuleDirection(dir), Protocol: apiclient.FirewallRuleProtocol(proto)}
		if port != "" {
			rule.Port = &port
		}
		if desc != "" {
			rule.Description = &desc
		}
		if len(src) > 0 {
			rule.SourceIps = &src
		}
		if len(dst) > 0 {
			rule.DestinationIps = &dst
		}
		api, err := a.api()
		if err != nil {
			return err
		}
		f, err := api.Firewall(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		return setRules(a, cmd, api, wo, f, append(f.Rules, rule), "Rule added to firewall "+f.Name)
	}
	fl := cmd.Flags()
	fl.StringVar(&dir, "direction", "", "in or out")
	fl.StringVar(&proto, "protocol", "", "tcp, udp, icmp, esp or gre")
	fl.StringVar(&port, "port", "", "port (80) or range (8000-8100)")
	fl.StringSliceVar(&src, "source-ips", nil, "CIDRs allowed to connect (inbound only)")
	fl.StringSliceVar(&dst, "destination-ips", nil, "CIDRs the server may reach (outbound only)")
	fl.StringVar(&desc, "description", "", "rule description")
	_ = cmd.MarkFlagRequired("direction")
	_ = cmd.MarkFlagRequired("protocol")
	return cmd
}

func firewallDeleteRuleCmd(a *app) *cobra.Command {
	var index int
	var proto, port string
	cmd := &cobra.Command{
		Use:   "delete-rule <ref>",
		Short: "Delete a rule by its # in describe",
		Long:  "Delete a rule by its # in describe. # is the rule's position across all rules, inbound and outbound.\nUse --protocol and --port to make sure the rule at that position is the one you mean.",
		Args:  cobra.ExactArgs(1),
	}
	wo := addWaitFlags(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		api, err := a.api()
		if err != nil {
			return err
		}
		f, err := api.Firewall(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		if index < 1 || index > len(f.Rules) {
			return fmt.Errorf("firewall %s has no rule %d", f.Name, index)
		}
		victim := f.Rules[index-1]
		desc := ruleSummary(victim)
		if proto != "" && string(victim.Protocol) != proto {
			return fmt.Errorf("rule %d is %s, not protocol %s; nothing changed", index, desc, proto)
		}
		if port != "" && (victim.Port == nil || *victim.Port != port) {
			return fmt.Errorf("rule %d is %s, not port %s; nothing changed", index, desc, port)
		}
		rules := append(append([]apiclient.FirewallRule{}, f.Rules[:index-1]...), f.Rules[index:]...)
		return setRules(a, cmd, api, wo, f, rules, fmt.Sprintf("Rule %d deleted (%s) from firewall %s", index, desc, f.Name))
	}
	cmd.Flags().StringVar(&proto, "protocol", "", "only delete if the rule has this protocol")
	cmd.Flags().StringVar(&port, "port", "", "only delete if the rule has this port")
	cmd.Flags().IntVar(&index, "index", 0, "rule number as shown by describe")
	_ = cmd.MarkFlagRequired("index")
	return cmd
}

func firewallReplaceRulesCmd(a *app) *cobra.Command {
	var file string
	cmd := &cobra.Command{Use: "replace-rules <ref>", Short: "Replace all rules from a JSON file", Args: cobra.ExactArgs(1)}
	wo := addWaitFlags(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		rules, err := readRulesFile(a, file)
		if err != nil {
			return err
		}
		api, err := a.api()
		if err != nil {
			return err
		}
		f, err := api.Firewall(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		return setRules(a, cmd, api, wo, f, rules, "Rules replaced on firewall "+f.Name)
	}
	cmd.Flags().StringVar(&file, "rules-file", "", "JSON file with the rules array (- for stdin)")
	_ = cmd.MarkFlagRequired("rules-file")
	return cmd
}

// firewallResourceCmd builds apply-to-resource (apply) or remove-from-resource.
func firewallResourceCmd(a *app, apply bool) *cobra.Command {
	var typ, server, selector string
	use, short, started, done := "apply-to-resource <ref>", "Apply a firewall to a server or label selector", "is being applied", "applied"
	if !apply {
		use, short, started, done = "remove-from-resource <ref>", "Remove a firewall from a server or label selector", "is being removed", "removed"
	}
	cmd := &cobra.Command{Use: use, Short: short, Args: cobra.ExactArgs(1)}
	wo := addWaitFlags(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		api, err := a.api()
		if err != nil {
			return err
		}
		var res apiclient.FirewallResource
		switch typ {
		case "server":
			if server == "" {
				return errors.New("--server is required with --type server")
			}
			s, err := api.Server(cmd.Context(), server)
			if err != nil {
				return err
			}
			res.Type = apiclient.FirewallResourceTypeServer
			res.Server = &struct {
				Id int64 `json:"id"`
			}{Id: s.Id}
		case "label_selector":
			if selector == "" {
				return errors.New("--label-selector is required with --type label_selector")
			}
			res.Type = apiclient.FirewallResourceTypeLabelSelector
			res.LabelSelector = &struct {
				Selector string `json:"selector"`
			}{Selector: selector}
		default:
			return errors.New("--type must be server or label_selector")
		}
		f, err := api.Firewall(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		var acts []apiclient.Action
		if apply {
			r, err := api.C.ApplyFirewallToResourcesWithResponse(cmd.Context(), f.Id, apiclient.ApplyFirewallToResourcesJSONRequestBody{ApplyTo: []apiclient.FirewallResource{res}})
			if err != nil {
				return err
			}
			v, err := result(r.StatusCode(), r.Body, r.JSON201)
			if err != nil {
				return err
			}
			acts = v.Actions
		} else {
			r, err := api.C.RemoveFirewallFromResourcesWithResponse(cmd.Context(), f.Id, apiclient.RemoveFirewallFromResourcesJSONRequestBody{RemoveFrom: []apiclient.FirewallResource{res}})
			if err != nil {
				return err
			}
			v, err := result(r.StatusCode(), r.Body, r.JSON201)
			if err != nil {
				return err
			}
			acts = v.Actions
		}
		return finishActions(a, cmd, api, wo, acts, "Firewall "+f.Name+" "+started, "Firewall "+f.Name+" "+done)
	}
	fl := cmd.Flags()
	fl.StringVar(&typ, "type", "", "server or label_selector")
	fl.StringVar(&server, "server", "", "server id or name (with --type server)")
	fl.StringVar(&selector, "label-selector", "", "label selector, e.g. role=web (with --type label_selector)")
	_ = cmd.MarkFlagRequired("type")
	return cmd
}

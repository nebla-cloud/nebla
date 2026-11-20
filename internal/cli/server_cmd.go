package cli

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Hexeption/nebla/internal/cli/apiclient"
	"github.com/spf13/cobra"
)

// ipString returns the address or "-" when the server has none.
func ipString(ip *apiclient.ServerIp) string {
	if ip == nil || ip.Ip == "" {
		return "-"
	}
	return ip.Ip
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
	cmd.AddCommand(serverSSHCmd(a))
	cmd.AddCommand(serverActionCmds(a)...)
	return cmd
}

func serverCreateCmd(a *app) *cobra.Command {
	var (
		name, typ, image, location, userDataFile string
		sshKeys, labels                          []string
		withoutIPv4, withIPv6, start             bool
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
		body := apiclient.PostServersJSONRequestBody{
			Name: name, ServerType: typ, Image: image, Location: location,
			StartAfterCreate: &start,
		}
		v4, v6 := !withoutIPv4, withIPv6
		body.PublicNet = &struct {
			Ipv4 *bool `json:"ipv4,omitempty"`
			Ipv6 *bool `json:"ipv6,omitempty"`
		}{Ipv4: &v4, Ipv6: &v6}
		if lbl != nil {
			body.Labels = &lbl
		}
		if len(sshKeys) > 0 {
			body.SshKeys = &sshKeys
		}
		if userDataFile != "" {
			b, err := os.ReadFile(userDataFile)
			if err != nil {
				return err
			}
			s := string(b)
			body.UserData = &s
		}
		api, err := a.api()
		if err != nil {
			return err
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
	f.StringVar(&userDataFile, "user-data-from-file", "", "read cloud-init user data from a file")
	f.StringArrayVar(&labels, "label", nil, "label as key=value (repeatable)")
	f.BoolVar(&withoutIPv4, "without-ipv4", false, "do not assign an IPv4 address")
	f.BoolVar(&withIPv6, "with-ipv6", false, "assign an IPv6 address")
	f.BoolVar(&start, "start-after-create", true, "start the server once it is created")
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
				s.ServerType.Name, s.Image.Name, s.Location.Name, age(s.Created, time.Now())}
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
			p.KV([][2]string{
				{"ID", id64(s.Id)},
				{"Name", s.Name},
				{"Status", string(s.Status)},
				{"Created", s.Created.Format(time.RFC3339)},
				{"IPv4", ipString(s.PublicNet.Ipv4)},
				{"IPv6", ipString(s.PublicNet.Ipv6)},
				{"Type", fmt.Sprintf("%s (%d cores, %s, %d GB)", t.Name, t.Cores, memoryString(t.MemoryMb), t.DiskGb)},
				{"Image", s.Image.Name},
				{"Location", s.Location.Name},
				{"Disk", fmt.Sprintf("%d GB", s.DiskGb)},
				{"Labels", labelString(s.Labels)},
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
		res, err := api.C.DeleteServerWithResponse(cmd.Context(), s.Id)
		if err != nil {
			return err
		}
		v, err := result(res.StatusCode(), res.Body, res.JSON200)
		if err != nil {
			return err
		}
		act := v.Action
		if !wo.NoWait {
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
			fmt.Fprintf(a.env.Stdout, "Server %s is being deleted (action %s)\n", s.Name, id64(act.Id))
			return nil
		}
		fmt.Fprintf(a.env.Stdout, "Server %s deleted\n", s.Name)
		return nil
	}
	return cmd
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

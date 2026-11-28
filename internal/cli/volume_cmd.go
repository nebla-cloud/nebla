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
	"time"

	"github.com/nebla-cloud/nebla/internal/cli/apiclient"
	"github.com/spf13/cobra"
)

func newVolumeCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{Use: "volume", Short: "Manage volumes"}
	cmd.AddCommand(volumeCreateCmd(a), volumeListCmd(a), volumeDescribeCmd(a), volumeUpdateCmd(a),
		volumeAttachCmd(a), volumeDetachCmd(a), volumeResizeCmd(a), volumeDeleteCmd(a))
	return cmd
}

func sizeString(gb int) string { return fmt.Sprintf("%d GB", gb) }

// serverNames maps server ids to names; ids it cannot resolve stay unmapped.
func serverNames(ctx context.Context, api *API, vols ...apiclient.Volume) map[int64]string {
	names := map[int64]string{}
	for _, v := range vols {
		if v.Server != nil {
			list, err := api.Servers(ctx, apiclient.GetServersParams{})
			if err == nil {
				for _, s := range list {
					names[s.Id] = s.Name
				}
			}
			break
		}
	}
	return names
}

func volumeServer(v apiclient.Volume, names map[int64]string) string {
	if v.Server == nil {
		return "-"
	}
	if n, ok := names[*v.Server]; ok {
		return n
	}
	return id64(*v.Server)
}

func volumeCreateCmd(a *app) *cobra.Command {
	var (
		name, location, server string
		size                   int
		labels                 []string
		protectDelete          bool
	)
	cmd := &cobra.Command{Use: "create", Short: "Create a volume", Args: cobra.NoArgs}
	wo := addWaitFlags(cmd)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		lbl, err := parseLabels(labels)
		if err != nil {
			return err
		}
		api, err := a.api()
		if err != nil {
			return err
		}
		body := apiclient.PostVolumesJSONRequestBody{Name: name, Size: size, Protection: protectionInput(cmd.Flags(), protectDelete)}
		if lbl != nil {
			body.Labels = &lbl
		}
		if location != "" {
			body.Location = &location
		}
		if server != "" {
			s, err := api.Server(cmd.Context(), server)
			if err != nil {
				return err
			}
			ref := id64(s.Id)
			body.Server = &ref
		}
		res, err := api.C.PostVolumesWithResponse(cmd.Context(), body)
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
			fmt.Fprintf(out, "Volume %s is being created (action %s)\n", id64(created.Volume.Id), id64(created.Action.Id))
			return nil
		}
		after := func(err error) error {
			return fmt.Errorf("volume %s was created, but: %w", id64(created.Volume.Id), err)
		}
		api.Timeout = wo.Timeout
		done, err := api.Wait(cmd.Context(), a.env.Stderr, a.stderrTTY(), created.Action.Id)
		if err != nil {
			return after(err)
		}
		vres, err := api.C.GetVolumeWithResponse(cmd.Context(), created.Volume.Id)
		if err != nil {
			return after(err)
		}
		vol, err := result(vres.StatusCode(), vres.Body, vres.JSON200)
		if err != nil {
			return after(err)
		}
		if a.flags.output != "table" {
			return a.printer().Object(map[string]any{"volume": vol, "action": done[0]})
		}
		fmt.Fprintf(out, "Volume %s created\n", id64(vol.Id))
		return nil
	}
	f := cmd.Flags()
	f.StringVar(&name, "name", "", "volume name")
	f.IntVar(&size, "size", 0, "size in GB")
	f.StringVar(&location, "location", "", "location id or name (optional with --server)")
	f.StringVar(&server, "server", "", "server id or name to attach to")
	f.StringArrayVar(&labels, "label", nil, "label as key=value (repeatable)")
	addProtectDeleteFlag(f, &protectDelete)
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("size")
	return cmd
}

func volumeListCmd(a *app) *cobra.Command {
	var selector, server string
	var volNames map[int64]string
	return listCmd(a, "list", "List volumes",
		[]string{"ID", "NAME", "SIZE", "SERVER", "LOCATION", "STATUS"},
		func(c *cobra.Command) {
			c.Flags().StringVarP(&selector, "selector", "l", "", "label selector, e.g. env=prod")
			c.Flags().StringVar(&server, "server", "", "only volumes attached to this server id or name")
		},
		func(c *cobra.Command, api *API) ([]apiclient.Volume, error) {
			var p apiclient.GetVolumesParams
			if selector != "" {
				p.LabelSelector = &selector
			}
			if server != "" {
				s, err := api.Server(c.Context(), server)
				if err != nil {
					return nil, err
				}
				ref := id64(s.Id)
				p.Server = &ref
			}
			vols, err := api.Volumes(c.Context(), p)
			if err != nil {
				return nil, err
			}
			if a.flags.output == "table" {
				volNames = serverNames(c.Context(), api, vols...)
			}
			return vols, nil
		},
		func(v apiclient.Volume) []string {
			return []string{id64(v.Id), v.Name, sizeString(v.Size), volumeServer(v, volNames), v.Location.Name, string(v.Status)}
		})
}

func volumeDescribeCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "describe <ref>",
		Short: "Show a volume by id or name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			api, err := a.api()
			if err != nil {
				return err
			}
			v, err := api.Volume(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			p := a.printer()
			if a.flags.output != "table" {
				return p.Object(v)
			}
			p.KV([][2]string{
				{"ID", id64(v.Id)},
				{"Name", v.Name},
				{"Status", string(v.Status)},
				{"Created", v.Created.Format(time.RFC3339)},
				{"Size", sizeString(v.Size)},
				{"Location", v.Location.Name},
				{"Server", volumeServer(v, serverNames(cmd.Context(), api, v))},
				{"Linux device", v.LinuxDevice},
				{"Labels", labelString(v.Labels)},
				{"Protection", protectionLine(v.Protection.Delete, nil)},
			})
			return nil
		},
	}
}

func volumeUpdateCmd(a *app) *cobra.Command {
	var name string
	var labels []string
	var protectDelete bool
	cmd := &cobra.Command{Use: "update <ref>", Short: "Rename a volume, replace its labels or change its protection", Args: cobra.ExactArgs(1)}
	cmd.Flags().StringVar(&name, "name", "", "new name")
	cmd.Flags().StringArrayVar(&labels, "label", nil, "label as key=value (repeatable); replaces all labels")
	addProtectDeleteFlag(cmd.Flags(), &protectDelete)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		prot := protectionInput(cmd.Flags(), protectDelete)
		if name == "" && len(labels) == 0 && prot == nil {
			return errors.New("nothing to update: pass --name, --label or --protect-delete")
		}
		lbl, err := parseLabels(labels)
		if err != nil {
			return err
		}
		api, err := a.api()
		if err != nil {
			return err
		}
		v, err := api.Volume(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		body := apiclient.PutVolumeJSONRequestBody{Protection: prot}
		if name != "" {
			body.Name = &name
		}
		if lbl != nil {
			body.Labels = &lbl
		}
		res, err := api.C.PutVolumeWithResponse(cmd.Context(), v.Id, body)
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
		fmt.Fprintf(a.env.Stdout, "Volume %s updated\n", upd.Name)
		return nil
	}
	return cmd
}

// runVolumeAction resolves the volume, starts the action and waits unless --no-wait.
func runVolumeAction(a *app, cmd *cobra.Command, wo *waitOpts, ref string, call actionCall, done string) error {
	api, err := a.api()
	if err != nil {
		return err
	}
	v, err := api.Volume(cmd.Context(), ref)
	if err != nil {
		return err
	}
	created, err := call(api, cmd, v.Id)
	if err != nil {
		return err
	}
	act := created.Action
	if !wo.NoWait {
		api.Timeout = wo.Timeout
		fin, err := api.Wait(cmd.Context(), a.env.Stderr, a.stderrTTY(), act.Id)
		if err != nil {
			return err
		}
		act = fin[0]
	}
	if a.flags.output != "table" {
		return a.printer().Object(act)
	}
	if wo.NoWait {
		fmt.Fprintf(a.env.Stdout, "Volume %s: action %s started\n", v.Name, id64(act.Id))
		return nil
	}
	fmt.Fprintf(a.env.Stdout, "Volume %s %s\n", v.Name, done)
	return nil
}

func volumeAttachCmd(a *app) *cobra.Command {
	var server string
	cmd := &cobra.Command{Use: "attach <ref>", Short: "Attach a volume to a server", Args: cobra.ExactArgs(1)}
	wo := addWaitFlags(cmd)
	cmd.Flags().StringVar(&server, "server", "", "server id or name")
	_ = cmd.MarkFlagRequired("server")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		call := func(api *API, c *cobra.Command, id int64) (*apiclient.ActionCreated, error) {
			s, err := api.Server(c.Context(), server)
			if err != nil {
				return nil, err
			}
			r, err := api.C.AttachVolumeWithResponse(c.Context(), id, apiclient.VolumeAttachInput{Server: id64(s.Id)})
			if err != nil {
				return nil, err
			}
			return result(r.StatusCode(), r.Body, r.JSON201)
		}
		return runVolumeAction(a, cmd, wo, args[0], call, "attached to "+server)
	}
	return cmd
}

func volumeDetachCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{Use: "detach <ref>", Short: "Detach a volume from its server", Args: cobra.ExactArgs(1)}
	wo := addWaitFlags(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		call := func(api *API, c *cobra.Command, id int64) (*apiclient.ActionCreated, error) {
			r, err := api.C.DetachVolumeWithResponse(c.Context(), id)
			if err != nil {
				return nil, err
			}
			return result(r.StatusCode(), r.Body, r.JSON201)
		}
		return runVolumeAction(a, cmd, wo, args[0], call, "detached")
	}
	return cmd
}

func volumeResizeCmd(a *app) *cobra.Command {
	var size int
	cmd := &cobra.Command{Use: "resize <ref>", Short: "Grow a volume", Args: cobra.ExactArgs(1)}
	wo := addWaitFlags(cmd)
	cmd.Flags().IntVar(&size, "size", 0, "new size in GB")
	_ = cmd.MarkFlagRequired("size")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		call := func(api *API, c *cobra.Command, id int64) (*apiclient.ActionCreated, error) {
			r, err := api.C.ResizeVolumeWithResponse(c.Context(), id, apiclient.VolumeResizeInput{Size: size})
			if err != nil {
				return nil, err
			}
			return result(r.StatusCode(), r.Body, r.JSON201)
		}
		return runVolumeAction(a, cmd, wo, args[0], call, "resized to "+sizeString(size))
	}
	return cmd
}

func volumeDeleteCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{Use: "delete <ref>", Short: "Delete a volume by id or name", Args: cobra.ExactArgs(1)}
	wo := addWaitFlags(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		api, err := a.api()
		if err != nil {
			return err
		}
		v, err := api.Volume(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		res, err := api.C.DeleteVolumeWithResponse(cmd.Context(), v.Id)
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
			fmt.Fprintf(a.env.Stdout, "Volume %s is being deleted (action %s)\n", v.Name, id64(act.Id))
			return nil
		}
		fmt.Fprintf(a.env.Stdout, "Volume %s deleted\n", v.Name)
		return nil
	}
	return cmd
}

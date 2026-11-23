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
	"strings"
	"time"

	"github.com/nebla-cloud/nebla/internal/cli/apiclient"
	"github.com/spf13/cobra"
)

func newSnapshotCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{Use: "snapshot", Short: "Manage snapshots"}
	cmd.AddCommand(snapshotCreateCmd(a), snapshotListCmd(a), snapshotDescribeCmd(a), snapshotUpdateCmd(a),
		snapshotRestoreCmd(a), snapshotDeleteCmd(a))
	return cmd
}

// stdinTTY reports whether stdin is an interactive terminal.
func (a *app) stdinTTY() bool {
	f, ok := a.env.Stdin.(*os.File)
	return ok && a.env.IsTerminal != nil && a.env.IsTerminal(int(f.Fd()))
}

func snapshotServer(s apiclient.Snapshot) string {
	if s.Server == nil {
		return s.ServerName + " (deleted)"
	}
	return s.ServerName
}

func snapshotCreateCmd(a *app) *cobra.Command {
	var (
		name, description string
		labels            []string
		forceStop, yes    bool
	)
	cmd := &cobra.Command{Use: "create <server>", Short: "Snapshot a server's boot disk", Args: cobra.ExactArgs(1)}
	wo := addWaitFlags(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		lbl, err := parseLabels(labels)
		if err != nil {
			return err
		}
		api, err := a.api()
		if err != nil {
			return err
		}
		srv, err := api.Server(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		if srv.Status == apiclient.ServerStatusRunning {
			msg := fmt.Sprintf("%s is running. It will be shut down, saved and started again.", srv.Name)
			if a.stdinTTY() && !yes {
				if err := a.confirm(cmd.Context(), msg+" Continue?"); err != nil {
					return err
				}
			} else {
				fmt.Fprintln(a.env.Stderr, msg)
			}
		}
		body := apiclient.PostSnapshotsJSONRequestBody{Server: id64(srv.Id)}
		if name != "" {
			body.Name = &name
		}
		if cmd.Flags().Changed("description") {
			body.Description = &description
		}
		if lbl != nil {
			body.Labels = &lbl
		}
		if forceStop {
			body.ForceStop = &forceStop
		}
		res, err := api.C.PostSnapshotsWithResponse(cmd.Context(), body)
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
			fmt.Fprintf(out, "Snapshot %s is being created (action %s)\n", created.Snapshot.Name, id64(created.Action.Id))
			return nil
		}
		after := func(err error) error {
			return fmt.Errorf("snapshot %s was started, but: %w", id64(created.Snapshot.Id), err)
		}
		api.Timeout = wo.Timeout
		done, err := api.Wait(cmd.Context(), a.env.Stderr, a.stderrTTY(), created.Action.Id)
		if err != nil {
			return after(err)
		}
		snap, err := api.Snapshot(cmd.Context(), id64(created.Snapshot.Id))
		if err != nil {
			return after(err)
		}
		if a.flags.output != "table" {
			return a.printer().Object(map[string]any{"snapshot": snap, "action": done[0]})
		}
		fmt.Fprintf(out, "Snapshot %s created\n", snap.Name)
		return nil
	}
	f := cmd.Flags()
	f.StringVar(&name, "name", "", "snapshot name (default <server>-<YYYYMMDD-HHMM> in UTC)")
	f.StringVar(&description, "description", "", "description")
	f.StringArrayVar(&labels, "label", nil, "label as key=value (repeatable)")
	f.BoolVar(&forceStop, "force-stop", false, "power the server off if it doesn't shut down in time")
	f.BoolVarP(&yes, "yes", "y", false, "do not ask before shutting down a running server")
	return cmd
}

func snapshotListCmd(a *app) *cobra.Command {
	var selector, server, location string
	return listCmd(a, "list", "List snapshots",
		[]string{"ID", "NAME", "SIZE", "SERVER", "LOCATION", "STATUS", "AGE"},
		func(c *cobra.Command) {
			c.Flags().StringVarP(&selector, "selector", "l", "", "label selector, e.g. env=prod")
			c.Flags().StringVar(&server, "server", "", "only snapshots of this server id or name")
			c.Flags().StringVar(&location, "location", "", "only snapshots in this location id or name")
		},
		func(c *cobra.Command, api *API) ([]apiclient.Snapshot, error) {
			var p apiclient.GetSnapshotsParams
			if selector != "" {
				p.LabelSelector = &selector
			}
			if server != "" {
				p.Server = &server
			}
			if location != "" {
				p.Location = &location
			}
			return api.Snapshots(c.Context(), p)
		},
		func(s apiclient.Snapshot) []string {
			return []string{id64(s.Id), s.Name, sizeString(s.Size), snapshotServer(s), s.Location.Name, string(s.Status), age(s.Created, time.Now())}
		})
}

func snapshotDescribeCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "describe <ref>",
		Short: "Show a snapshot by id or name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			api, err := a.api()
			if err != nil {
				return err
			}
			s, err := api.Snapshot(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			p := a.printer()
			if a.flags.output != "table" {
				return p.Object(s)
			}
			p.KV([][2]string{
				{"ID", id64(s.Id)},
				{"Name", s.Name},
				{"Description", s.Description},
				{"Status", string(s.Status)},
				{"Created", s.Created.Format(time.RFC3339)},
				{"Size", sizeString(s.Size)},
				{"Server", snapshotServer(s)},
				{"Location", s.Location.Name},
				{"OS", strings.TrimSpace(s.OsFlavor + " " + s.OsVersion)},
				{"Labels", labelString(s.Labels)},
			})
			return nil
		},
	}
}

func snapshotUpdateCmd(a *app) *cobra.Command {
	var name, description string
	var labels []string
	cmd := &cobra.Command{Use: "update <ref>", Short: "Rename a snapshot, change its description or replace its labels", Args: cobra.ExactArgs(1)}
	f := cmd.Flags()
	f.StringVar(&name, "name", "", "new name")
	f.StringVar(&description, "description", "", "new description")
	f.StringArrayVar(&labels, "label", nil, "label as key=value (repeatable); replaces all labels")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		setDesc := f.Changed("description")
		if name == "" && !setDesc && len(labels) == 0 {
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
		s, err := api.Snapshot(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		var body apiclient.PutSnapshotJSONRequestBody
		if name != "" {
			body.Name = &name
		}
		if setDesc {
			body.Description = &description
		}
		if lbl != nil {
			body.Labels = &lbl
		}
		res, err := api.C.PutSnapshotWithResponse(cmd.Context(), s.Id, body)
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
		fmt.Fprintf(a.env.Stdout, "Snapshot %s updated\n", upd.Name)
		return nil
	}
	return cmd
}

func snapshotRestoreCmd(a *app) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{Use: "restore <snapshot>", Short: "Rebuild the snapshot's server from it", Args: cobra.ExactArgs(1)}
	wo := addWaitFlags(cmd)
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask for confirmation")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		api, err := a.api()
		if err != nil {
			return err
		}
		snap, err := api.Snapshot(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		if snap.Server == nil {
			return fmt.Errorf("This snapshot's server no longer exists. Use `nebla server create --snapshot %s` instead.", args[0])
		}
		srv, err := api.Server(cmd.Context(), id64(*snap.Server))
		if err != nil {
			return err
		}
		if snap.RestoreKeepsIdentity != nil && !*snap.RestoreKeepsIdentity {
			fmt.Fprintf(a.env.Stderr, "%s's network changed since this snapshot. It will come back with new SSH host keys and run its user data again.\n", srv.Name)
		}
		if a.stdinTTY() && !yes {
			if err := a.confirm(cmd.Context(), fmt.Sprintf("Restore %s from %s? Its boot disk will be replaced.", srv.Name, snap.Name)); err != nil {
				return err
			}
		}
		call := func(api *API, c *cobra.Command, id int64) (*apiclient.ActionCreated, error) {
			ref := id64(snap.Id)
			r, err := api.C.RebuildServerWithResponse(c.Context(), id, apiclient.RebuildInput{Snapshot: &ref})
			if err != nil {
				return nil, err
			}
			return result(r.StatusCode(), r.Body, r.JSON201)
		}
		return runServerAction(a, cmd, wo, id64(srv.Id), call, "restored from "+snap.Name)
	}
	return cmd
}

func snapshotDeleteCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{Use: "delete <ref>", Short: "Delete a snapshot by id or name", Args: cobra.ExactArgs(1)}
	wo := addWaitFlags(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		api, err := a.api()
		if err != nil {
			return err
		}
		s, err := api.Snapshot(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		res, err := api.C.DeleteSnapshotWithResponse(cmd.Context(), s.Id)
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
			fmt.Fprintf(a.env.Stdout, "Snapshot %s is being deleted (action %s)\n", s.Name, id64(act.Id))
			return nil
		}
		fmt.Fprintf(a.env.Stdout, "Snapshot %s deleted\n", s.Name)
		return nil
	}
	return cmd
}

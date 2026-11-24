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
	"bufio"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/nebla-cloud/nebla/internal/cli/apiclient"
	"github.com/spf13/cobra"
)

const (
	defaultBackupKeep      = 7
	restoreIdentityWarning = "This server's network changed since this backup. Its network will be reconfigured and SSH host keys will change."
)

func newBackupCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{Use: "backup", Short: "Manage server backups"}
	cmd.AddCommand(backupEnableCmd(a), backupDisableCmd(a), backupNowCmd(a), backupListCmd(a), backupDeleteCmd(a), backupRestoreCmd(a))
	return cmd
}

func bytesString(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	v, suffix := float64(n), []string{"KB", "MB", "GB", "TB"}
	i := -1
	for v >= unit && i < len(suffix)-1 {
		v /= unit
		i++
	}
	return fmt.Sprintf("%.1f %s", v, suffix[i])
}

func parseBackupID(s string) (int64, error) {
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid backup id %q", s)
	}
	return id, nil
}

// confirm asks on stderr and reads one line from stdin; only y or yes goes ahead.
func (a *app) confirm(ctx context.Context, prompt string) error {
	fmt.Fprintf(a.env.Stderr, "%s [y/N] ", prompt)
	line, err := readLine(ctx, bufio.NewReader(a.env.Stdin))
	if err != nil {
		return err
	}
	if s := strings.ToLower(strings.TrimSpace(line)); s != "y" && s != "yes" {
		return errors.New("aborted")
	}
	return nil
}

func setBackups(a *app, cmd *cobra.Command, ref string, enabled bool, keep *int, done string) error {
	api, err := a.api()
	if err != nil {
		return err
	}
	s, err := api.Server(cmd.Context(), ref)
	if err != nil {
		return err
	}
	k := s.Backups.Keep
	if keep != nil {
		k = *keep
	}
	res, err := api.C.PutServerWithResponse(cmd.Context(), s.Id, apiclient.PutServerJSONRequestBody{
		Backups: &apiclient.BackupSettings{Enabled: enabled, Keep: k},
	})
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
	fmt.Fprintf(a.env.Stdout, "Backups %s for %s\n", done, s.Name)
	return nil
}

func backupEnableCmd(a *app) *cobra.Command {
	var keep int
	cmd := &cobra.Command{Use: "enable <server>", Short: "Turn on scheduled backups", Args: cobra.ExactArgs(1)}
	cmd.Flags().IntVar(&keep, "keep", 0, "backups to keep, 1-14 (default: the server's current setting)")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		var k *int
		if cmd.Flags().Changed("keep") {
			k = &keep
		}
		return setBackups(a, cmd, args[0], true, k, "enabled")
	}
	return cmd
}

func backupDisableCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "disable <server>",
		Short: "Turn off scheduled backups; existing backups are kept",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return setBackups(a, cmd, args[0], false, nil, "disabled")
		},
	}
}

func backupNowCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{Use: "now <server>", Short: "Back up a server now", Args: cobra.ExactArgs(1)}
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
		res, err := api.C.PostServerBackupsWithResponse(cmd.Context(), s.Id)
		if err != nil {
			return err
		}
		b := res.JSON202
		if b == nil {
			b = res.JSON200
		}
		b, err = result(res.StatusCode(), res.Body, b)
		if err != nil {
			return err
		}
		if !wo.NoWait {
			api.Timeout = wo.Timeout
			if b, err = api.waitBackup(cmd.Context(), a.env.Stderr, s.Id, b.Id, wo.Timeout); err != nil {
				return err
			}
		}
		if a.flags.output != "table" {
			return a.printer().Object(b)
		}
		if wo.NoWait {
			fmt.Fprintf(a.env.Stdout, "Backup %s of %s is %s\n", id64(b.Id), s.Name, b.Status)
			return nil
		}
		fmt.Fprintf(a.env.Stdout, "Backup %s of %s is available\n", id64(b.Id), s.Name)
		return nil
	}
	return cmd
}

// waitBackup polls the backup until it is available; any other final state is an error.
func (a *API) waitBackup(ctx context.Context, w interface{ Write([]byte) (int, error) }, server, id int64, timeout time.Duration) (*apiclient.Backup, error) {
	parent := ctx
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	fmt.Fprintf(w, "Waiting for backup %d\n", id)
	delay := pollBase
	for {
		res, err := a.C.GetServerBackupWithResponse(ctx, server, id)
		if err == nil {
			var b *apiclient.Backup
			if b, err = result(res.StatusCode(), res.Body, res.JSON200); err == nil {
				switch b.Status {
				case apiclient.BackupStatusAvailable:
					fmt.Fprintf(w, "backup %d: available\n", id)
					return b, nil
				case apiclient.BackupStatusError, apiclient.BackupStatusMissing, apiclient.BackupStatusDeleting:
					return b, fmt.Errorf("backup %d failed: it is %s", id, b.Status)
				}
			}
		}
		if err != nil && ctx.Err() == nil {
			return nil, err
		}
		t := time.NewTimer(delay)
		select {
		case <-t.C:
		case <-ctx.Done():
			t.Stop()
			if parent.Err() == nil {
				return nil, fmt.Errorf("timed out after %s waiting for backup %d; it keeps running", timeout, id)
			}
			return nil, interruptedBackup{id}
		}
		delay = min(delay*3/2, pollMax)
	}
}

type interruptedBackup struct{ id int64 }

func (e interruptedBackup) Error() string {
	return fmt.Sprintf("stopped waiting; backup %d keeps running", e.id)
}

func (e interruptedBackup) Is(target error) bool { return target == ErrInterrupted }

func backupListCmd(a *app) *cobra.Command {
	var noHeader bool
	var status, kind string
	cmd := &cobra.Command{Use: "list <server>", Short: "List a server's backups", Args: cobra.ExactArgs(1)}
	cmd.Flags().BoolVar(&noHeader, "no-header", false, "omit the table header")
	cmd.Flags().StringVar(&status, "status", "", "only backups in this status")
	cmd.Flags().StringVar(&kind, "kind", "", "only manual or scheduled backups")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		api, err := a.api()
		if err != nil {
			return err
		}
		s, err := api.Server(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		var p apiclient.GetServerBackupsParams
		if status != "" {
			st := apiclient.BackupStatus(status)
			p.Status = &st
		}
		if kind != "" {
			k := apiclient.BackupKind(kind)
			p.Kind = &k
		}
		res, err := api.C.GetServerBackupsWithResponse(cmd.Context(), s.Id, &p)
		if err != nil {
			return err
		}
		out, err := result(res.StatusCode(), res.Body, res.JSON200)
		if err != nil {
			return err
		}
		pr := a.printer()
		if a.flags.output != "table" {
			if out.Backups == nil {
				out.Backups = []apiclient.Backup{}
			}
			return pr.Object(out.Backups)
		}
		pr.NoHeader = noHeader
		rows := make([][]string, 0, len(out.Backups))
		for _, b := range out.Backups {
			rows = append(rows, []string{id64(b.Id), string(b.Kind), string(b.Status), bytesString(b.Size), age(b.Created, time.Now())})
		}
		pr.Table([]string{"ID", "KIND", "STATUS", "SIZE", "CREATED"}, rows)
		return nil
	}
	return cmd
}

func backupDeleteCmd(a *app) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{Use: "delete <server> <backup>", Short: "Delete a backup", Args: cobra.ExactArgs(2)}
	wo := addWaitFlags(cmd)
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask for confirmation")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		bid, err := parseBackupID(args[1])
		if err != nil {
			return err
		}
		api, err := a.api()
		if err != nil {
			return err
		}
		s, err := api.Server(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		if !yes {
			if err := a.confirm(cmd.Context(), fmt.Sprintf("Delete backup %d of server %s?", bid, s.Name)); err != nil {
				return err
			}
		}
		res, err := api.C.DeleteServerBackupWithResponse(cmd.Context(), s.Id, bid)
		if err != nil {
			return err
		}
		out, err := result(res.StatusCode(), res.Body, res.JSON200)
		if err != nil {
			return err
		}
		return finishBackupAction(a, cmd, api, wo, out.Action, fmt.Sprintf("Backup %d of %s", bid, s.Name), "is being deleted", "deleted")
	}
	return cmd
}

func backupRestoreCmd(a *app) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{Use: "restore <server> <backup>", Short: "Restore a server in place from a backup", Args: cobra.ExactArgs(2)}
	wo := addWaitFlags(cmd)
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask for confirmation")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		bid, err := parseBackupID(args[1])
		if err != nil {
			return err
		}
		api, err := a.api()
		if err != nil {
			return err
		}
		s, err := api.Server(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		if !yes {
			bres, err := api.C.GetServerBackupWithResponse(cmd.Context(), s.Id, bid)
			if err != nil {
				return err
			}
			b, err := result(bres.StatusCode(), bres.Body, bres.JSON200)
			if err != nil {
				return err
			}
			if b.RestoreResetsIdentity {
				fmt.Fprintln(a.env.Stderr, "Warning: "+restoreIdentityWarning)
			}
			if err := a.confirm(cmd.Context(), fmt.Sprintf("Restore server %s from backup %d? Its disk is replaced with the backup.", s.Name, bid)); err != nil {
				return err
			}
		}
		res, err := api.C.RestoreServerBackupWithResponse(cmd.Context(), s.Id, apiclient.RestoreBackupInput{Backup: bid})
		if err != nil {
			return err
		}
		created, err := result(res.StatusCode(), res.Body, res.JSON201)
		if err != nil {
			return err
		}
		return finishBackupAction(a, cmd, api, wo, created.Action, fmt.Sprintf("Server %s", s.Name), "is being restored", fmt.Sprintf("restored from backup %d", bid))
	}
	return cmd
}

// finishBackupAction waits for the action unless --no-wait and prints the outcome.
func finishBackupAction(a *app, cmd *cobra.Command, api *API, wo *waitOpts, act apiclient.Action, subject, started, done string) error {
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
		fmt.Fprintf(a.env.Stdout, "%s %s (action %s)\n", subject, started, id64(act.Id))
		return nil
	}
	fmt.Fprintf(a.env.Stdout, "%s %s\n", subject, done)
	return nil
}

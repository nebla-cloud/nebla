package cli

import (
	"fmt"

	"github.com/nebla-cloud/nebla/internal/cli/apiclient"
	"github.com/spf13/cobra"
)

type actionCall func(api *API, cmd *cobra.Command, id int64) (*apiclient.ActionCreated, error)

// runServerAction resolves the server, starts the action and waits unless --no-wait.
func runServerAction(a *app, cmd *cobra.Command, wo *waitOpts, ref string, call actionCall, done string) error {
	api, err := a.api()
	if err != nil {
		return err
	}
	s, err := api.Server(cmd.Context(), ref)
	if err != nil {
		return err
	}
	v, err := call(api, cmd, s.Id)
	if err != nil {
		return err
	}
	act := v.Action
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
		fmt.Fprintf(a.env.Stdout, "Server %s: action %s started\n", s.Name, id64(act.Id))
		return nil
	}
	fmt.Fprintf(a.env.Stdout, "Server %s %s\n", s.Name, done)
	return nil
}

func serverActionCmds(a *app) []*cobra.Command {
	table := []struct {
		use, short, past string
		call             actionCall
	}{
		{"poweron", "Power on a server", "powered on", func(api *API, c *cobra.Command, id int64) (*apiclient.ActionCreated, error) {
			r, err := api.C.PoweronServerWithResponse(c.Context(), id)
			if err != nil {
				return nil, err
			}
			return result(r.StatusCode(), r.Body, r.JSON201)
		}},
		{"poweroff", "Power off a server", "powered off", func(api *API, c *cobra.Command, id int64) (*apiclient.ActionCreated, error) {
			r, err := api.C.PoweroffServerWithResponse(c.Context(), id)
			if err != nil {
				return nil, err
			}
			return result(r.StatusCode(), r.Body, r.JSON201)
		}},
		{"reboot", "Reboot a server", "rebooted", func(api *API, c *cobra.Command, id int64) (*apiclient.ActionCreated, error) {
			r, err := api.C.RebootServerWithResponse(c.Context(), id)
			if err != nil {
				return nil, err
			}
			return result(r.StatusCode(), r.Body, r.JSON201)
		}},
		{"reset", "Hard reset a server", "reset", func(api *API, c *cobra.Command, id int64) (*apiclient.ActionCreated, error) {
			r, err := api.C.ResetServerWithResponse(c.Context(), id)
			if err != nil {
				return nil, err
			}
			return result(r.StatusCode(), r.Body, r.JSON201)
		}},
		{"shutdown", "Gracefully shut down a server", "shut down", func(api *API, c *cobra.Command, id int64) (*apiclient.ActionCreated, error) {
			r, err := api.C.ShutdownServerWithResponse(c.Context(), id)
			if err != nil {
				return nil, err
			}
			return result(r.StatusCode(), r.Body, r.JSON201)
		}},
	}
	var cmds []*cobra.Command
	for _, e := range table {
		e := e
		cmd := &cobra.Command{Use: e.use + " <ref>", Short: e.short, Args: cobra.ExactArgs(1)}
		wo := addWaitFlags(cmd)
		cmd.RunE = func(cmd *cobra.Command, args []string) error {
			return runServerAction(a, cmd, wo, args[0], e.call, e.past)
		}
		cmds = append(cmds, cmd)
	}
	return append(cmds, serverRebuildCmd(a), serverChangeTypeCmd(a))
}

func serverRebuildCmd(a *app) *cobra.Command {
	var image string
	cmd := &cobra.Command{Use: "rebuild <ref>", Short: "Rebuild a server from an image", Args: cobra.ExactArgs(1)}
	wo := addWaitFlags(cmd)
	cmd.Flags().StringVar(&image, "image", "", "image id or name")
	_ = cmd.MarkFlagRequired("image")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		call := func(api *API, c *cobra.Command, id int64) (*apiclient.ActionCreated, error) {
			r, err := api.C.RebuildServerWithResponse(c.Context(), id, apiclient.RebuildInput{Image: image})
			if err != nil {
				return nil, err
			}
			return result(r.StatusCode(), r.Body, r.JSON201)
		}
		return runServerAction(a, cmd, wo, args[0], call, "rebuilt from "+image)
	}
	return cmd
}

func serverChangeTypeCmd(a *app) *cobra.Command {
	var typ string
	var upgrade bool
	cmd := &cobra.Command{Use: "change-type <ref>", Short: "Change the type of a stopped server", Args: cobra.ExactArgs(1)}
	wo := addWaitFlags(cmd)
	cmd.Flags().StringVar(&typ, "type", "", "server type id or name")
	cmd.Flags().BoolVar(&upgrade, "upgrade-disk", false, "grow the disk to the new type's size")
	_ = cmd.MarkFlagRequired("type")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		call := func(api *API, c *cobra.Command, id int64) (*apiclient.ActionCreated, error) {
			r, err := api.C.ChangeServerTypeWithResponse(c.Context(), id, apiclient.ChangeTypeInput{ServerType: typ, UpgradeDisk: upgrade})
			if err != nil {
				return nil, err
			}
			return result(r.StatusCode(), r.Body, r.JSON201)
		}
		return runServerAction(a, cmd, wo, args[0], call, "changed to "+typ)
	}
	return cmd
}

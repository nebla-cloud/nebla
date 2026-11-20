// Package cli implements the nebla command line client.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// Version, Commit and Date are set by ldflags.
var (
	Version = "dev"
	Commit  = ""
	Date    = ""
)

// Env carries the process dependencies so tests can replace them.
type Env struct {
	Stdin      io.Reader
	Stdout     io.Writer
	Stderr     io.Writer
	Getenv     func(string) string
	ConfigDir  string
	HTTPClient *http.Client
	Exec       func(argv0 string, argv []string, env []string) error
	IsTerminal func(fd int) bool
	LookPath   func(file string) (string, error)
}

type globalFlags struct {
	context string
	config  string
	output  string
}

// app is what every command closes over.
type app struct {
	env    Env
	flags  *globalFlags
	target Target
}

// api loads the config, resolves the target and builds the API for it.
func (a *app) api() (*API, error) {
	p, err := configPath(a.env, a.flags.config)
	if err != nil {
		return nil, err
	}
	cfg, err := LoadConfig(p)
	if err != nil {
		return nil, err
	}
	t, err := resolveTarget(a.env, cfg, a.flags.context)
	if err != nil {
		return nil, err
	}
	a.target = t
	api, err := newAPI(a.env, t)
	if err != nil {
		return nil, err
	}
	return api, nil
}

// printer writes to stdout in the -o format.
func (a *app) printer() Printer {
	return Printer{Format: a.flags.output, Out: a.env.Stdout}
}

// stderrTTY reports whether progress can redraw a line on stderr.
func (a *app) stderrTTY() bool {
	f, ok := a.env.Stderr.(*os.File)
	return ok && a.env.IsTerminal != nil && a.env.IsTerminal(int(f.Fd()))
}

// DefaultEnv returns the Env backed by the real process.
func DefaultEnv() Env {
	return Env{
		Stdin:      os.Stdin,
		Stdout:     os.Stdout,
		Stderr:     os.Stderr,
		Getenv:     os.Getenv,
		IsTerminal: term.IsTerminal,
		Exec:       execProcess,
	}
}

// Run executes the CLI and returns the exit code.
func Run(ctx context.Context, args []string, env Env) int {
	root, a := newRoot(env)
	root.SetArgs(args)
	return exitCode(env.Stderr, a.annotate(root.ExecuteContext(ctx)))
}

// annotate names the context behind a 401 so the user knows which one to recreate.
func (a *app) annotate(err error) error {
	var ae *APIError
	if errors.As(err, &ae) && ae.Status == http.StatusUnauthorized && a.target.Source != "" {
		return fmt.Errorf("%w (%s, %s)", err, a.target.Source, a.target.Endpoint)
	}
	return err
}

// exitStatus is a child process exit code to pass on without printing.
type exitStatus int

func (e exitStatus) Error() string { return fmt.Sprintf("exit status %d", int(e)) }

func exitCode(stderr io.Writer, err error) int {
	switch {
	case err == nil:
		return 0
	case errors.As(err, new(exitStatus)):
		var st exitStatus
		errors.As(err, &st)
		return int(st)
	case errors.Is(err, ErrInterrupted):
		fmt.Fprintln(stderr, err)
		return 130
	}
	fmt.Fprintf(stderr, "error: %v\n", err)
	return 1
}

func newRootCmd(env Env) *cobra.Command {
	root, _ := newRoot(env)
	return root
}

func newRoot(env Env) (*cobra.Command, *app) {
	g := &globalFlags{}
	a := &app{env: env, flags: g}
	root := &cobra.Command{
		Use:           "nebla",
		Short:         "Command line client for the Nebla panel",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(*cobra.Command, []string) error {
			switch g.output {
			case "table", "json", "yaml":
				return nil
			}
			return fmt.Errorf("invalid output format %q", g.output)
		},
	}
	root.SetOut(env.Stdout)
	root.SetErr(env.Stderr)
	root.SetIn(env.Stdin)
	f := root.PersistentFlags()
	f.StringVar(&g.context, "context", "", "context to use")
	f.StringVar(&g.config, "config", "", "path to the config file")
	f.StringVarP(&g.output, "output", "o", "table", "output format: table, json or yaml")

	root.AddCommand(newContextCmd(a))
	root.AddCommand(newSSHKeyCmd(a), newImageCmd(a), newServerTypeCmd(a), newLocationCmd(a), newServerCmd(a))
	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the nebla version",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintln(cmd.OutOrStdout(), versionString())
		},
	})
	return root, a
}

func versionString() string {
	switch {
	case Commit == "":
		return "nebla " + Version
	case Date == "":
		return fmt.Sprintf("nebla %s (%s)", Version, Commit)
	}
	return fmt.Sprintf("nebla %s (%s, %s)", Version, Commit, Date)
}

// Package cli implements the nebla command line client.
package cli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// Version is set by ldflags.
var Version = "dev"

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
}

type globalFlags struct {
	context string
	config  string
	output  string
}

// DefaultEnv returns the Env backed by the real process.
func DefaultEnv() Env {
	return Env{
		Stdin:      os.Stdin,
		Stdout:     os.Stdout,
		Stderr:     os.Stderr,
		Getenv:     os.Getenv,
		IsTerminal: term.IsTerminal,
	}
}

// Run executes the CLI and returns the exit code.
func Run(ctx context.Context, args []string, env Env) int {
	root := newRootCmd(env)
	root.SetArgs(args)
	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintf(env.Stderr, "error: %v\n", err)
		return 1
	}
	return 0
}

func newRootCmd(env Env) *cobra.Command {
	g := &globalFlags{}
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

	root.AddCommand(newContextCmd(env, g))
	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the nebla version",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintf(cmd.OutOrStdout(), "nebla %s\n", Version)
		},
	})
	return root
}

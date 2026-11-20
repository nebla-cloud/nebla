package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Hexeption/nebla/internal/cli/apiclient"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newContextCmd(a *app) *cobra.Command {
	env := a.env
	load := func() (*Config, error) {
		p, err := configPath(env, a.flags.config)
		if err != nil {
			return nil, err
		}
		return LoadConfig(p)
	}
	cmd := &cobra.Command{Use: "context", Short: "Manage panel contexts"}

	var endpoint string
	var activate bool
	create := &cobra.Command{
		Use:   "create <name>",
		Short: "Add a context after checking its token",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if strings.TrimSpace(name) == "" {
				return errors.New("context name must not be empty")
			}
			cfg, err := load()
			if err != nil {
				return err
			}
			if cfg.find(name) != nil {
				return fmt.Errorf("context %q already exists", name)
			}
			in := bufio.NewReader(env.Stdin)
			if endpoint == "" {
				fmt.Fprint(env.Stderr, "Endpoint: ")
				line, err := readLine(cmd.Context(), in)
				if err != nil {
					fmt.Fprintln(env.Stderr)
					return err
				}
				endpoint = strings.TrimSpace(line)
			}
			ep, err := normalizeEndpoint(endpoint)
			if err != nil {
				return err
			}
			token, err := readToken(cmd.Context(), env, in)
			if err != nil {
				return err
			}
			if err := checkToken(cmd.Context(), env, Target{Endpoint: ep, Token: token}); err != nil {
				return err
			}
			cfg.Contexts = append(cfg.Contexts, Context{Name: name, Endpoint: ep, Token: token})
			if activate || len(cfg.Contexts) == 1 {
				cfg.ActiveContext = name
			}
			if err := cfg.Save(); err != nil {
				return err
			}
			if a.flags.output != "table" {
				return a.printer().Object(contextView{Name: name, Endpoint: ep, Active: cfg.ActiveContext == name})
			}
			fmt.Fprintf(env.Stdout, "Context %s created\n", name)
			return nil
		},
	}
	create.Flags().StringVar(&endpoint, "endpoint", "", "panel base URL")
	create.Flags().BoolVar(&activate, "activate", false, "make the new context active")

	var noHeader bool
	list := &cobra.Command{
		Use:   "list",
		Short: "List contexts",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := load()
			if err != nil {
				return err
			}
			views := make([]contextView, 0, len(cfg.Contexts))
			rows := make([][]string, 0, len(cfg.Contexts))
			for _, c := range cfg.Contexts {
				active := c.Name == cfg.ActiveContext
				views = append(views, contextView{Name: c.Name, Endpoint: c.Endpoint, Active: active})
				mark := ""
				if active {
					mark = "*"
				}
				rows = append(rows, []string{c.Name, c.Endpoint, mark})
			}
			p := a.printer()
			if a.flags.output != "table" {
				return p.Object(views)
			}
			p.NoHeader = noHeader
			p.Table([]string{"NAME", "ENDPOINT", "ACTIVE"}, rows)
			return nil
		},
	}
	list.Flags().BoolVar(&noHeader, "no-header", false, "omit the table header")

	use := &cobra.Command{
		Use:   "use <name>",
		Short: "Switch the active context",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := load()
			if err != nil {
				return err
			}
			if cfg.find(args[0]) == nil {
				return fmt.Errorf("context %q not found", args[0])
			}
			cfg.ActiveContext = args[0]
			return cfg.Save()
		},
	}

	del := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a context",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := load()
			if err != nil {
				return err
			}
			if cfg.find(args[0]) == nil {
				return fmt.Errorf("context %q not found", args[0])
			}
			kept := cfg.Contexts[:0]
			for _, c := range cfg.Contexts {
				if c.Name != args[0] {
					kept = append(kept, c)
				}
			}
			cfg.Contexts = kept
			if cfg.ActiveContext == args[0] {
				cfg.ActiveContext = ""
			}
			return cfg.Save()
		},
	}
	cmd.AddCommand(create, list, use, del)
	return cmd
}

// contextView is a context without its token.
type contextView struct {
	Name     string `json:"name"`
	Endpoint string `json:"endpoint"`
	Active   bool   `json:"active"`
}

// readLine reads one line from in, giving up with ErrInterrupted when ctx ends first.
func readLine(ctx context.Context, in *bufio.Reader) (string, error) {
	ch := make(chan string, 1)
	go func() {
		s, _ := in.ReadString('\n')
		ch <- s
	}()
	select {
	case s := <-ch:
		return s, nil
	case <-ctx.Done():
		return "", ErrInterrupted
	}
}

// readToken reads the token from a hidden prompt, NEBLA_TOKEN, or one line of stdin.
func readToken(ctx context.Context, env Env, in *bufio.Reader) (string, error) {
	if f, ok := env.Stdin.(*os.File); ok && env.IsTerminal != nil && env.IsTerminal(int(f.Fd())) {
		fd := int(f.Fd())
		fmt.Fprint(env.Stderr, "Token: ")
		state, err := term.GetState(fd)
		if err != nil {
			return "", err
		}
		type pw struct {
			b   []byte
			err error
		}
		ch := make(chan pw, 1)
		go func() {
			b, err := term.ReadPassword(fd)
			ch <- pw{b, err}
		}()
		select {
		case r := <-ch:
			fmt.Fprintln(env.Stderr)
			if r.err != nil {
				return "", r.err
			}
			return requireToken(string(r.b))
		case <-ctx.Done():
			_ = term.Restore(fd, state)
			fmt.Fprintln(env.Stderr)
			return "", ErrInterrupted
		}
	}
	if t := env.Getenv("NEBLA_TOKEN"); t != "" {
		return requireToken(t)
	}
	line, err := readLine(ctx, in)
	if err != nil {
		return "", err
	}
	return requireToken(line)
}

func requireToken(s string) (string, error) {
	if s = strings.TrimSpace(s); s == "" {
		return "", errors.New("no token given")
	}
	return s, nil
}

var tokenCheckTimeout = 30 * time.Second

// checkToken verifies the pair against the panel.
func checkToken(ctx context.Context, env Env, t Target) error {
	ctx, cancel := context.WithTimeout(ctx, tokenCheckTimeout)
	defer cancel()
	api, err := newAPI(env, t)
	if err != nil {
		return err
	}
	one := 1
	res, err := api.C.GetLocationsWithResponse(ctx, &apiclient.GetLocationsParams{PerPage: &one})
	if err != nil {
		return err
	}
	if _, err := result(res.StatusCode(), res.Body, res.JSON200); err != nil {
		return fmt.Errorf("token check failed: %w", err)
	}
	return nil
}

package cli

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v4"
)

// Context is one panel endpoint with its API token.
type Context struct {
	Name     string `yaml:"name"`
	Endpoint string `yaml:"endpoint"`
	Token    string `yaml:"token"`
}

// Config is the per-user CLI config file.
type Config struct {
	ActiveContext string    `yaml:"active_context,omitempty"`
	Contexts      []Context `yaml:"contexts,omitempty"`
	path          string
}

// Target is the endpoint and token an API command talks to.
type Target struct {
	Endpoint, Token, Source string
}

// LoadConfig reads path; a missing file gives an empty config.
func LoadConfig(path string) (*Config, error) {
	cfg := &Config{path: path}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(b, cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return cfg, nil
}

// Save writes the config with mode 0600 through a temp file and rename.
func (c *Config) Save() error {
	dir := filepath.Dir(c.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".cli-*.yaml")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), c.path)
}

func (c *Config) find(name string) *Context {
	for i := range c.Contexts {
		if c.Contexts[i].Name == name {
			return &c.Contexts[i]
		}
	}
	return nil
}

func configPath(env Env, flag string) (string, error) {
	if flag != "" {
		return flag, nil
	}
	if p := env.Getenv("NEBLA_CONFIG"); p != "" {
		return p, nil
	}
	dir := env.ConfigDir
	if dir == "" {
		var err error
		if dir, err = os.UserConfigDir(); err != nil {
			return "", err
		}
	}
	return filepath.Join(dir, "nebla", "cli.yaml"), nil
}

func resolveTarget(env Env, cfg *Config, contextFlag string) (Target, error) {
	name, source := contextFlag, ""
	if name == "" {
		ep, tok := env.Getenv("NEBLA_ENDPOINT"), env.Getenv("NEBLA_TOKEN")
		if ep != "" && tok != "" {
			ep, err := normalizeEndpoint(ep)
			if err != nil {
				return Target{}, err
			}
			return Target{Endpoint: ep, Token: tok, Source: "env"}, nil
		}
		if ep != "" {
			return Target{}, errors.New("NEBLA_ENDPOINT and NEBLA_TOKEN must be set together")
		}
		if name = env.Getenv("NEBLA_CONTEXT"); name == "" {
			name = cfg.ActiveContext
		}
	}
	if name == "" {
		return Target{}, errors.New(`no active context; run "nebla context create <name>"`)
	}
	c := cfg.find(name)
	if c == nil {
		return Target{}, fmt.Errorf("context %q not found", name)
	}
	source = "context " + name
	return Target{Endpoint: c.Endpoint, Token: c.Token, Source: source}, nil
}

func normalizeEndpoint(s string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("invalid endpoint %q: need an http or https URL", s)
	}
	return strings.TrimRight(u.String(), "/"), nil
}

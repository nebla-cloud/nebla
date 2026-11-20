package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestConfigRoundTripPerms(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "cli.yaml")
	cfg := &Config{ActiveContext: "a", Contexts: []Context{{Name: "a", Endpoint: "https://a.example.com", Token: "tok"}}, path: path}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	got, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, cfg) {
		t.Fatalf("got %+v want %+v", got, cfg)
	}
	fi, _ := os.Stat(path)
	di, _ := os.Stat(filepath.Dir(path))
	if fi.Mode().Perm() != 0o600 || di.Mode().Perm() != 0o700 {
		t.Fatalf("file %v dir %v", fi.Mode().Perm(), di.Mode().Perm())
	}
}

func TestLoadMissingIsEmpty(t *testing.T) {
	cfg, err := LoadConfig(filepath.Join(t.TempDir(), "none.yaml"))
	if err != nil || cfg.ActiveContext != "" || len(cfg.Contexts) != 0 {
		t.Fatalf("cfg=%+v err=%v", cfg, err)
	}
}

func TestResolveTargetPrecedence(t *testing.T) {
	cfg := &Config{ActiveContext: "a", Contexts: []Context{
		{Name: "a", Endpoint: "https://a", Token: "ta"},
		{Name: "b", Endpoint: "https://b", Token: "tb"},
	}}
	pair := map[string]string{"NEBLA_ENDPOINT": "https://env/", "NEBLA_TOKEN": "te"}
	tests := []struct {
		name    string
		env     map[string]string
		flag    string
		want    Target
		wantErr string
	}{
		{"flag beats env pair", pair, "b", Target{"https://b", "tb", "context b"}, ""},
		{"env pair", pair, "", Target{"https://env", "te", "env"}, ""},
		{"lone token uses active", map[string]string{"NEBLA_TOKEN": "te"}, "", Target{"https://a", "ta", "context a"}, ""},
		{"lone endpoint errors", map[string]string{"NEBLA_ENDPOINT": "https://env"}, "", Target{}, "NEBLA_ENDPOINT and NEBLA_TOKEN must be set together"},
		{"NEBLA_CONTEXT", map[string]string{"NEBLA_CONTEXT": "b"}, "", Target{"https://b", "tb", "context b"}, ""},
		{"unknown context", nil, "x", Target{}, `context "x" not found`},
		{"env pair token trimmed", map[string]string{"NEBLA_ENDPOINT": "https://env", "NEBLA_TOKEN": " te\n"}, "", Target{"https://env", "te", "env"}, ""},
		{"env pair blank token", map[string]string{"NEBLA_ENDPOINT": "https://env", "NEBLA_TOKEN": "  "}, "", Target{}, "no token given"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env := Env{Getenv: func(k string) string { return tc.env[k] }}
			got, err := resolveTarget(env, cfg, tc.flag)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err=%v", err)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("got %+v err=%v want %+v", got, err, tc.want)
			}
		})
	}
	_, err := resolveTarget(Env{Getenv: func(string) string { return "" }}, &Config{}, "")
	if err == nil || !strings.Contains(err.Error(), `nebla context create`) {
		t.Fatalf("err=%v", err)
	}
}

func TestNormalizeEndpoint(t *testing.T) {
	for in, want := range map[string]string{
		"https://p.example.com/":       "https://p.example.com",
		"https://p.example.com/panel/": "https://p.example.com/panel",
	} {
		got, err := normalizeEndpoint(in)
		if err != nil || got != want {
			t.Fatalf("%q: got %q err=%v", in, got, err)
		}
	}
	for _, bad := range []string{"p.example.com", "ftp://x", "https://"} {
		if _, err := normalizeEndpoint(bad); err == nil {
			t.Fatalf("%q accepted", bad)
		}
	}
}

func TestConfigPath(t *testing.T) {
	env := Env{ConfigDir: "/cfg", Getenv: func(k string) string {
		if k == "NEBLA_CONFIG" {
			return "/env.yaml"
		}
		return ""
	}}
	if p, _ := configPath(env, "/flag.yaml"); p != "/flag.yaml" {
		t.Fatal(p)
	}
	if p, _ := configPath(env, ""); p != "/env.yaml" {
		t.Fatal(p)
	}
	env.Getenv = func(string) string { return "" }
	if p, _ := configPath(env, ""); p != filepath.Join("/cfg", "nebla", "cli.yaml") {
		t.Fatal(p)
	}
}

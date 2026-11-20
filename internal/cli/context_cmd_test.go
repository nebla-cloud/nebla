package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestContextCreateChecksToken(t *testing.T) {
	h := newHarness(t)
	_, stderr, code := h.runEnv(map[string]string{"NEBLA_TOKEN": h.Token}, "", "context", "create", "home", "--endpoint", h.URL+"/")
	if code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	cfg, _ := LoadConfig(h.configFile())
	var home *Context
	for i := range cfg.Contexts {
		if cfg.Contexts[i].Name == "home" {
			home = &cfg.Contexts[i]
		}
	}
	if home == nil || home.Endpoint != h.URL || home.Token != h.Token {
		t.Fatalf("home=%+v", home)
	}
	if cfg.ActiveContext != "test" {
		t.Fatalf("active=%q, only a first context becomes active", cfg.ActiveContext)
	}
}

func TestContextCreateFirstIsActive(t *testing.T) {
	h := newHarness(t)
	if err := (&Config{path: h.configFile()}).Save(); err != nil {
		t.Fatal(err)
	}
	_, stderr, code := h.runEnv(nil, h.Token+"\n", "context", "create", "home", "--endpoint", h.URL)
	if code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	cfg, _ := LoadConfig(h.configFile())
	if cfg.ActiveContext != "home" {
		t.Fatalf("active=%q", cfg.ActiveContext)
	}
}

func TestContextCreateBadToken(t *testing.T) {
	h := newHarness(t)
	_, _, code := h.runEnv(map[string]string{"NEBLA_TOKEN": "nope"}, "", "context", "create", "home", "--endpoint", h.URL)
	if code != 1 {
		t.Fatalf("code=%d", code)
	}
	cfg, _ := LoadConfig(h.configFile())
	if len(cfg.Contexts) != 1 {
		t.Fatalf("contexts=%+v", cfg.Contexts)
	}
}

func TestContextCreateDuplicate(t *testing.T) {
	h := newHarness(t)
	env := map[string]string{"NEBLA_TOKEN": h.Token}
	if _, _, code := h.runEnv(env, "", "context", "create", "home", "--endpoint", h.URL); code != 0 {
		t.Fatal(code)
	}
	_, stderr, code := h.runEnv(env, "", "context", "create", "home", "--endpoint", h.URL)
	if code != 1 || !strings.Contains(stderr, `context "home" already exists`) {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
}

func TestContextListUseDelete(t *testing.T) {
	h := newHarness(t)
	env := map[string]string{"NEBLA_TOKEN": h.Token}
	if _, _, code := h.runEnv(env, "", "context", "create", "home", "--endpoint", h.URL); code != 0 {
		t.Fatal(code)
	}
	out, _, code := h.run("context", "list")
	if code != 0 || strings.Contains(out, h.Token) {
		t.Fatalf("code=%d out=%q", code, out)
	}
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "test") && !strings.Contains(l, "*") {
			t.Fatalf("test not marked: %q", out)
		}
		if strings.HasPrefix(l, "home") && strings.Contains(l, "*") {
			t.Fatalf("home marked: %q", out)
		}
	}
	if _, _, code := h.run("context", "use", "home"); code != 0 {
		t.Fatal(code)
	}
	if cfg, _ := LoadConfig(h.configFile()); cfg.ActiveContext != "home" {
		t.Fatalf("active=%q", cfg.ActiveContext)
	}
	if _, _, code := h.run("context", "delete", "home"); code != 0 {
		t.Fatal(code)
	}
	cfg, _ := LoadConfig(h.configFile())
	if cfg.ActiveContext != "" || len(cfg.Contexts) != 1 {
		t.Fatalf("cfg=%+v", cfg)
	}
	if _, stderr, code := h.run("context", "use", "gone"); code != 1 || !strings.Contains(stderr, `context "gone" not found`) {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
}

func runInDir(t *testing.T, env map[string]string, args ...string) (int, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	dir := t.TempDir()
	code := Run(context.Background(), append([]string{"--config", filepath.Join(dir, "cli.yaml")}, args...), Env{
		Stdin:  strings.NewReader(""),
		Stdout: &stdout,
		Stderr: &stderr,
		Getenv: func(k string) string { return env[k] },
	})
	return code, stderr.String()
}

func TestContextCreateEmptyName(t *testing.T) {
	code, stderr := runInDir(t, map[string]string{"NEBLA_TOKEN": "tok"}, "context", "create", " ", "--endpoint", "http://127.0.0.1:1")
	if code != 1 || !strings.Contains(stderr, "context name must not be empty") {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
}

func TestContextCreateBlankEnvToken(t *testing.T) {
	code, stderr := runInDir(t, map[string]string{"NEBLA_TOKEN": "  \n"}, "context", "create", "home", "--endpoint", "http://127.0.0.1:1")
	if code != 1 || !strings.Contains(stderr, "no token given") {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
}

func TestContextCreateTrimsEnvToken(t *testing.T) {
	h := newHarness(t)
	_, stderr, code := h.runEnv(map[string]string{"NEBLA_TOKEN": " " + h.Token + "\n"}, "", "context", "create", "home", "--endpoint", h.URL)
	if code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	cfg, _ := LoadConfig(h.configFile())
	if c := cfg.find("home"); c == nil || c.Token != h.Token {
		t.Fatalf("home=%+v", c)
	}
}

func TestContextListJSON(t *testing.T) {
	h := newHarness(t)
	out, errOut, code := h.run("-o", "json", "context", "list")
	var got []map[string]any
	if code != 0 || json.Unmarshal([]byte(out), &got) != nil || len(got) != 1 {
		t.Fatalf("json: %d %q %q", code, out, errOut)
	}
	if got[0]["name"] != "test" || got[0]["endpoint"] != h.URL || got[0]["active"] != true || len(got[0]) != 3 || strings.Contains(out, h.Token) {
		t.Fatalf("got %q", out)
	}
	out, _, code = h.run("context", "list", "--no-header")
	if code != 0 || strings.Contains(out, "NAME") || !strings.Contains(out, "test") {
		t.Fatalf("no-header: %d %q", code, out)
	}
}

func TestContextCreateJSON(t *testing.T) {
	h := newHarness(t)
	out, errOut, code := h.runEnv(map[string]string{"NEBLA_TOKEN": h.Token}, "", "-o", "json", "context", "create", "home", "--endpoint", h.URL)
	var got map[string]any
	if code != 0 || json.Unmarshal([]byte(out), &got) != nil || got["name"] != "home" || got["active"] != false || strings.Contains(out, h.Token) {
		t.Fatalf("create json: %d %q %q", code, out, errOut)
	}
}

func TestContextCreateInterrupted(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	var stderr bytes.Buffer
	e := Env{Stdin: pr, Stdout: io.Discard, Stderr: &stderr, Getenv: func(string) string { return "" }, ConfigDir: dir}
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	codes := make(chan int, 1)
	go func() {
		codes <- Run(ctx, []string{"context", "create", "home", "--endpoint", "http://127.0.0.1:1"}, e)
	}()
	select {
	case code := <-codes:
		if code != 130 {
			t.Fatalf("code=%d stderr=%q", code, stderr.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("create did not return after cancel")
	}
}

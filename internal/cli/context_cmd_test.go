package cli

import (
	"strings"
	"testing"
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

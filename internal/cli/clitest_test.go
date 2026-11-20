package cli

import (
	"bytes"
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hexeption/nebla/internal/httpapi"
	"github.com/Hexeption/nebla/internal/testutil"
)

type harness struct {
	t          *testing.T
	Env        *testutil.ServerEnv
	URL, Token string
	dir        string
}

func newHarness(t *testing.T, opts ...func(*httpapi.Deps)) *harness {
	t.Helper()
	return buildHarness(t, testutil.NewAppWithWorkers(t, testutil.ServerWorkers, opts...))
}

func newHarnessQueueOnly(t *testing.T) *harness {
	t.Helper()
	return buildHarness(t, testutil.NewApp(t, testutil.QueueOnlyJobs(t)))
}

func buildHarness(t *testing.T, app *testutil.App) *harness {
	t.Helper()
	e := testutil.NewServerEnv(app)
	h := &harness{t: t, Env: e, URL: app.URL, Token: e.C.CreateToken(e.Project, "read_write"), dir: t.TempDir()}
	cfg := &Config{ActiveContext: "test", Contexts: []Context{{Name: "test", Endpoint: h.URL, Token: h.Token}}, path: h.configFile()}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	return h
}

func (h *harness) configFile() string { return filepath.Join(h.dir, "cli.yaml") }

func (h *harness) run(args ...string) (string, string, int) {
	return h.runEnv(nil, "", args...)
}

func (h *harness) runEnv(env map[string]string, stdin string, args ...string) (string, string, int) {
	var stdout, stderr bytes.Buffer
	e := Env{
		Stdin:      strings.NewReader(stdin),
		Stdout:     &stdout,
		Stderr:     &stderr,
		Getenv:     func(k string) string { return env[k] },
		ConfigDir:  h.dir,
		HTTPClient: http.DefaultClient,
		IsTerminal: func(int) bool { return false },
	}
	code := Run(context.Background(), append([]string{"--config", h.configFile()}, args...), e)
	return stdout.String(), stderr.String(), code
}

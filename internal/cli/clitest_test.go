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
	"bytes"
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nebla-cloud/nebla/internal/httpapi"
	"github.com/nebla-cloud/nebla/internal/testutil"
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

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
	"fmt"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func runCLI(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), args, Env{
		Stdin:  strings.NewReader(""),
		Stdout: &stdout,
		Stderr: &stderr,
		Getenv: func(string) string { return "" },
	})
	return code, stdout.String(), stderr.String()
}

func TestVersion(t *testing.T) {
	code, stdout, _ := runCLI(t, "version")
	if code != 0 || stdout != "nebla dev\n" {
		t.Fatalf("code=%d stdout=%q", code, stdout)
	}
}

func TestVersionWithBuildInfo(t *testing.T) {
	v, c, d := Version, Commit, Date
	t.Cleanup(func() { Version, Commit, Date = v, c, d })
	Version, Commit, Date = "0.3.0", "abc1234", "2026-11-20"
	code, stdout, _ := runCLI(t, "version")
	if code != 0 || stdout != "nebla 0.3.0 (abc1234, 2026-11-20)\n" {
		t.Fatalf("code=%d stdout=%q", code, stdout)
	}
	Date = ""
	if _, stdout, _ = runCLI(t, "version"); stdout != "nebla 0.3.0 (abc1234)\n" {
		t.Fatalf("stdout=%q", stdout)
	}
}

func TestUnknownOutputFormat(t *testing.T) {
	code, _, stderr := runCLI(t, "version", "-o", "xml")
	if code != 1 || !strings.Contains(stderr, `invalid output format "xml"`) {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
}

func TestUnknownCommand(t *testing.T) {
	code, _, stderr := runCLI(t, "nope")
	if code != 1 || strings.Count(stderr, "error:") != 1 {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
}

func TestHelpHasNoForbiddenWords(t *testing.T) {
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		text := strings.ToLower(c.Short + c.Long + c.Example)
		for _, w := range []string{"hetzner", "hcloud"} {
			if strings.Contains(text, w) {
				t.Errorf("%s help contains %q", c.CommandPath(), w)
			}
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(newRootCmd(Env{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}))
}

func TestExitCodeChildStatus(t *testing.T) {
	var b bytes.Buffer
	if c := exitCode(&b, fmt.Errorf("wrap: %w", exitStatus(255))); c != 255 || b.Len() != 0 {
		t.Fatalf("got %d %q", c, b.String())
	}
}

func TestUnauthorizedNamesContext(t *testing.T) {
	h := newHarness(t)
	cfg := &Config{ActiveContext: "home", Contexts: []Context{{Name: "home", Endpoint: h.URL, Token: "bad"}}, path: h.configFile()}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	_, errOut, code := h.run("server", "list")
	if code != 1 || !strings.Contains(errOut, "(context home, "+h.URL+")") {
		t.Fatalf("code=%d stderr=%q", code, errOut)
	}
}

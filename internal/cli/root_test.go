package cli

import (
	"bytes"
	"context"
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

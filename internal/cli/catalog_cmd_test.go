package cli

import (
	"regexp"
	"strings"
	"testing"
)

func TestCatalogLists(t *testing.T) {
	h := newHarness(t)
	out, e, code := h.run("image", "list")
	if code != 0 || !strings.Contains(out, "ubuntu") {
		t.Fatalf("image: %d %q %q", code, out, e)
	}
	out, e, code = h.run("image", "list", "--location", "eu-1")
	if code != 0 || !strings.Contains(out, "ubuntu") {
		t.Fatalf("image location: %d %q %q", code, out, e)
	}
	out, e, code = h.run("server-type", "list")
	if code != 0 || !regexp.MustCompile(`(?m)small\s+2\s+2 GB\s+20 GB\s+no$`).MatchString(out) {
		t.Fatalf("server-type: %d %q %q", code, out, e)
	}
	out, e, code = h.run("location", "list")
	if code != 0 || !strings.Contains(out, "eu-1") {
		t.Fatalf("location: %d %q %q", code, out, e)
	}
}

package cli

import (
	"encoding/json"

	"github.com/Hexeption/nebla/internal/cli/apiclient"
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

func TestImageListUnknownLocation(t *testing.T) {
	h := newHarness(t)
	out, errOut, code := h.run("image", "list", "--location", "nowhere")
	if code != 1 || out != "" || !strings.Contains(errOut, "not found (not_found)") {
		t.Fatalf("got %d %q %q", code, out, errOut)
	}
}

func TestServerTypeListJSON(t *testing.T) {
	h := newHarness(t)
	out, e, code := h.run("-o", "json", "server-type", "list")
	var types []apiclient.ServerType
	if code != 0 || json.Unmarshal([]byte(out), &types) != nil || len(types) == 0 {
		t.Fatalf("json: %d %q %q", code, out, e)
	}
	found := false
	for _, s := range types {
		found = found || s.Name == "small"
	}
	if !found {
		t.Fatalf("no small in %q", out)
	}
}

func TestMemoryString(t *testing.T) {
	if got := memoryString(2048); got != "2 GB" {
		t.Fatal(got)
	}
	if got := memoryString(1536); got != "1536 MB" {
		t.Fatal(got)
	}
}

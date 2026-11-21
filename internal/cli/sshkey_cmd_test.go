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
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nebla-cloud/nebla/internal/cli/apiclient"
)

const testPubKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl test@example\n"

func writeKey(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "id.pub")
	if err := os.WriteFile(p, []byte(testPubKey), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSSHKeyLifecycle(t *testing.T) {
	h := newHarness(t)
	out, errOut, code := h.run("ssh-key", "create", "--name", "laptop", "--public-key-from-file", writeKey(t))
	if code != 0 || !strings.Contains(out, "SSH key ") || !strings.Contains(out, " created") {
		t.Fatalf("create: %d %q %q", code, out, errOut)
	}
	out, errOut, code = h.run("ssh-key", "list")
	if code != 0 || !strings.Contains(out, "laptop") || strings.Count(out, ":") < 15 {
		t.Fatalf("list: %d %q %q", code, out, errOut)
	}
	out, errOut, code = h.run("ssh-key", "delete", "laptop")
	if code != 0 || !strings.Contains(out, "SSH key laptop deleted") {
		t.Fatalf("delete: %d %q %q", code, out, errOut)
	}
	out, _, code = h.run("ssh-key", "list", "--no-header")
	if code != 0 || strings.TrimSpace(out) != "" {
		t.Fatalf("list after delete: %d %q", code, out)
	}
}

func TestSSHKeyCreateNeedsOneSource(t *testing.T) {
	h := newHarness(t)
	want := "one of --public-key or --public-key-from-file is required"
	_, errOut, code := h.run("ssh-key", "create", "--name", "k")
	if code != 1 || !strings.Contains(errOut, want) {
		t.Fatalf("none: %d %q", code, errOut)
	}
	_, errOut, code = h.run("ssh-key", "create", "--name", "k", "--public-key", "x", "--public-key-from-file", writeKey(t))
	if code != 1 || !strings.Contains(errOut, want) {
		t.Fatalf("both: %d %q", code, errOut)
	}
}

func TestSSHKeyListJSON(t *testing.T) {
	h := newHarness(t)
	if _, e, code := h.run("ssh-key", "create", "--name", "laptop", "--public-key", strings.TrimSpace(testPubKey)); code != 0 {
		t.Fatal(e)
	}
	out, _, code := h.run("-o", "json", "ssh-key", "list")
	var keys []apiclient.SshKey
	if code != 0 || json.Unmarshal([]byte(out), &keys) != nil || len(keys) != 1 || keys[0].Name != "laptop" {
		t.Fatalf("json: %d %q", code, out)
	}
}

func TestSSHKeyDeleteJSON(t *testing.T) {
	h := newHarness(t)
	if _, e, code := h.run("ssh-key", "create", "--name", "laptop", "--public-key", strings.TrimSpace(testPubKey)); code != 0 {
		t.Fatal(e)
	}
	out, errOut, code := h.run("-o", "json", "ssh-key", "delete", "laptop")
	var k apiclient.SshKey
	if code != 0 || json.Unmarshal([]byte(out), &k) != nil || k.Name != "laptop" {
		t.Fatalf("delete json: %d %q %q", code, out, errOut)
	}
}

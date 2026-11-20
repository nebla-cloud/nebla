package cli

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/cli/apiclient"
)

func createArgs(name string, extra ...string) []string {
	return append([]string{"server", "create", "--name", name, "--type", "small", "--image", "ubuntu", "--location", "eu-1"}, extra...)
}

func TestServerCreateWaits(t *testing.T) {
	h := newHarness(t)
	out, errOut, code := h.run(createArgs("web1")...)
	if code != 0 || !strings.Contains(out, "Server ") || !strings.Contains(out, " created") || !strings.Contains(out, "IPv4: 10.0.0.10") {
		t.Fatalf("create: %d %q %q", code, out, errOut)
	}
	out, _, code = h.run("server", "list")
	if code != 0 || !strings.Contains(out, "web1") || !strings.Contains(out, "running") {
		t.Fatalf("list: %d %q", code, out)
	}
}

func TestServerCreateJSONIsClean(t *testing.T) {
	h := newHarness(t)
	out, errOut, code := h.run(append([]string{"-o", "json"}, createArgs("web1")...)...)
	if code != 0 {
		t.Fatalf("create: %d %q", code, errOut)
	}
	var doc struct {
		Server      apiclient.Server   `json:"server"`
		Action      apiclient.Action   `json:"action"`
		NextActions []apiclient.Action `json:"next_actions"`
	}
	dec := json.NewDecoder(strings.NewReader(out))
	if err := dec.Decode(&doc); err != nil || dec.More() {
		t.Fatalf("stdout is not one JSON document: %v %q", err, out)
	}
	if doc.Server.Status != "running" || doc.Action.Status != apiclient.ActionStatusSuccess {
		t.Fatalf("got server %q action %q", doc.Server.Status, doc.Action.Status)
	}
	for _, a := range doc.NextActions {
		if a.Status != apiclient.ActionStatusSuccess {
			t.Fatalf("next action %d is %q", a.Id, a.Status)
		}
	}
	if !strings.Contains(errOut, "Waiting for") {
		t.Fatalf("no progress on stderr: %q", errOut)
	}
}

func TestServerCreateNoWait(t *testing.T) {
	h := newHarnessQueueOnly(t)
	out, errOut, code := h.run(createArgs("web1", "--no-wait")...)
	if code != 0 || !strings.Contains(out, "is being created (action ") {
		t.Fatalf("create: %d %q %q", code, out, errOut)
	}
	out, _, _ = h.run("server", "list")
	if strings.Contains(out, "running") {
		t.Fatalf("server should not be running yet: %q", out)
	}
}

func TestServerCreateUnknownImage(t *testing.T) {
	h := newHarness(t)
	args := createArgs("web1")
	args[7] = "nope"
	out, errOut, code := h.run(args...)
	if code != 1 || out != "" || !strings.HasPrefix(errOut, "error: ") {
		t.Fatalf("got %d %q %q", code, out, errOut)
	}
}

func TestServerCreateBadLabel(t *testing.T) {
	h := newHarness(t)
	_, errOut, code := h.run(createArgs("web1", "--label", "x")...)
	if code != 1 || !strings.Contains(errOut, `invalid label "x": want key=value`) {
		t.Fatalf("got %d %q", code, errOut)
	}
}

func TestServerDescribeAndRename(t *testing.T) {
	h := newHarness(t)
	if _, e, code := h.run(createArgs("web1", "--label", "env=prod")...); code != 0 {
		t.Fatal(e)
	}
	out, _, code := h.run("server", "describe", "web1")
	for _, want := range []string{"Location:", "eu-1", "Type:", "small (2 cores, 2 GB, 20 GB)", "env=prod"} {
		if code != 0 || !strings.Contains(out, want) {
			t.Fatalf("describe missing %q: %d %q", want, code, out)
		}
	}
	out, errOut, code := h.run("server", "rename", "web1", "web2")
	if code != 0 || !strings.Contains(out, "Server web1 renamed to web2") {
		t.Fatalf("rename: %d %q %q", code, out, errOut)
	}
	if _, e, code := h.run("server", "describe", "web2"); code != 0 {
		t.Fatal(e)
	}
	_, errOut, code = h.run("server", "describe", "web1")
	if code != 1 || !strings.Contains(errOut, `server "web1" not found`) {
		t.Fatalf("describe old: %d %q", code, errOut)
	}
}

func TestServerListFilters(t *testing.T) {
	h := newHarness(t)
	if _, e, code := h.run(createArgs("web1", "--label", "env=prod")...); code != 0 {
		t.Fatal(e)
	}
	if _, e, code := h.run(createArgs("web2")...); code != 0 {
		t.Fatal(e)
	}
	out, _, code := h.run("server", "list", "--selector", "env=prod")
	if code != 0 || !strings.Contains(out, "web1") || strings.Contains(out, "web2") {
		t.Fatalf("selector: %d %q", code, out)
	}
	out, _, code = h.run("server", "list", "--status", "running", "--no-header")
	if code != 0 || !strings.Contains(out, "web1") || !strings.Contains(out, "web2") {
		t.Fatalf("status: %d %q", code, out)
	}
	out, _, code = h.run("server", "list", "--status", "off")
	if code != 0 || strings.Contains(out, "web1") {
		t.Fatalf("status off: %d %q", code, out)
	}
}

func TestServerDelete(t *testing.T) {
	h := newHarness(t)
	if _, e, code := h.run(createArgs("web1")...); code != 0 {
		t.Fatal(e)
	}
	out, errOut, code := h.run("server", "delete", "web1")
	if code != 0 || !strings.Contains(out, "Server web1 deleted") {
		t.Fatalf("delete: %d %q %q", code, out, errOut)
	}
	out, _, code = h.run("server", "list", "--no-header")
	if code != 0 || strings.TrimSpace(out) != "" {
		t.Fatalf("list: %d %q", code, out)
	}
}

func TestServerCreateWaitFailureNamesServer(t *testing.T) {
	h := newHarnessQueueOnly(t)
	ctx := context.Background()
	stop := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		for {
			ids, _ := h.Env.App.Ent.Action.Query().IDs(ctx)
			if len(ids) > 0 {
				for _, id := range ids {
					_ = actions.Fail(ctx, h.Env.App.Ent, id, "pve_error", "boom")
				}
				return
			}
			select {
			case <-stop:
				return
			case <-time.After(20 * time.Millisecond):
			}
		}
	}()
	_, errOut, code := h.run(createArgs("web1")...)
	close(stop)
	<-finished
	if code != 1 || !regexp.MustCompile(`server \d+ was created, but:`).MatchString(errOut) || !strings.Contains(errOut, "boom") {
		t.Fatalf("got %d %q", code, errOut)
	}
}

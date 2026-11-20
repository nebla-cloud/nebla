package cli

import (
	"bytes"
	"context"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestSSHUser(t *testing.T) {
	cases := map[string]string{
		"debian": "debian", "Debian": "debian", "ubuntu": "ubuntu", "rocky": "rocky",
		"almalinux": "almalinux", "alma": "almalinux", "fedora": "fedora",
		"centos": "cloud-user", "arch": "root", "": "root",
	}
	for in, want := range cases {
		if got := sshUser(in); got != want {
			t.Errorf("sshUser(%q) = %q, want %q", in, got, want)
		}
	}
}

// runSSH runs the CLI with Exec captured and returns the exec'd argv.
func runSSH(h *harness, args ...string) ([]string, string, int) {
	var stdout, stderr bytes.Buffer
	var got []string
	e := Env{
		Stdin:      strings.NewReader(""),
		Stdout:     &stdout,
		Stderr:     &stderr,
		Getenv:     func(string) string { return "" },
		ConfigDir:  h.dir,
		HTTPClient: http.DefaultClient,
		IsTerminal: func(int) bool { return false },
		LookPath:   func(string) (string, error) { return "/usr/bin/ssh", nil },
		Exec: func(argv0 string, argv []string, _ []string) error {
			got = append([]string{argv0}, argv...)
			return nil
		},
	}
	code := Run(context.Background(), append([]string{"--config", h.configFile()}, args...), e)
	return got, stderr.String(), code
}

func TestServerSSHArgv(t *testing.T) {
	h := runningServer(t)
	got, errOut, code := runSSH(h, "server", "ssh", "web1")
	if code != 0 || !reflect.DeepEqual(got, []string{"/usr/bin/ssh", "ssh", "ubuntu@10.0.0.10"}) {
		t.Fatalf("default: %d %v %q", code, got, errOut)
	}
	got, errOut, code = runSSH(h, "server", "ssh", "web1", "-u", "root", "-p", "2222", "--", "-A")
	if code != 0 || !reflect.DeepEqual(got, []string{"/usr/bin/ssh", "ssh", "-p", "2222", "-A", "root@10.0.0.10"}) {
		t.Fatalf("flags: %d %v %q", code, got, errOut)
	}
}

func TestServerSSHIPv6(t *testing.T) {
	h := newHarness(t)
	if _, errOut, code := h.run(createArgs("web6", "--with-ipv6")...); code != 0 {
		t.Fatalf("create: %d %q", code, errOut)
	}
	got, errOut, code := runSSH(h, "server", "ssh", "web6", "--ipv6")
	if code != 0 || !reflect.DeepEqual(got, []string{"/usr/bin/ssh", "ssh", "ubuntu@2001:db8::10"}) {
		t.Fatalf("ipv6: %d %v %q", code, got, errOut)
	}
}

func TestServerSSHNoIP(t *testing.T) {
	h := newHarness(t)
	if _, errOut, code := h.run(createArgs("noip", "--without-ipv4")...); code != 0 {
		t.Fatalf("create: %d %q", code, errOut)
	}
	_, errOut, code := runSSH(h, "server", "ssh", "noip")
	if code != 1 || !strings.Contains(errOut, "server noip has no public IP") {
		t.Fatalf("noip: %d %q", code, errOut)
	}
}

func TestServerSSHNoIPv6(t *testing.T) {
	h := runningServer(t)
	_, errOut, code := runSSH(h, "server", "ssh", "web1", "--ipv6")
	if code != 1 || !strings.Contains(errOut, "server web1 has no IPv6 address") {
		t.Fatalf("no ipv6: %d %q", code, errOut)
	}
}

func TestServerSSHBadPort(t *testing.T) {
	h := runningServer(t)
	for _, p := range []string{"0", "70000"} {
		if _, errOut, code := runSSH(h, "server", "ssh", "web1", "-p", p); code != 1 || !strings.Contains(errOut, "invalid port") {
			t.Fatalf("port %s: %d %q", p, code, errOut)
		}
	}
}

func TestServerSSHRejectsRemoteCommand(t *testing.T) {
	h := runningServer(t)
	got, errOut, code := runSSH(h, "server", "ssh", "web1", "--", "uptime")
	if code != 1 || got != nil || !strings.Contains(errOut, "arguments after -- are ssh options") {
		t.Fatalf("got %v %d %q", got, code, errOut)
	}
}

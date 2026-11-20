package enroll

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	testPanel  = "https://panel.test"
	testToken  = "nbe_AbCdEfGhIjKlMnOpQrStUvWxYz0123456789-_abcde"
	testName   = "nebla-home-ab12cd"
	stubSecret = "0b7c6a52-6a49-4c8e-9a0e-5b2f1d3c4e5f"
)

const (
	privs8   = "VM.Allocate VM.Clone VM.Config.CDROM VM.Config.CPU VM.Config.Disk VM.Config.Memory VM.Config.Network VM.Config.Options VM.Config.Cloudinit VM.PowerMgmt VM.Console VM.Audit VM.Monitor Datastore.AllocateSpace Datastore.AllocateTemplate Datastore.Audit Sys.Audit SDN.Use"
	privs9   = "VM.Allocate VM.Clone VM.Config.CDROM VM.Config.CPU VM.Config.Disk VM.Config.Memory VM.Config.Network VM.Config.Options VM.Config.Cloudinit VM.PowerMgmt VM.Console VM.Audit VM.GuestAgent.Audit Datastore.AllocateSpace Datastore.AllocateTemplate Datastore.Audit Sys.Audit SDN.Use"
	privsISO = "Datastore.Allocate Datastore.AllocateSpace Datastore.AllocateTemplate Datastore.Audit"
)

type scriptRun struct {
	calls  [][]string
	stdout string
	stderr string
	code   int
	body   string
}

func (r scriptRun) has(prefix ...string) bool { return r.count(prefix...) > 0 }

func (r scriptRun) count(prefix ...string) int {
	n := 0
	for _, c := range r.calls {
		if len(c) >= len(prefix) && strings.Join(c[:len(prefix)], "\x00") == strings.Join(prefix, "\x00") {
			n++
		}
	}
	return n
}

func (r scriptRun) privs(verb, role string) string {
	for _, c := range r.calls {
		if len(c) >= 6 && c[0] == "pveum" && c[1] == "role" && c[2] == verb && c[3] == role && c[4] == "--privs" {
			return c[5]
		}
	}
	return ""
}

func (r scriptRun) lastLine() string {
	lines := strings.Split(strings.TrimRight(r.stdout, "\n"), "\n")
	return lines[len(lines)-1]
}

// execScript runs bash with args against the stubs and a fake /etc/pve.
func execScript(t *testing.T, args []string, env map[string]string) scriptRun {
	t.Helper()
	stubs, err := filepath.Abs("testdata/stubs")
	require.NoError(t, err)
	tmp := t.TempDir()
	pveDir := filepath.Join(tmp, "pve")
	for name, content := range map[string]string{
		"nodes/pve1/pve-ssl.pem":      fp("AA"),
		"nodes/pve2/pve-ssl.pem":      fp("CC"),
		"nodes/pve2/pveproxy-ssl.pem": fp("BB"),
	} {
		p := filepath.Join(pveDir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(content+"\n"), 0o644))
	}
	vars := map[string]string{
		"PATH":              stubs + ":/usr/bin:/bin",
		"HOME":              tmp,
		"NEBLA_PVE_DIR":     pveDir,
		"NEBLA_TOKEN":       testToken,
		"STUB_LOG":          filepath.Join(tmp, "calls.log"),
		"STUB_BODY":         filepath.Join(tmp, "body.json"),
		"STUB_SECRET":       stubSecret,
		"STUB_EXPECT_TOKEN": testToken,
	}
	for k, v := range env {
		vars[k] = v
	}
	bash, err := exec.LookPath("bash")
	require.NoError(t, err)
	cmd := exec.Command(bash, args...)
	for k, v := range vars {
		if v != "-" {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	r := scriptRun{code: 0}
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		require.True(t, errors.As(err, &ee), "run bash: %v", err)
		r.code = ee.ExitCode()
	}
	r.stdout, r.stderr = stdout.String(), stderr.String()
	if log, err := os.ReadFile(vars["STUB_LOG"]); err == nil {
		for _, l := range strings.Split(strings.TrimRight(string(log), "\n"), "\n") {
			if l != "" {
				r.calls = append(r.calls, strings.Split(l, "\t"))
			}
		}
	}
	for _, c := range r.calls {
		argv := strings.Join(c, " ")
		require.False(t, strings.Contains(argv, testToken), "enrollment token in argv of %s", c[0])
		require.False(t, strings.Contains(argv, stubSecret), "token secret in argv of %s", c[0])
	}
	require.False(t, r.has("pveum", "user", "token", "remove"), "the script removed a token")
	if b, err := os.ReadFile(vars["STUB_BODY"]); err == nil {
		r.body = string(b)
	}
	return r
}

// redact keeps the test token and secret out of failure messages.
func redact(s string) string {
	return strings.NewReplacer(testToken, "[token]", stubSecret, "[secret]").Replace(s)
}

func runScript(t *testing.T, env map[string]string) scriptRun {
	t.Helper()
	f := filepath.Join(t.TempDir(), "enroll.sh")
	require.NoError(t, os.WriteFile(f, Script(testPanel), 0o644))
	return execScript(t, []string{f}, env)
}

func noWrites(t *testing.T, r scriptRun) {
	t.Helper()
	for _, w := range [][]string{
		{"pveum", "role", "add"}, {"pveum", "role", "modify"}, {"pveum", "user", "add"},
		{"pveum", "acl"}, {"pveum", "user", "token", "add"}, {"pveum", "user", "token", "remove"},
	} {
		require.False(t, r.has(w...), "unexpected %v", w)
	}
}

func TestScriptRender(t *testing.T) {
	t.Parallel()
	s := string(Script(testPanel))
	require.Contains(t, s, "\nPANEL_URL='https://panel.test'\n")
	require.NotContains(t, s, "{{")
	require.NotContains(t, s, "\t")
	f := filepath.Join(t.TempDir(), "enroll.sh")
	require.NoError(t, os.WriteFile(f, []byte(s), 0o644))
	out, err := exec.Command("bash", "-n", f).CombinedOutput()
	require.NoError(t, err, string(out))
	for _, bad := range []string{"", "ftp://x", "https://x'; rm -rf /", "https://a b", "https://x/$(id)", "https://x/\nfoo"} {
		require.Panics(t, func() { Script(bad) }, bad)
	}
	require.NotPanics(t, func() { Script("http://[::1]:8080/panel/") })
}

func TestScriptCommand(t *testing.T) {
	t.Parallel()
	want := "curl -fsSL 'https://panel.test/v1/enroll.sh' | NEBLA_TOKEN=" + testToken + " bash"
	require.True(t, Command(testPanel, testToken) == want, "command mismatch")
	require.True(t, Command(testPanel+"/", testToken) == want, "command mismatch with trailing slash")
	require.Panics(t, func() { Command(testPanel, "nbe_x; rm -rf /") })
	off := OfflineScript(testPanel, testToken, testName)
	require.True(t, strings.HasPrefix(off, "NEBLA_TOKEN="+testToken+" NEBLA_TOKEN_NAME="+testName+" NEBLA_OFFLINE=1 bash <<'NEBLA_EOF'\n"+string(Script(testPanel))))
	require.True(t, strings.HasSuffix(off, "\nNEBLA_EOF\n"))
	require.Panics(t, func() { OfflineScript(testPanel, testToken, "panel") })
}

func TestScriptRefusesPVE7(t *testing.T) {
	t.Parallel()
	r := runScript(t, map[string]string{"STUB_PVE": "7.4-3"})
	require.Equal(t, 1, r.code)
	require.Contains(t, r.stderr, "nebla: error: Nebla supports Proxmox VE 8 and 9; this node runs 7.4-3")
	require.False(t, r.has("pveum"))
	require.False(t, r.has("pvesh"))
	require.False(t, r.has("curl"))
}

func TestScriptRefusesNonRoot(t *testing.T) {
	t.Parallel()
	r := runScript(t, map[string]string{"STUB_UID": "1000"})
	require.Equal(t, 1, r.code)
	require.Contains(t, r.stderr, "root")
	require.False(t, r.has("pveum"))
	require.False(t, r.has("curl"))
}

func TestScriptRejectsBadToken(t *testing.T) {
	t.Parallel()
	for _, tok := range []string{"abc", "-"} {
		r := runScript(t, map[string]string{"NEBLA_TOKEN": tok})
		require.Equal(t, 1, r.code)
		require.Contains(t, r.stderr, "NEBLA_TOKEN")
		require.False(t, r.has("pveum"))
		require.False(t, r.has("curl"))
	}
}

func TestScriptPreflightUsedStopsBeforeChanges(t *testing.T) {
	t.Parallel()
	r := runScript(t, map[string]string{"STUB_CHECK": "409"})
	require.Equal(t, 1, r.code)
	require.Contains(t, r.stderr, "This enrollment was already used.")
	noWrites(t, r)
	require.Equal(t, 1, r.count("curl", "-fsS"))
	require.False(t, r.has("pveum"))
}

func TestScriptPreflightUnreachable(t *testing.T) {
	t.Parallel()
	r := runScript(t, map[string]string{"STUB_CHECK": "000"})
	require.Equal(t, 1, r.code)
	require.Contains(t, r.stderr, `can't reach https://panel.test; use "The node can't reach this panel?" on the enrollment page`)
	noWrites(t, r)
}

func TestScriptPVE8Privs(t *testing.T) {
	t.Parallel()
	r := runScript(t, map[string]string{"STUB_PVE": "8.2.4", "STUB_ADMIN_PRIVS": "Sys.Audit Sys.AccessNetwork VM.Config.HWType"})
	require.Equal(t, 0, r.code, redact(r.stderr))
	require.Equal(t, privs8+" Sys.AccessNetwork VM.Config.HWType", r.privs("add", "Nebla"))
	require.Equal(t, privsISO, r.privs("add", "NeblaISO"))

	r = runScript(t, map[string]string{"STUB_PVE": "8.2.4", "STUB_ADMIN_PRIVS": "Sys.Audit VM.Config.HWType"})
	require.Equal(t, 0, r.code, redact(r.stderr))
	require.Equal(t, privs8+" VM.Config.HWType", r.privs("add", "Nebla"))
}

func TestScriptPVE9Privs(t *testing.T) {
	t.Parallel()
	r := runScript(t, map[string]string{"STUB_ADMIN_PRIVS": "Sys.Audit"})
	require.Equal(t, 0, r.code, redact(r.stderr))
	require.Equal(t, privs9, r.privs("add", "Nebla"))
	require.Equal(t, privsISO, r.privs("add", "NeblaISO"))

	r = runScript(t, map[string]string{"STUB_ADMIN_PRIVS": "Sys.AccessNetwork VM.Config.HWType"})
	require.Equal(t, 0, r.code, redact(r.stderr))
	require.Equal(t, privs9+" Sys.AccessNetwork VM.Config.HWType", r.privs("add", "Nebla"))
}

func TestScriptRerunModifiesRoles(t *testing.T) {
	t.Parallel()
	r := runScript(t, map[string]string{"STUB_ROLES": "Administrator NeblaISOx Nebla NeblaISO", "STUB_USERS": "root@pam nebla@pve"})
	require.Equal(t, 0, r.code, redact(r.stderr))
	require.False(t, r.has("pveum", "role", "add"))
	require.Equal(t, privs9, r.privs("modify", "Nebla"))
	require.Equal(t, privsISO, r.privs("modify", "NeblaISO"))
	require.False(t, r.has("pveum", "user", "add"))

	r = runScript(t, map[string]string{"STUB_ROLES": "Administrator NeblaX", "STUB_USERS": "root@pam nebla@pvex"})
	require.Equal(t, 0, r.code, redact(r.stderr))
	require.False(t, r.has("pveum", "role", "modify"))
	require.Equal(t, 2, r.count("pveum", "role", "add"))
	require.True(t, r.has("pveum", "user", "add", "nebla@pve", "--comment", "Nebla panel"))
}

func TestScriptUserAndACLs(t *testing.T) {
	t.Parallel()
	r := runScript(t, nil)
	require.Equal(t, 0, r.code, redact(r.stderr))
	require.True(t, r.has("pveum", "acl", "modify", "/", "--users", "nebla@pve", "--roles", "Nebla"))
	require.True(t, r.has("pveum", "acl", "modify", "/storage/local", "--users", "nebla@pve", "--roles", "NeblaISO"))
	require.True(t, r.has("pveum", "acl", "modify", "/storage/imp", "--users", "nebla@pve", "--roles", "NeblaISO"))
	require.Equal(t, 3, r.count("pveum", "acl", "modify"))
	require.True(t, r.has("pveum", "user", "token", "add", "nebla@pve", testName, "--privsep", "0", "--comment", "Nebla panel", "--output-format", "json"))
}

func TestScriptSkipsBadStorageID(t *testing.T) {
	t.Parallel()
	r := runScript(t, map[string]string{"STUB_STORAGE": `[{"content":"iso","storage":"9bad","type":"dir"},{"content":"iso","storage":"local","type":"dir"}]`})
	require.Equal(t, 0, r.code, redact(r.stderr))
	require.Contains(t, r.stderr, "9bad")
	require.False(t, r.has("pveum", "acl", "modify", "/storage/9bad"))
	require.True(t, r.has("pveum", "acl", "modify", "/storage/local"))
}

func TestScriptLeavesOtherTokens(t *testing.T) {
	t.Parallel()
	r := runScript(t, map[string]string{"STUB_TOKENS": "panel nebla-home-zzzzzz " + testName + "x", "STUB_USERS": "root@pam nebla@pve"})
	require.Equal(t, 0, r.code, redact(r.stderr))
	require.False(t, r.has("pveum", "user", "token", "remove"))
	require.Equal(t, 1, r.count("pveum", "user", "token", "add", "nebla@pve", testName))

	r = runScript(t, map[string]string{"STUB_TOKENS": "panel " + testName + " nebla-home-zzzzzz", "STUB_USERS": "root@pam nebla@pve"})
	require.Equal(t, 1, r.code)
	require.True(t, r.has("pveum", "user", "token", "list", "nebla@pve"))
	noWrites(t, r)
	require.Contains(t, r.stderr, "nebla: error: this setup command was already used; get a new command in the panel")
	require.False(t, r.has("pveum", "user", "token", "remove"))
	require.False(t, r.has("pveum", "user", "token", "add"))
	require.NotContains(t, r.stderr, "pveum user token remove")
	require.False(t, r.has("curl", "-sS", "--max-time", "20"))
}

func TestScriptTokenHintAfterLaterFailure(t *testing.T) {
	t.Parallel()
	r := runScript(t, map[string]string{"STUB_CLUSTER": `[{"id":"node/pve9","ip":"10.0.0.9","local":1,"name":"pve9","online":1,"type":"node"}]`})
	require.Equal(t, 1, r.code)
	require.True(t, r.has("pveum", "user", "token", "add"))
	require.Contains(t, r.stderr, "nebla: error: unexpected certificate fingerprint for node pve9")
	require.Contains(t, r.stderr, "pveum user token remove nebla@pve "+testName)
	require.False(t, r.has("curl", "-sS", "--max-time", "20"))
	require.NotContains(t, r.stdout, "nebla-enroll:")

	r = runScript(t, map[string]string{"STUB_SECRET": "not-a-uuid"})
	require.Equal(t, 1, r.code)
	require.Contains(t, r.stderr, "pveum user token remove nebla@pve "+testName)

	r = runScript(t, nil)
	require.Equal(t, 0, r.code, redact(r.stderr))
	require.NotContains(t, r.stdout+r.stderr, "pveum user token remove")
}

func TestScriptSendsTokenInConfig(t *testing.T) {
	t.Parallel()
	r := runScript(t, map[string]string{"STUB_EXPECT_TOKEN": "nbe_" + strings.Repeat("z", 43)})
	require.Equal(t, 1, r.code)
	require.Contains(t, r.stderr, "the panel refused this enrollment")
	noWrites(t, r)
}

func TestScriptOnlineReportParses(t *testing.T) {
	t.Parallel()
	r := runScript(t, nil)
	require.Equal(t, 0, r.code, redact(r.stderr))
	require.True(t, r.has("curl", "-sS", "--max-time", "20", "--config"))
	rep, err := ParseReport([]byte(r.body))
	require.NoError(t, err, "report rejected (body redacted)")
	require.Equal(t, "9.0.3", rep.PVEVersion)
	require.Equal(t, "pve1", rep.Node)
	require.Equal(t, "nebla@pve!"+testName, rep.TokenID)
	require.True(t, rep.TokenSecret == stubSecret, "token secret mismatch")
	require.Equal(t, []ReportNode{
		{Name: "pve1", IP: "10.0.0.1", Online: true, Fingerprint: strings.Repeat("AA", 32)},
		{Name: "pve2", IP: "10.0.0.2", Online: false, Fingerprint: strings.Repeat("BB", 32)},
	}, rep.Nodes)
	iso := map[string]bool{}
	for _, s := range rep.Storages {
		iso[s.ID] = s.NeblaISO
	}
	require.Equal(t, map[string]bool{"local": true, "local-lvm": false, "imp": true}, iso)
	require.Contains(t, r.stdout, "nebla@pve!"+testName)
	require.Contains(t, r.stdout, "Finish in the panel.")
	require.NotContains(t, r.stdout, "nebla-enroll:")
}

func TestScriptStandaloneNode(t *testing.T) {
	t.Parallel()
	r := runScript(t, map[string]string{"STUB_CLUSTER": `[{"id":"node/pve1","ip":"192.168.1.5","level":"","local":1,"name":"pve1","nodeid":0,"online":1,"type":"node"}]`})
	require.Equal(t, 0, r.code, redact(r.stderr))
	rep, err := ParseReport([]byte(r.body))
	require.NoError(t, err)
	require.Len(t, rep.Nodes, 1)
}

func TestScriptOfflineBlob(t *testing.T) {
	t.Parallel()
	r := execScript(t, []string{"-c", OfflineScript(testPanel, testToken, testName)}, map[string]string{"NEBLA_TOKEN": "-"})
	require.Equal(t, 0, r.code, redact(r.stderr))
	require.False(t, r.has("curl"))
	tok, raw, err := DecodeBlob(r.lastLine())
	require.NoError(t, err)
	require.True(t, tok == testToken, "blob token mismatch")
	rep, err := ParseReport(raw)
	require.NoError(t, err)
	require.Equal(t, "nebla@pve!"+testName, rep.TokenID)
	require.Contains(t, r.stderr, "Paste this line into the panel. It contains a secret; clear your scrollback afterwards.")
}

func TestScriptReportFailsFallsBackToBlob(t *testing.T) {
	t.Parallel()
	for _, code := range []string{"000", "500"} {
		r := runScript(t, map[string]string{"STUB_REPORT": code})
		require.Equal(t, 0, r.code, redact(r.stderr))
		tok, raw, err := DecodeBlob(r.lastLine())
		require.NoError(t, err, code)
		require.True(t, tok == testToken, "blob token mismatch")
		_, err = ParseReport(raw)
		require.NoError(t, err)
	}
}

func TestScriptReportUsedNoBlob(t *testing.T) {
	t.Parallel()
	for _, code := range []string{"401", "409", "410"} {
		r := runScript(t, map[string]string{"STUB_REPORT": code})
		require.Equal(t, 1, r.code, code)
		require.NotContains(t, r.stdout+r.stderr, "nebla-enroll:")
		require.Contains(t, r.stdout+r.stderr, "pveum user token remove nebla@pve "+testName)
		require.Contains(t, r.stdout+r.stderr, "This enrollment was already used.")
	}
}

func TestScriptNeverPrintsSecretOnline(t *testing.T) {
	t.Parallel()
	r := runScript(t, nil)
	require.Equal(t, 0, r.code, redact(r.stderr))
	require.False(t, strings.Contains(r.stdout, stubSecret), "secret on stdout")
	require.False(t, strings.Contains(r.stderr, stubSecret), "secret on stderr")
	require.False(t, strings.Contains(r.stdout+r.stderr, testToken), "enrollment token printed")
}

package enroll

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nebla-cloud/nebla/internal/apierr"
)

func fp(c string) string { return strings.TrimSuffix(strings.Repeat(c+":", 32), ":") }

func TestParseReportPVE9(t *testing.T) {
	raw, err := os.ReadFile("testdata/report-pve9.json")
	require.NoError(t, err)
	r, err := ParseReport(raw)
	require.NoError(t, err)
	require.Equal(t, "9.0.3", r.PVEVersion)
	require.Equal(t, "pve1", r.Node)
	require.Equal(t, "nebla@pve!nebla-home-ab12cd", r.TokenID)
	require.Equal(t, "3f2b8c1e-5d4a-4e7b-9a10-0c6d2e8f7a11", r.TokenSecret)
	require.Equal(t, []ReportNode{
		{Name: "pve1", IP: "10.0.0.1", Online: true, Fingerprint: strings.Repeat("AA", 32)},
		{Name: "pve2", IP: "10.0.0.2", Online: false, Fingerprint: strings.Repeat("BB", 32)},
	}, r.Nodes)
	require.Equal(t, []ReportStorage{
		{ID: "local", Type: "dir", Content: []string{"iso", "vztmpl", "backup"}, NeblaISO: true},
		{ID: "local-lvm", Type: "lvmthin", Content: []string{"images", "rootdir"}},
		{ID: "nfs-iso", Type: "nfs", Content: []string{"iso", "import"}, Shared: true, NeblaISO: true},
	}, r.Storages)
}

func baseReport() map[string]any {
	return map[string]any{
		"v": 1, "pve_version": "8.4-1", "node": "pve1",
		"token":        map[string]any{"full-tokenid": "nebla@pve!nebla-home-ab12cd", "value": "3f2b8c1e-5d4a-4e7b-9a10-0c6d2e8f7a11"},
		"cluster":      []any{map[string]any{"type": "node", "name": "pve1", "ip": "10.0.0.1", "online": true, "local": 1}},
		"storage":      []any{map[string]any{"storage": "local", "type": "dir", "content": "iso,backup", "shared": false}},
		"fingerprints": map[string]any{"pve1": fp("AA")},
		"nebla_iso":    []any{"local"},
	}
}

func TestParseReportRejects(t *testing.T) {
	many := func(n int) (cl []any, fps map[string]any) {
		fps = map[string]any{}
		for i := 0; i < n; i++ {
			name := "n" + strings.Repeat("a", i/26) + string(rune('a'+i%26))
			cl = append(cl, map[string]any{"type": "node", "name": name, "ip": "10.0.0.1", "online": 1})
			fps[name] = fp("AA")
		}
		return
	}
	type row struct {
		mut  func(m map[string]any)
		want string
	}
	node0 := func(m map[string]any) map[string]any { return m["cluster"].([]any)[0].(map[string]any) }
	stor0 := func(m map[string]any) map[string]any { return m["storage"].([]any)[0].(map[string]any) }
	tok := func(m map[string]any) map[string]any { return m["token"].(map[string]any) }
	cases := map[string]row{
		"v2":              {func(m map[string]any) { m["v"] = 2 }, "unsupported report version"},
		"version 7.4-1":   {func(m map[string]any) { m["pve_version"] = "7.4-1" }, "unsupported Proxmox VE version"},
		"version +9":      {func(m map[string]any) { m["pve_version"] = "+9.0" }, "invalid Proxmox VE version"},
		"version 09":      {func(m map[string]any) { m["pve_version"] = "09.0" }, "unsupported Proxmox VE version"},
		"version junk":    {func(m map[string]any) { m["pve_version"] = "x" }, "invalid Proxmox VE version"},
		"version long":    {func(m map[string]any) { m["pve_version"] = "9.0" + strings.Repeat("a", 64) }, "invalid Proxmox VE version"},
		"non-uuid secret": {func(m map[string]any) { tok(m)["value"] = "NOT-A-UUID" }, "invalid token secret"},
		"uppercase uuid": {func(m map[string]any) {
			tok(m)["value"] = "3F2B8C1E-5D4A-4E7B-9A10-0C6D2E8F7A11"
		}, "invalid token secret"},
		"wrong tokenid prefix": {func(m map[string]any) { tok(m)["full-tokenid"] = "root@pam!nebla-home-ab12cd" }, "invalid token id"},
		"bad tokenid suffix":   {func(m map[string]any) { tok(m)["full-tokenid"] = "nebla@pve!nebla-home-AB12CD" }, "invalid token id"},
		"no nodes": {func(m map[string]any) {
			m["cluster"] = []any{map[string]any{"type": "cluster", "name": "c"}}
		}, "no nodes"},
		"65 nodes": {func(m map[string]any) {
			cl, f := many(65)
			m["cluster"], m["fingerprints"], m["node"] = cl, f, "naa"
		}, "too many nodes"},
		"bad node name": {func(m map[string]any) { node0(m)["name"] = "pve_1" }, "invalid node name"},
		"duplicate node": {func(m map[string]any) {
			m["cluster"] = append(m["cluster"].([]any), node0(m))
		}, "duplicate node"},
		"bad node ip":   {func(m map[string]any) { node0(m)["ip"] = "10.0.0.256" }, "invalid node ip"},
		"empty node ip": {func(m map[string]any) { node0(m)["ip"] = "" }, "invalid node ip"},
		"offline reporting node without ip": {func(m map[string]any) {
			node0(m)["online"] = 0
			delete(node0(m), "ip")
		}, "invalid node ip for pve1"},
		"offline member bad ip":    {func(m map[string]any) { addNode(m, "pve2", "nope", 0) }, "invalid node ip for pve2"},
		"online member without ip": {func(m map[string]any) { addNode(m, "pve2", "", 1) }, "invalid node ip for pve2"},
		"missing fingerprint":      {func(m map[string]any) { m["fingerprints"] = map[string]any{} }, "fingerprint"},
		"short fingerprint":        {func(m map[string]any) { m["fingerprints"] = map[string]any{"pve1": "AA:BB"} }, "fingerprint"},
		"non-hex fingerprint":      {func(m map[string]any) { m["fingerprints"] = map[string]any{"pve1": strings.Repeat("ZZ", 32)} }, "fingerprint"},
		"node not in nodes":        {func(m map[string]any) { m["node"] = "other" }, "not in the cluster"},
		"quote in storage id": {func(m map[string]any) {
			stor0(m)["storage"] = `lo"cal`
			m["nebla_iso"] = []any{}
		}, "invalid storage id"},
		"newline in storage id": {func(m map[string]any) {
			stor0(m)["storage"] = "lo\ncal"
			m["nebla_iso"] = []any{}
		}, "invalid storage id"},
		"duplicate storage": {func(m map[string]any) {
			m["storage"] = append(m["storage"].([]any), stor0(m))
		}, "duplicate storage"},
		"long storage type": {func(m map[string]any) { stor0(m)["type"] = strings.Repeat("a", 65) }, "invalid storage type"},
		"bad storage type":  {func(m map[string]any) { stor0(m)["type"] = "d ir" }, "invalid storage type"},
		"bad content item":  {func(m map[string]any) { stor0(m)["content"] = "iso,b\"ad" }, "invalid storage content"},
		"long content item": {func(m map[string]any) { stor0(m)["content"] = "iso," + strings.Repeat("a", 65) }, "invalid storage content"},
		"nebla_iso without iso content": {func(m map[string]any) {
			stor0(m)["content"] = "images,backup"
		}, "no iso or import"},
		"nebla_iso unknown storage": {func(m map[string]any) { m["nebla_iso"] = []any{"ghost"} }, "unknown storage"},
		"invalid online flag":       {func(m map[string]any) { node0(m)["online"] = "yes" }, "invalid flag"},
		"invalid shared flag":       {func(m map[string]any) { stor0(m)["shared"] = 2 }, "invalid flag"},
		"513 storages": {func(m map[string]any) {
			var st []any
			for i := 0; i < 513; i++ {
				st = append(st, map[string]any{"storage": "s" + strings.Repeat("a", i/26) + string(rune('a'+i%26)), "type": "dir", "content": "iso"})
			}
			m["storage"], m["nebla_iso"] = st, []any{}
		}, "too many storages"},
		"oversize": {func(m map[string]any) { m["pad"] = strings.Repeat("a", maxReportBytes) }, "too large"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			m := baseReport()
			c.mut(m)
			raw, err := json.Marshal(m)
			require.NoError(t, err)
			_, err = ParseReport(raw)
			var e apierr.E
			require.True(t, errors.As(err, &e), "got %v", err)
			require.Equal(t, "invalid_report", e.Code)
			require.Equal(t, 400, e.Status)
			require.Contains(t, e.Msg, c.want)
			require.NotContains(t, e.Msg, "3f2b8c1e")
		})
	}
	_, err := ParseReport([]byte("{not json"))
	var e apierr.E
	require.True(t, errors.As(err, &e))
	require.Equal(t, "invalid_report", e.Code)
	require.Contains(t, e.Msg, "not valid JSON")
}

func addNode(m map[string]any, name, ip string, online int) {
	n := map[string]any{"type": "node", "name": name, "online": online}
	if ip != "" {
		n["ip"] = ip
	}
	m["cluster"] = append(m["cluster"].([]any), n)
	m["fingerprints"].(map[string]any)[name] = fp("BB")
}

func TestParseReportOfflineMemberWithoutIP(t *testing.T) {
	m := baseReport()
	addNode(m, "pve2", "", 0)
	addNode(m, "pve3", "", 0)
	m["cluster"].([]any)[2].(map[string]any)["ip"] = ""
	raw, err := json.Marshal(m)
	require.NoError(t, err)
	r, err := ParseReport(raw)
	require.NoError(t, err)
	require.Equal(t, []ReportNode{
		{Name: "pve1", IP: "10.0.0.1", Online: true, Fingerprint: strings.Repeat("AA", 32)},
		{Name: "pve2", Fingerprint: strings.Repeat("BB", 32)},
		{Name: "pve3", Fingerprint: strings.Repeat("BB", 32)},
	}, r.Nodes)
}

func TestReportRedactsSecret(t *testing.T) {
	raw, _ := json.Marshal(baseReport())
	r, err := ParseReport(raw)
	require.NoError(t, err)
	secret := "3f2b8c1e-5d4a-4e7b-9a10-0c6d2e8f7a11"
	require.Equal(t, secret, r.TokenSecret)
	for _, out := range []string{fmt.Sprintf("%v", r), fmt.Sprintf("%+v", r), fmt.Sprintf("%#v", r), fmt.Sprint(&r)} {
		require.NotContains(t, out, secret)
	}
}

func TestParseReportAcceptsBaseline(t *testing.T) {
	raw, _ := json.Marshal(baseReport())
	r, err := ParseReport(raw)
	require.NoError(t, err)
	require.True(t, r.Nodes[0].Online)
	require.True(t, r.Storages[0].NeblaISO)
}

func TestBlobRoundTrip(t *testing.T) {
	raw, err := os.ReadFile("testdata/report-pve9.json")
	require.NoError(t, err)
	tok := "nbe_" + strings.Repeat("A", 43)
	line, err := EncodeBlob(tok, raw)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(line, "nebla-enroll:v1:"))
	got, body, err := DecodeBlob("  " + line + "\n")
	require.NoError(t, err)
	require.Equal(t, tok, got)
	require.NotContains(t, string(body), "enroll_token")
	r, err := ParseReport(body)
	require.NoError(t, err)
	require.Equal(t, "pve1", r.Node)
}

func TestDecodeBlobWrapped(t *testing.T) {
	raw, err := os.ReadFile("testdata/report-pve9.json")
	require.NoError(t, err)
	tok := "nbe_" + strings.Repeat("B", 43)
	line, err := EncodeBlob(tok, raw)
	require.NoError(t, err)
	var b strings.Builder
	for i := 0; i < len(line); i += 80 {
		b.WriteString(line[i:min(i+80, len(line))])
		b.WriteString([]string{"\n", "\r\n", " \n\t", "\n  "}[(i/80)%4])
	}
	require.Greater(t, strings.Count(b.String(), "\n"), 3)
	got, body, err := DecodeBlob(b.String())
	require.NoError(t, err)
	require.True(t, got == tok, "token mismatch")
	r, err := ParseReport(body)
	require.NoError(t, err)
	require.Equal(t, "pve1", r.Node)
}

func TestEncodeBlob(t *testing.T) {
	line, err := EncodeBlob("nbe_new", []byte(`{"v":1,"enroll_token":"nbe_old"}`))
	require.NoError(t, err)
	tok, body, err := DecodeBlob(line)
	require.NoError(t, err)
	require.Equal(t, "nbe_new", tok)
	require.JSONEq(t, `{"v":1}`, string(body))
	for _, in := range []string{`[1]`, `null`, `nope`, ``} {
		_, err := EncodeBlob("nbe_x", []byte(in))
		require.Error(t, err, in)
	}
}

func TestDecodeBlobRejects(t *testing.T) {
	good, err := EncodeBlob("nbe_x", []byte(`{"v":1}`))
	require.NoError(t, err)
	noTok, err := EncodeBlob("", []byte(`{"v":1}`))
	require.NoError(t, err)
	enc := func(s string) string { return blobPrefix + base64.RawURLEncoding.EncodeToString([]byte(s)) }
	big := enc(`{"enroll_token":"nbe_x","pad":"` + strings.Repeat("a", maxReportBytes) + `"}`)
	for name, c := range map[string]struct{ line, want string }{
		"no token key": {enc(`{"v":1}`), "no enrollment token"},
		"oversize":     {big, "too large"},
		"wrong prefix": {"nebla-enroll:v2:" + strings.TrimPrefix(good, blobPrefix), "not an enrollment blob"},
		"bad base64":   {blobPrefix + "!!!", "not valid base64"},
		"no token":     {noTok, "no enrollment token"},
		"not json":     {blobPrefix + "bm90IGpzb24", "does not contain a JSON report"},
		"empty":        {"", "not an enrollment blob"},
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := DecodeBlob(c.line)
			var e apierr.E
			require.True(t, errors.As(err, &e), "got %v", err)
			require.Equal(t, "invalid_report", e.Code)
			require.Contains(t, e.Msg, c.want)
		})
	}
}

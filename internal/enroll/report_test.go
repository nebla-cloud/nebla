package enroll

import (
	"encoding/json"
	"errors"
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
	cases := map[string]func(m map[string]any){
		"v2":            func(m map[string]any) { m["v"] = 2 },
		"version 7.4-1": func(m map[string]any) { m["pve_version"] = "7.4-1" },
		"version junk":  func(m map[string]any) { m["pve_version"] = "x" },
		"non-uuid secret": func(m map[string]any) {
			m["token"].(map[string]any)["value"] = "NOT-A-UUID"
		},
		"uppercase uuid": func(m map[string]any) {
			m["token"].(map[string]any)["value"] = "3F2B8C1E-5D4A-4E7B-9A10-0C6D2E8F7A11"
		},
		"wrong tokenid prefix": func(m map[string]any) {
			m["token"].(map[string]any)["full-tokenid"] = "root@pam!nebla-home-ab12cd"
		},
		"bad tokenid suffix": func(m map[string]any) {
			m["token"].(map[string]any)["full-tokenid"] = "nebla@pve!nebla-home-AB12CD"
		},
		"no nodes": func(m map[string]any) { m["cluster"] = []any{map[string]any{"type": "cluster", "name": "c"}} },
		"65 nodes": func(m map[string]any) {
			cl, f := many(65)
			m["cluster"], m["fingerprints"], m["node"] = cl, f, "naa"
		},
		"bad node name": func(m map[string]any) {
			m["cluster"].([]any)[0].(map[string]any)["name"] = "pve_1"
		},
		"missing fingerprint": func(m map[string]any) { m["fingerprints"] = map[string]any{} },
		"short fingerprint":   func(m map[string]any) { m["fingerprints"] = map[string]any{"pve1": "AA:BB"} },
		"non-hex fingerprint": func(m map[string]any) { m["fingerprints"] = map[string]any{"pve1": strings.Repeat("ZZ", 32)} },
		"node not in nodes":   func(m map[string]any) { m["node"] = "other" },
		"quote in storage id": func(m map[string]any) {
			m["storage"].([]any)[0].(map[string]any)["storage"] = `lo"cal`
			m["nebla_iso"] = []any{}
		},
		"newline in storage id": func(m map[string]any) {
			m["storage"].([]any)[0].(map[string]any)["storage"] = "lo\ncal"
			m["nebla_iso"] = []any{}
		},
		"nebla_iso without iso content": func(m map[string]any) {
			m["storage"].([]any)[0].(map[string]any)["content"] = "images,backup"
		},
		"nebla_iso unknown storage": func(m map[string]any) { m["nebla_iso"] = []any{"ghost"} },
		"513 storages": func(m map[string]any) {
			var st []any
			for i := 0; i < 513; i++ {
				st = append(st, map[string]any{"storage": "s" + strings.Repeat("a", i/26) + string(rune('a'+i%26)), "type": "dir", "content": "iso"})
			}
			m["storage"], m["nebla_iso"] = st, []any{}
		},
	}
	for name, mut := range cases {
		t.Run(name, func(t *testing.T) {
			m := baseReport()
			mut(m)
			raw, err := json.Marshal(m)
			require.NoError(t, err)
			_, err = ParseReport(raw)
			var e apierr.E
			require.True(t, errors.As(err, &e), "got %v", err)
			require.Equal(t, "invalid_report", e.Code)
			require.Equal(t, 400, e.Status)
			require.NotContains(t, e.Msg, "3f2b8c1e")
		})
	}
	_, err := ParseReport([]byte("{not json"))
	var e apierr.E
	require.True(t, errors.As(err, &e))
	require.Equal(t, "invalid_report", e.Code)
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
	line := EncodeBlob(tok, raw)
	require.True(t, strings.HasPrefix(line, "nebla-enroll:v1:"))
	got, body, err := DecodeBlob("  " + line + "\n")
	require.NoError(t, err)
	require.Equal(t, tok, got)
	require.NotContains(t, string(body), "enroll_token")
	r, err := ParseReport(body)
	require.NoError(t, err)
	require.Equal(t, "pve1", r.Node)
}

func TestDecodeBlobRejects(t *testing.T) {
	good := EncodeBlob("nbe_x", []byte(`{"v":1}`))
	noTok := EncodeBlob("", []byte(`{"v":1}`))
	for name, line := range map[string]string{
		"wrong prefix": "nebla-enroll:v2:" + strings.TrimPrefix(good, "nebla-enroll:v1:"),
		"bad base64":   "nebla-enroll:v1:!!!",
		"no token":     noTok,
		"not json":     "nebla-enroll:v1:" + "bm90IGpzb24",
		"empty":        "",
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := DecodeBlob(line)
			var e apierr.E
			require.True(t, errors.As(err, &e), "got %v", err)
			require.Equal(t, "invalid_report", e.Code)
		})
	}
}

// Package enroll parses and validates location enrollment reports.
package enroll

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/nebla-cloud/nebla/internal/apierr"
	"github.com/nebla-cloud/nebla/internal/pve"
)

const blobPrefix = "nebla-enroll:v1:"

const (
	maxNodes    = 64
	maxStorages = 512
)

type Report struct {
	PVEVersion  string          `json:"pve_version"`
	Node        string          `json:"node"`
	TokenID     string          `json:"token_id"`
	TokenSecret string          `json:"-"`
	Nodes       []ReportNode    `json:"nodes"`
	Storages    []ReportStorage `json:"storages"`
}

type ReportNode struct {
	Name        string `json:"name"`
	IP          string `json:"ip"`
	Online      bool   `json:"online"`
	Fingerprint string `json:"fingerprint"`
}

type ReportStorage struct {
	ID       string   `json:"id"`
	Type     string   `json:"type"`
	Content  []string `json:"content"`
	Shared   bool     `json:"shared"`
	NeblaISO bool     `json:"nebla_iso"`
}

var (
	uuidRe    = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	tokenIDRe = regexp.MustCompile(`^nebla@pve!nebla-[a-z0-9-]{1,32}-[a-z0-9]{6}$`)
	nodeRe    = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`)
	storageRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9._-]{0,99}$`)
	fpRe      = regexp.MustCompile(`^[0-9A-F]{64}$`)
)

func bad(msg string) error { return apierr.New(http.StatusBadRequest, "invalid_report", msg) }

// flag accepts 0/1 or a bool; absent is false.
type flag bool

func (f *flag) UnmarshalJSON(b []byte) error {
	switch string(b) {
	case "1", "true":
		*f = true
	case "0", "false", "null":
		*f = false
	default:
		return bad("invalid flag value")
	}
	return nil
}

type wire struct {
	V          int    `json:"v"`
	PVEVersion string `json:"pve_version"`
	Node       string `json:"node"`
	Token      struct {
		FullTokenID string `json:"full-tokenid"`
		Value       string `json:"value"`
	} `json:"token"`
	Cluster []struct {
		Type   string `json:"type"`
		Name   string `json:"name"`
		IP     string `json:"ip"`
		Online flag   `json:"online"`
	} `json:"cluster"`
	Storage []struct {
		Storage string `json:"storage"`
		Type    string `json:"type"`
		Content string `json:"content"`
		Shared  flag   `json:"shared"`
	} `json:"storage"`
	Fingerprints map[string]string `json:"fingerprints"`
	NeblaISO     []string          `json:"nebla_iso"`
}

// ParseReport validates the script's JSON and returns the typed report.
func ParseReport(raw []byte) (Report, error) {
	var w wire
	if err := json.Unmarshal(raw, &w); err != nil {
		if e, ok := err.(apierr.E); ok {
			return Report{}, e
		}
		return Report{}, bad("report is not valid JSON")
	}
	if w.V != 1 {
		return Report{}, bad("unsupported report version")
	}
	major, _, _ := strings.Cut(w.PVEVersion, ".")
	if n, err := strconv.Atoi(major); err != nil || (n != 8 && n != 9) {
		return Report{}, bad("unsupported Proxmox VE version")
	}
	if !uuidRe.MatchString(w.Token.Value) {
		return Report{}, bad("invalid token secret")
	}
	if !tokenIDRe.MatchString(w.Token.FullTokenID) {
		return Report{}, bad("invalid token id")
	}
	r := Report{PVEVersion: w.PVEVersion, Node: w.Node, TokenID: w.Token.FullTokenID, TokenSecret: w.Token.Value}
	names := map[string]bool{}
	for _, c := range w.Cluster {
		if c.Type != "node" {
			continue
		}
		if len(r.Nodes) >= maxNodes {
			return Report{}, bad("too many nodes")
		}
		if !nodeRe.MatchString(c.Name) {
			return Report{}, bad("invalid node name")
		}
		f := pve.NormFP(w.Fingerprints[c.Name])
		if !fpRe.MatchString(f) {
			return Report{}, bad("missing or invalid fingerprint for node " + c.Name)
		}
		names[c.Name] = true
		r.Nodes = append(r.Nodes, ReportNode{Name: c.Name, IP: c.IP, Online: bool(c.Online), Fingerprint: f})
	}
	if len(r.Nodes) == 0 {
		return Report{}, bad("no nodes in report")
	}
	if !names[w.Node] {
		return Report{}, bad("reporting node is not in the cluster")
	}
	if len(w.Storage) > maxStorages {
		return Report{}, bad("too many storages")
	}
	iso := map[string]bool{}
	for _, id := range w.NeblaISO {
		iso[id] = true
	}
	seen := map[string]bool{}
	for _, s := range w.Storage {
		if !storageRe.MatchString(s.Storage) {
			return Report{}, bad("invalid storage id")
		}
		var content []string
		if s.Content != "" {
			content = strings.Split(s.Content, ",")
		}
		st := ReportStorage{ID: s.Storage, Type: s.Type, Content: content, Shared: bool(s.Shared), NeblaISO: iso[s.Storage]}
		if st.NeblaISO {
			ok := false
			for _, c := range content {
				ok = ok || c == "iso" || c == "import"
			}
			if !ok {
				return Report{}, bad("nebla_iso storage " + s.Storage + " has no iso or import content")
			}
		}
		seen[s.Storage] = true
		r.Storages = append(r.Storages, st)
	}
	for id := range iso {
		if !seen[id] {
			return Report{}, bad("nebla_iso names an unknown storage")
		}
	}
	return r, nil
}

// DecodeBlob unpacks a pasted blob into its enrollment token and the report JSON.
func DecodeBlob(line string) (string, []byte, error) {
	line = strings.TrimSpace(line)
	enc, ok := strings.CutPrefix(line, blobPrefix)
	if !ok {
		return "", nil, bad("not an enrollment blob")
	}
	body, err := base64.RawURLEncoding.DecodeString(enc)
	if err != nil {
		return "", nil, bad("blob is not valid base64")
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return "", nil, bad("blob does not contain a JSON report")
	}
	var tok string
	if err := json.Unmarshal(m["enroll_token"], &tok); err != nil || tok == "" {
		return "", nil, bad("blob has no enrollment token")
	}
	delete(m, "enroll_token")
	out, err := json.Marshal(m)
	if err != nil {
		return "", nil, bad("blob does not contain a JSON report")
	}
	return tok, out, nil
}

// EncodeBlob builds the paste-back line from a token and report JSON.
func EncodeBlob(token string, raw []byte) string {
	m := map[string]json.RawMessage{}
	_ = json.Unmarshal(raw, &m)
	t, _ := json.Marshal(token)
	m["enroll_token"] = t
	b, _ := json.Marshal(m)
	return blobPrefix + base64.RawURLEncoding.EncodeToString(b)
}

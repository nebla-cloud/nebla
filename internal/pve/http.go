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

package pve

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"mime/multipart"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

const maxPollErrors = 3

const apiPrefix = "/api2/json"

type httpClient struct {
	conn Conn
	hc   *http.Client
	poll time.Duration
}

// TLSConfig builds the TLS settings for c: pinned fingerprint, insecure, or system roots.
func TLSConfig(c Conn) *tls.Config {
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	switch {
	case c.InsecureTLS:
		cfg.InsecureSkipVerify = true
	case c.CAFingerprint != "":
		want := NormFP(c.CAFingerprint)
		cfg.InsecureSkipVerify = true
		cfg.VerifyPeerCertificate = func(raw [][]byte, _ [][]*x509.Certificate) error {
			if len(raw) == 0 {
				return errors.New("pve: no peer certificate")
			}
			sum := sha256.Sum256(raw[0])
			if fmt.Sprintf("%X", sum[:]) != want {
				return ErrFingerprint
			}
			return nil
		}
	}
	return cfg
}

// NormFP upper-cases a certificate fingerprint and strips colons and spaces.
func NormFP(s string) string {
	return strings.ToUpper(strings.NewReplacer(":", "", " ", "").Replace(s))
}

// New returns a Client for c.
func New(c Conn) Client {
	c.BaseURL = strings.TrimRight(c.BaseURL, "/")
	tr := &http.Transport{
		TLSClientConfig:       TLSConfig(c),
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
		MaxIdleConnsPerHost:   4,
	}
	return &httpClient{conn: c, hc: &http.Client{Transport: tr}, poll: cmp.Or(c.PollInterval, 500*time.Millisecond)}
}

func (c *httpClient) AuthHeader() http.Header {
	h := http.Header{}
	h.Set("Authorization", "PVEAPIToken="+c.conn.TokenID+"="+c.conn.Secret)
	return h
}

// num decodes a JSON number that Proxmox may send as a string.
type num float64

func (n *num) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*n = 0
		return nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return err
	}
	*n = num(f)
	return nil
}

// optNum is a num where null, empty, NaN and Inf decode as nil.
type optNum struct{ v *float64 }

func (n *optNum) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		n.v = nil
		return nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return err
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		n.v = nil
		return nil
	}
	n.v = &f
	return nil
}

func (c *httpClient) do(ctx context.Context, method, path string, form url.Values, out any) error {
	var body io.Reader
	target := c.conn.BaseURL + apiPrefix + path
	if method == http.MethodGet || method == http.MethodDelete {
		if len(form) > 0 {
			target += "?" + form.Encode()
		}
	} else {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	return c.send(req, out)
}

func (c *httpClient) send(req *http.Request, out any) error {
	req.Header.Set("Authorization", c.AuthHeader().Get("Authorization"))
	res, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}
	var env struct {
		Data    json.RawMessage `json:"data"`
		Message string          `json:"message"`
	}
	_ = json.Unmarshal(raw, &env)
	if res.StatusCode < 200 || res.StatusCode > 299 {
		msg := env.Message
		if msg == "" {
			msg = strings.TrimSpace(strings.TrimPrefix(res.Status, strconv.Itoa(res.StatusCode)))
		}
		if msg == "" {
			msg = strings.TrimSpace(string(raw))
		}
		return &Error{Status: res.StatusCode, Message: msg}
	}
	if out == nil || len(env.Data) == 0 || string(env.Data) == "null" {
		return nil
	}
	return json.Unmarshal(env.Data, out)
}

func (c *httpClient) Version(ctx context.Context) (string, error) {
	var v struct {
		Version string `json:"version"`
	}
	err := c.do(ctx, http.MethodGet, "/version", nil, &v)
	return v.Version, err
}

func (c *httpClient) NodeVersion(ctx context.Context, node string) (string, error) {
	var v struct {
		Version string `json:"version"`
	}
	err := c.do(ctx, http.MethodGet, "/nodes/"+url.PathEscape(node)+"/version", nil, &v)
	return v.Version, err
}

func (c *httpClient) Nodes(ctx context.Context) ([]Node, error) {
	var raw []struct {
		Node   string `json:"node"`
		Status string `json:"status"`
		MaxCPU num    `json:"maxcpu"`
		MaxMem num    `json:"maxmem"`
		CPU    num    `json:"cpu"`
		Mem    num    `json:"mem"`
	}
	if err := c.do(ctx, http.MethodGet, "/nodes", nil, &raw); err != nil {
		return nil, err
	}
	out := make([]Node, len(raw))
	for i, r := range raw {
		out[i] = Node{Name: r.Node, Status: r.Status, MaxCPU: int(r.MaxCPU), MaxMem: int64(r.MaxMem), CPU: float64(r.CPU), Mem: int64(r.Mem)}
	}
	return out, nil
}

func (c *httpClient) Storages(ctx context.Context, node string) ([]Storage, error) {
	var raw []struct {
		Storage string `json:"storage"`
		Type    string `json:"type"`
		Content string `json:"content"`
		Shared  num    `json:"shared"`
		Avail   num    `json:"avail"`
	}
	if err := c.do(ctx, http.MethodGet, "/nodes/"+url.PathEscape(node)+"/storage", nil, &raw); err != nil {
		return nil, err
	}
	out := make([]Storage, len(raw))
	for i, r := range raw {
		var content []string
		if r.Content != "" {
			content = strings.Split(r.Content, ",")
		}
		out[i] = Storage{ID: r.Storage, Type: r.Type, Content: content, Shared: r.Shared != 0, Avail: int64(r.Avail)}
	}
	return out, nil
}

func (c *httpClient) Permissions(ctx context.Context, path string) (map[string]map[string]int, error) {
	var raw map[string]map[string]num
	if err := c.do(ctx, http.MethodGet, "/access/permissions", url.Values{"path": {path}}, &raw); err != nil {
		return nil, err
	}
	out := make(map[string]map[string]int, len(raw))
	for p, privs := range raw {
		out[p] = make(map[string]int, len(privs))
		for k, v := range privs {
			out[p][k] = int(v)
		}
	}
	return out, nil
}

func (c *httpClient) VMs(ctx context.Context) ([]VM, error) {
	var raw []struct {
		Type     string `json:"type"`
		VMID     num    `json:"vmid"`
		Node     string `json:"node"`
		Name     string `json:"name"`
		Status   string `json:"status"`
		Tags     string `json:"tags"`
		Template num    `json:"template"`
	}
	if err := c.do(ctx, http.MethodGet, "/cluster/resources", url.Values{"type": {"vm"}}, &raw); err != nil {
		return nil, err
	}
	var out []VM
	for _, r := range raw {
		if r.Type != "" && r.Type != "qemu" {
			continue
		}
		out = append(out, VM{VMID: int(r.VMID), Node: r.Node, Name: r.Name, Status: r.Status, Tags: r.Tags, Template: r.Template != 0})
	}
	return out, nil
}

func (c *httpClient) GuestExists(ctx context.Context, vmid int) (bool, error) {
	var raw []struct {
		VMID num `json:"vmid"`
	}
	if err := c.do(ctx, http.MethodGet, "/cluster/resources", url.Values{"type": {"vm"}}, &raw); err != nil {
		return false, err
	}
	for _, r := range raw {
		if int(r.VMID) == vmid {
			return true, nil
		}
	}
	return false, nil
}

func (c *httpClient) NextID(ctx context.Context) (int, error) {
	var n num
	if err := c.do(ctx, http.MethodGet, "/cluster/nextid", nil, &n); err != nil {
		return 0, err
	}
	return int(n), nil
}

func vmPath(node string, vmid int) string {
	return "/nodes/" + url.PathEscape(node) + "/qemu/" + strconv.Itoa(vmid)
}

func (c *httpClient) Clone(ctx context.Context, node string, tmpl int, p CloneParams) (UPID, error) {
	f := url.Values{"newid": {strconv.Itoa(p.NewID)}}
	if p.Name != "" {
		f.Set("name", p.Name)
	}
	if p.Target != "" {
		f.Set("target", p.Target)
	}
	if p.Storage != "" {
		f.Set("storage", p.Storage)
	}
	if p.Full {
		f.Set("full", "1")
	}
	if p.Description != "" {
		f.Set("description", p.Description)
	}
	return c.task(ctx, http.MethodPost, vmPath(node, tmpl)+"/clone", f)
}

func (c *httpClient) DownloadURL(ctx context.Context, node, storage string, p DownloadParams) (UPID, error) {
	f := url.Values{"url": {p.URL}, "filename": {p.Filename}, "content": {p.Content}}
	if p.Checksum != "" {
		f.Set("checksum", p.Checksum)
		f.Set("checksum-algorithm", p.Algorithm)
	}
	return c.task(ctx, http.MethodPost, "/nodes/"+url.PathEscape(node)+"/storage/"+url.PathEscape(storage)+"/download-url", f)
}

func (c *httpClient) CreateVM(ctx context.Context, node string, vmid int, kv map[string]string) (UPID, error) {
	f := url.Values{}
	for k, v := range kv {
		f.Set(k, v)
	}
	f.Set("vmid", strconv.Itoa(vmid))
	return c.task(ctx, http.MethodPost, "/nodes/"+url.PathEscape(node)+"/qemu", f)
}

func (c *httpClient) ConvertToTemplate(ctx context.Context, node string, vmid int) (UPID, error) {
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodPost, vmPath(node, vmid)+"/template", nil, &raw); err != nil {
		return "", err
	}
	var up string
	if json.Unmarshal(raw, &up) != nil || !strings.HasPrefix(up, "UPID:") {
		return "", nil
	}
	return UPID(up), nil
}

func (c *httpClient) task(ctx context.Context, method, path string, f url.Values) (UPID, error) {
	var up string
	if err := c.do(ctx, method, path, f, &up); err != nil {
		return "", err
	}
	return UPID(up), nil
}

func (c *httpClient) WaitTask(ctx context.Context, node string, upid UPID) error {
	path := "/nodes/" + url.PathEscape(node) + "/tasks/" + url.PathEscape(string(upid)) + "/status"
	fails := 0
	for {
		var st struct {
			Status     string `json:"status"`
			ExitStatus string `json:"exitstatus"`
		}
		if err := c.do(ctx, http.MethodGet, path, nil, &st); err != nil {
			if fails++; fails > maxPollErrors || !IsTransient(err) {
				return err
			}
		} else {
			fails = 0
		}
		if st.Status == "stopped" {
			if st.ExitStatus == "OK" || strings.HasPrefix(st.ExitStatus, "WARNINGS") {
				return nil
			}
			return &TaskError{UPID: upid, ExitStatus: st.ExitStatus}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(c.poll):
		}
	}
}

func (c *httpClient) Config(ctx context.Context, node string, vmid int) (map[string]string, error) {
	return c.getMap(ctx, vmPath(node, vmid)+"/config")
}

func (c *httpClient) FirewallOptions(ctx context.Context, node string, vmid int) (map[string]string, error) {
	return c.getMap(ctx, vmPath(node, vmid)+"/firewall/options")
}

// getMap reads a flat object and renders every value as Proxmox's string form.
func (c *httpClient) getMap(ctx context.Context, path string) (map[string]string, error) {
	var raw map[string]any
	if err := c.do(ctx, http.MethodGet, path, nil, &raw); err != nil {
		return nil, err
	}
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		switch t := v.(type) {
		case string:
			out[k] = t
		case float64:
			out[k] = strconv.FormatFloat(t, 'f', -1, 64)
		case bool:
			out[k] = map[bool]string{true: "1", false: "0"}[t]
		default:
			out[k] = fmt.Sprint(t)
		}
	}
	return out, nil
}

func (c *httpClient) SetConfig(ctx context.Context, node string, vmid int, kv map[string]string) error {
	return c.SetConfigDigest(ctx, node, vmid, kv, "")
}

func (c *httpClient) SetConfigDigest(ctx context.Context, node string, vmid int, kv map[string]string, digest string) error {
	f := url.Values{}
	for k, v := range kv {
		f.Set(k, v)
	}
	if digest != "" {
		f.Set("digest", digest)
	}
	return c.do(ctx, http.MethodPut, vmPath(node, vmid)+"/config", f, nil)
}

func (c *httpClient) Pending(ctx context.Context, node string, vmid int) ([]PendingItem, error) {
	return c.pendingAt(ctx, vmPath(node, vmid))
}

func (c *httpClient) pendingAt(ctx context.Context, base string) ([]PendingItem, error) {
	var raw []struct {
		Key     string `json:"key"`
		Value   any    `json:"value"`
		Pending any    `json:"pending"`
		Delete  num    `json:"delete"`
	}
	if err := c.do(ctx, http.MethodGet, base+"/pending", nil, &raw); err != nil {
		return nil, err
	}
	out := make([]PendingItem, len(raw))
	for i, r := range raw {
		out[i] = PendingItem{Key: r.Key, Value: anyString(r.Value), Pending: anyString(r.Pending), Delete: int(r.Delete)}
	}
	return out, nil
}

// anyString renders a decoded JSON scalar as Proxmox's string form; nil is "".
func anyString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		return map[bool]string{true: "1", false: "0"}[t]
	}
	return fmt.Sprint(v)
}

func contentPath(node, storage string) string {
	return "/nodes/" + url.PathEscape(node) + "/storage/" + url.PathEscape(storage) + "/content"
}

func (c *httpClient) AllocVolume(ctx context.Context, node, storage string, ownerVMID int, filename string, sizeGB int) (string, error) {
	f := url.Values{"vmid": {strconv.Itoa(ownerVMID)}, "filename": {filename}, "size": {strconv.Itoa(sizeGB) + "G"}}
	var volid string
	if err := c.do(ctx, http.MethodPost, contentPath(node, storage), f, &volid); err != nil {
		return "", err
	}
	return volid, nil
}

func (c *httpClient) StorageContent(ctx context.Context, node, storage string, vmid int) ([]StorageItem, error) {
	var f url.Values
	if vmid > 0 {
		f = url.Values{"vmid": {strconv.Itoa(vmid)}}
	}
	var raw []struct {
		Volid string `json:"volid"`
		Size  num    `json:"size"`
		VMID  num    `json:"vmid"`
	}
	if err := c.do(ctx, http.MethodGet, contentPath(node, storage), f, &raw); err != nil {
		return nil, err
	}
	out := make([]StorageItem, len(raw))
	for i, r := range raw {
		out[i] = StorageItem{Volid: r.Volid, Size: int64(r.Size), VMID: int(r.VMID)}
	}
	return out, nil
}

func (c *httpClient) Vzdump(ctx context.Context, node string, vmid int, storage, notes string) (UPID, error) {
	f := url.Values{"vmid": {strconv.Itoa(vmid)}, "storage": {storage}, "mode": {"snapshot"}, "remove": {"0"}, "notes-template": {notes}}
	return c.task(ctx, http.MethodPost, "/nodes/"+url.PathEscape(node)+"/vzdump", f)
}

func (c *httpClient) BackupContent(ctx context.Context, node, storage string, vmid int) ([]BackupItem, error) {
	f := url.Values{"content": {"backup"}}
	if vmid > 0 {
		f.Set("vmid", strconv.Itoa(vmid))
	}
	var raw []struct {
		Volid string `json:"volid"`
		VMID  num    `json:"vmid"`
		Size  num    `json:"size"`
		Ctime num    `json:"ctime"`
		Notes string `json:"notes"`
	}
	if err := c.do(ctx, http.MethodGet, contentPath(node, storage), f, &raw); err != nil {
		return nil, err
	}
	out := make([]BackupItem, len(raw))
	for i, r := range raw {
		out[i] = BackupItem{Volid: r.Volid, VMID: int(r.VMID), Size: int64(r.Size), Ctime: time.Unix(int64(r.Ctime), 0).UTC(), Notes: r.Notes}
	}
	return out, nil
}

func (c *httpClient) RestoreVM(ctx context.Context, node string, vmid int, archive, storage string) (UPID, error) {
	f := url.Values{"vmid": {strconv.Itoa(vmid)}, "archive": {archive}, "storage": {storage}, "unique": {"1"}}
	return c.task(ctx, http.MethodPost, "/nodes/"+url.PathEscape(node)+"/qemu", f)
}

// ResizeDisk returns once the disk has grown; PVE 8+ may answer with a task to wait for.
func (c *httpClient) ResizeDisk(ctx context.Context, node string, vmid int, disk string, sizeGB int) error {
	f := url.Values{"disk": {disk}, "size": {strconv.Itoa(sizeGB) + "G"}}
	var up string
	if err := c.do(ctx, http.MethodPut, vmPath(node, vmid)+"/resize", f, &up); err != nil {
		return err
	}
	return c.settle(ctx, node, up)
}

func (c *httpClient) SetFirewallOptions(ctx context.Context, node string, vmid int, kv map[string]string) error {
	f := url.Values{}
	for k, v := range kv {
		f.Set(k, v)
	}
	return c.do(ctx, http.MethodPut, vmPath(node, vmid)+"/firewall/options", f, nil)
}

func (c *httpClient) FirewallRules(ctx context.Context, node string, vmid int) ([]FirewallRule, error) {
	var raw []struct {
		Pos     int    `json:"pos"`
		Type    string `json:"type"`
		Action  string `json:"action"`
		Iface   string `json:"iface"`
		Proto   string `json:"proto"`
		Dport   string `json:"dport"`
		Source  string `json:"source"`
		Dest    string `json:"dest"`
		Comment string `json:"comment"`
		Enable  int    `json:"enable"`
		Digest  string `json:"digest"`
	}
	if err := c.do(ctx, http.MethodGet, vmPath(node, vmid)+"/firewall/rules", nil, &raw); err != nil {
		return nil, err
	}
	out := make([]FirewallRule, len(raw))
	for i, r := range raw {
		out[i] = FirewallRule{Pos: r.Pos, Type: r.Type, Action: r.Action, Iface: r.Iface, Proto: r.Proto, Dport: r.Dport,
			Source: r.Source, Dest: r.Dest, Comment: r.Comment, Enable: r.Enable == 1, Digest: r.Digest}
	}
	return out, nil
}

func (c *httpClient) AddFirewallRule(ctx context.Context, node string, vmid int, r FirewallRule) error {
	f := url.Values{"type": {r.Type}, "action": {r.Action}, "enable": {"0"}}
	if r.Enable {
		f.Set("enable", "1")
	}
	for k, v := range map[string]string{"iface": r.Iface, "proto": r.Proto, "dport": r.Dport, "source": r.Source, "dest": r.Dest, "comment": r.Comment} {
		if v != "" {
			f.Set(k, v)
		}
	}
	return c.do(ctx, http.MethodPost, vmPath(node, vmid)+"/firewall/rules", f, nil)
}

func (c *httpClient) DeleteFirewallRule(ctx context.Context, node string, vmid, pos int, digest string) error {
	var f url.Values
	if digest != "" {
		f = url.Values{"digest": {digest}}
	}
	return c.do(ctx, http.MethodDelete, vmPath(node, vmid)+"/firewall/rules/"+strconv.Itoa(pos), f, nil)
}

func (c *httpClient) ClusterFirewallEnabled(ctx context.Context) (bool, error) {
	o, err := c.getMap(ctx, "/cluster/firewall/options")
	if err != nil {
		return false, err
	}
	return o["enable"] == "1", nil
}

// IPFilterSet is the ipset Proxmox's ipfilter checks net0 traffic against.
const IPFilterSet = "ipfilter-net0"

func (c *httpClient) ipsetExists(ctx context.Context, base, name string) (bool, error) {
	var sets []struct {
		Name string `json:"name"`
	}
	if err := c.do(ctx, http.MethodGet, base+"/ipset", nil, &sets); err != nil {
		return false, err
	}
	return slices.ContainsFunc(sets, func(s struct {
		Name string `json:"name"`
	}) bool {
		return s.Name == name
	}), nil
}

func (c *httpClient) ipsetEntries(ctx context.Context, base, name string) ([]string, error) {
	var have []struct {
		CIDR string `json:"cidr"`
	}
	if err := c.do(ctx, http.MethodGet, base+"/ipset/"+url.PathEscape(name), nil, &have); err != nil {
		return nil, err
	}
	out := make([]string, len(have))
	for i, h := range have {
		out[i] = h.CIDR
	}
	return out, nil
}

func (c *httpClient) FirewallIPSet(ctx context.Context, node string, vmid int) ([]string, bool, error) {
	return c.IPSet(ctx, node, vmid, IPFilterSet)
}

func (c *httpClient) IPSet(ctx context.Context, node string, vmid int, name string) ([]string, bool, error) {
	base := vmPath(node, vmid) + "/firewall"
	ok, err := c.ipsetExists(ctx, base, name)
	if err != nil || !ok {
		return nil, false, err
	}
	cidrs, err := c.ipsetEntries(ctx, base, name)
	return cidrs, err == nil, err
}

func (c *httpClient) SetFirewall(ctx context.Context, node string, vmid int, ips []netip.Addr) error {
	// Policies are the renderer's to write, not this call.
	opts := url.Values{"enable": {"1"}, "ipfilter": {"1"}, "macfilter": {"1"}}
	if err := c.do(ctx, http.MethodPut, vmPath(node, vmid)+"/firewall/options", opts, nil); err != nil {
		return err
	}
	return c.SetIPSet(ctx, node, vmid, IPFilterSet, ips)
}

func (c *httpClient) SetIPSet(ctx context.Context, node string, vmid int, name string, ips []netip.Addr) error {
	base := vmPath(node, vmid) + "/firewall"
	ok, err := c.ipsetExists(ctx, base, name)
	if err != nil {
		return err
	}
	if !ok {
		if err := c.do(ctx, http.MethodPost, base+"/ipset", url.Values{"name": {name}}, nil); err != nil {
			return err
		}
	}
	have, err := c.ipsetEntries(ctx, base, name)
	if err != nil {
		return err
	}
	want := map[string]bool{}
	for _, ip := range ips {
		want[ip.String()] = true
	}
	present := map[string]bool{}
	for _, h := range have {
		if want[h] {
			present[h] = true
			continue
		}
		if err := c.do(ctx, http.MethodDelete, base+"/ipset/"+url.PathEscape(name)+"/"+url.PathEscape(h), nil, nil); err != nil {
			return err
		}
	}
	for _, ip := range ips {
		if present[ip.String()] {
			continue
		}
		present[ip.String()] = true
		if err := c.do(ctx, http.MethodPost, base+"/ipset/"+url.PathEscape(name), url.Values{"cidr": {ip.String()}}, nil); err != nil {
			return err
		}
	}
	return nil
}

func (c *httpClient) DeleteIPSet(ctx context.Context, node string, vmid int, name string) error {
	base := vmPath(node, vmid) + "/firewall"
	ok, err := c.ipsetExists(ctx, base, name)
	if err != nil || !ok {
		return err
	}
	have, err := c.ipsetEntries(ctx, base, name)
	if err != nil {
		return err
	}
	for _, h := range have {
		if err := c.do(ctx, http.MethodDelete, base+"/ipset/"+url.PathEscape(name)+"/"+url.PathEscape(h), nil, nil); err != nil {
			return err
		}
	}
	return c.do(ctx, http.MethodDelete, base+"/ipset/"+url.PathEscape(name), nil, nil)
}

func (c *httpClient) SDNZones(ctx context.Context) ([]SDNZone, error) {
	var raw []struct {
		Zone  string `json:"zone"`
		Type  string `json:"type"`
		MTU   num    `json:"mtu"`
		Nodes string `json:"nodes"`
	}
	if err := c.do(ctx, http.MethodGet, "/cluster/sdn/zones", nil, &raw); err != nil {
		return nil, err
	}
	out := make([]SDNZone, len(raw))
	for i, r := range raw {
		out[i] = SDNZone{Zone: r.Zone, Type: r.Type, MTU: int(r.MTU)}
		for n := range strings.SplitSeq(r.Nodes, ",") {
			if n = strings.TrimSpace(n); n != "" {
				out[i].Nodes = append(out[i].Nodes, n)
			}
		}
	}
	return out, nil
}

func (c *httpClient) SDNVNets(ctx context.Context) ([]SDNVNet, error) {
	var raw []struct {
		VNet      string `json:"vnet"`
		Zone      string `json:"zone"`
		VLANAware num    `json:"vlanaware"`
	}
	if err := c.do(ctx, http.MethodGet, "/cluster/sdn/vnets", nil, &raw); err != nil {
		return nil, err
	}
	out := make([]SDNVNet, len(raw))
	for i, r := range raw {
		out[i] = SDNVNet{VNet: r.VNet, Zone: r.Zone, VLANAware: r.VLANAware != 0}
	}
	return out, nil
}

func (c *httpClient) SDNSubnets(ctx context.Context, vnet string) ([]string, error) {
	var raw []struct {
		Subnet string `json:"subnet"`
		CIDR   string `json:"cidr"`
	}
	if err := c.do(ctx, http.MethodGet, "/cluster/sdn/vnets/"+url.PathEscape(vnet)+"/subnets", nil, &raw); err != nil {
		return nil, err
	}
	out := make([]string, len(raw))
	for i, r := range raw {
		out[i] = cmp.Or(r.CIDR, r.Subnet)
	}
	return out, nil
}

func (c *httpClient) Guests(ctx context.Context) ([]GuestRef, error) {
	var raw []struct {
		Type string `json:"type"`
		VMID num    `json:"vmid"`
		Node string `json:"node"`
	}
	if err := c.do(ctx, http.MethodGet, "/cluster/resources", url.Values{"type": {"vm"}}, &raw); err != nil {
		return nil, err
	}
	var out []GuestRef
	for _, r := range raw {
		if r.Type == "qemu" || r.Type == "lxc" {
			out = append(out, GuestRef{VMID: int(r.VMID), Node: r.Node, Kind: r.Type})
		}
	}
	return out, nil
}

func guestPath(g GuestRef) string {
	return "/nodes/" + url.PathEscape(g.Node) + "/" + g.Kind + "/" + strconv.Itoa(g.VMID)
}

var netKey = regexp.MustCompile(`^net\d+$`)

// bridgesIn appends the bridge of every net key in cfg to out, tagged with where.
func bridgesIn(out []NetRef, cfg map[string]string, where string) []NetRef {
	for _, k := range slices.Sorted(maps.Keys(cfg)) {
		if !netKey.MatchString(k) {
			continue
		}
		for p := range strings.SplitSeq(cfg[k], ",") {
			if b, ok := strings.CutPrefix(p, "bridge="); ok {
				out = append(out, NetRef{Bridge: b, Where: where})
			}
		}
	}
	return out
}

func (c *httpClient) GuestNetRefs(ctx context.Context, g GuestRef) ([]NetRef, error) {
	base := guestPath(g)
	cfg, err := c.getMap(ctx, base+"/config")
	if err != nil {
		return nil, fmt.Errorf("guest %d config: %w", g.VMID, err)
	}
	out := bridgesIn(nil, cfg, "config")
	pend, err := c.pendingAt(ctx, base)
	if err != nil {
		return nil, fmt.Errorf("guest %d pending: %w", g.VMID, err)
	}
	for _, p := range pend {
		out = bridgesIn(out, map[string]string{p.Key: p.Value}, "pending")
		out = bridgesIn(out, map[string]string{p.Key: p.Pending}, "pending")
	}
	var snaps []struct {
		Name string `json:"name"`
	}
	if err := c.do(ctx, http.MethodGet, base+"/snapshot", nil, &snaps); err != nil {
		return nil, fmt.Errorf("guest %d snapshots: %w", g.VMID, err)
	}
	for _, s := range snaps {
		if s.Name == "current" {
			continue
		}
		sc, err := c.getMap(ctx, base+"/snapshot/"+url.PathEscape(s.Name)+"/config")
		if err != nil {
			return nil, fmt.Errorf("guest %d snapshot %s: %w", g.VMID, s.Name, err)
		}
		out = bridgesIn(out, sc, "snapshot "+s.Name)
	}
	return out, nil
}

func (c *httpClient) GuestBridges(ctx context.Context, g GuestRef) ([]string, error) {
	refs, err := c.GuestNetRefs(ctx, g)
	if err != nil {
		return nil, err
	}
	out := make([]string, len(refs))
	for i, r := range refs {
		out[i] = r.Bridge
	}
	return slices.Compact(slices.Sorted(slices.Values(out))), nil
}

func (c *httpClient) Power(ctx context.Context, node string, vmid int, op PowerOp) (UPID, error) {
	return c.task(ctx, http.MethodPost, vmPath(node, vmid)+"/status/"+string(op), nil)
}

func (c *httpClient) Destroy(ctx context.Context, node string, vmid int) (UPID, error) {
	return c.task(ctx, http.MethodDelete, vmPath(node, vmid), url.Values{"purge": {"1"}, "destroy-unreferenced-disks": {"1"}})
}

// settle waits when a call answered with a task id and ignores plain results.
func (c *httpClient) settle(ctx context.Context, node string, data string) error {
	if strings.HasPrefix(data, "UPID:") {
		return c.WaitTask(ctx, node, UPID(data))
	}
	return nil
}

// UploadISO buffers the form because pveproxy rejects chunked uploads; seed ISOs are small.
func (c *httpClient) UploadISO(ctx context.Context, node, storage, filename string, r io.Reader) error {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if err := mw.WriteField("content", "iso"); err != nil {
		return err
	}
	part, err := mw.CreateFormFile("filename", filename)
	if err != nil {
		return err
	}
	if _, err := io.Copy(part, r); err != nil {
		return err
	}
	if err := mw.Close(); err != nil {
		return err
	}
	target := c.conn.BaseURL + apiPrefix + "/nodes/" + url.PathEscape(node) + "/storage/" + url.PathEscape(storage) + "/upload"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body.Bytes()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	var up string
	if err := c.send(req, &up); err != nil {
		return err
	}
	return c.settle(ctx, node, up)
}

func (c *httpClient) DeleteVolume(ctx context.Context, node, storage, volid string) error {
	if !strings.Contains(volid, ":") {
		volid = storage + ":" + volid
	}
	var up string
	path := "/nodes/" + url.PathEscape(node) + "/storage/" + url.PathEscape(storage) + "/content/" + url.PathEscape(volid)
	if err := c.do(ctx, http.MethodDelete, path, nil, &up); err != nil {
		return err
	}
	return c.settle(ctx, node, up)
}

func (c *httpClient) VNCProxy(ctx context.Context, node string, vmid int) (VNCTicket, error) {
	var raw struct {
		Port   num    `json:"port"`
		Ticket string `json:"ticket"`
		User   string `json:"user"`
	}
	if err := c.do(ctx, http.MethodPost, vmPath(node, vmid)+"/vncproxy", url.Values{"websocket": {"1"}}, &raw); err != nil {
		return VNCTicket{}, err
	}
	return VNCTicket{Port: int(raw.Port), Ticket: raw.Ticket, User: raw.User}, nil
}

func (c *httpClient) VNCWebsocketURL(node string, vmid int, t VNCTicket) string {
	u := strings.Replace(c.conn.BaseURL, "http", "ws", 1) + apiPrefix + vmPath(node, vmid) + "/vncwebsocket"
	return u + "?" + url.Values{"port": {strconv.Itoa(t.Port)}, "vncticket": {t.Ticket}}.Encode()
}

func (c *httpClient) RRD(ctx context.Context, node string, vmid int, timeframe string) ([]RRDPoint, error) {
	var raw []struct {
		Time      num    `json:"time"`
		CPU       optNum `json:"cpu"`
		Mem       optNum `json:"mem"`
		MaxMem    optNum `json:"maxmem"`
		NetIn     optNum `json:"netin"`
		NetOut    optNum `json:"netout"`
		DiskRead  optNum `json:"diskread"`
		DiskWrite optNum `json:"diskwrite"`
	}
	f := url.Values{"timeframe": {timeframe}, "cf": {"AVERAGE"}}
	if err := c.do(ctx, http.MethodGet, vmPath(node, vmid)+"/rrddata", f, &raw); err != nil {
		return nil, err
	}
	out := make([]RRDPoint, len(raw))
	for i, r := range raw {
		out[i] = RRDPoint{Time: int64(r.Time), CPU: r.CPU.v, Mem: r.Mem.v, MaxMem: r.MaxMem.v,
			NetIn: r.NetIn.v, NetOut: r.NetOut.v, DiskRead: r.DiskRead.v, DiskWrite: r.DiskWrite.v}
	}
	return out, nil
}

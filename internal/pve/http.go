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
	"math"
	"mime/multipart"
	"net"
	"net/http"
	"net/netip"
	"net/url"
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
		want := normFP(c.CAFingerprint)
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

func normFP(s string) string {
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
		out[i] = Storage{ID: r.Storage, Content: content, Shared: r.Shared != 0, Avail: int64(r.Avail)}
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
	var raw map[string]any
	if err := c.do(ctx, http.MethodGet, vmPath(node, vmid)+"/config", nil, &raw); err != nil {
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
	f := url.Values{}
	for k, v := range kv {
		f.Set(k, v)
	}
	return c.do(ctx, http.MethodPut, vmPath(node, vmid)+"/config", f, nil)
}

func (c *httpClient) ResizeDisk(ctx context.Context, node string, vmid int, disk string, sizeGB int) error {
	f := url.Values{"disk": {disk}, "size": {strconv.Itoa(sizeGB) + "G"}}
	return c.do(ctx, http.MethodPut, vmPath(node, vmid)+"/resize", f, nil)
}

func (c *httpClient) SetFirewall(ctx context.Context, node string, vmid int, ips []netip.Addr) error {
	base := vmPath(node, vmid) + "/firewall"
	opts := url.Values{"enable": {"1"}, "ipfilter": {"1"}, "macfilter": {"1"}}
	if err := c.do(ctx, http.MethodPut, base+"/options", opts, nil); err != nil {
		return err
	}
	const set = "ipfilter-net0"
	var sets []struct {
		Name string `json:"name"`
	}
	if err := c.do(ctx, http.MethodGet, base+"/ipset", nil, &sets); err != nil {
		return err
	}
	if !slices.ContainsFunc(sets, func(s struct {
		Name string `json:"name"`
	}) bool {
		return s.Name == set
	}) {
		if err := c.do(ctx, http.MethodPost, base+"/ipset", url.Values{"name": {set}}, nil); err != nil {
			return err
		}
	}
	var have []struct {
		CIDR string `json:"cidr"`
	}
	if err := c.do(ctx, http.MethodGet, base+"/ipset/"+set, nil, &have); err != nil {
		return err
	}
	want := map[string]bool{}
	for _, ip := range ips {
		want[ip.String()] = true
	}
	present := map[string]bool{}
	for _, h := range have {
		if want[h.CIDR] {
			present[h.CIDR] = true
			continue
		}
		if err := c.do(ctx, http.MethodDelete, base+"/ipset/"+set+"/"+url.PathEscape(h.CIDR), nil, nil); err != nil {
			return err
		}
	}
	for _, ip := range ips {
		if present[ip.String()] {
			continue
		}
		present[ip.String()] = true
		if err := c.do(ctx, http.MethodPost, base+"/ipset/"+set, url.Values{"cidr": {ip.String()}}, nil); err != nil {
			return err
		}
	}
	return nil
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

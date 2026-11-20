// Package fake is an in-memory Proxmox VE API server for tests.
package fake

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/nebla-cloud/nebla/internal/pve"
)

const prefix = "/api2/json"

// FakeVM is a snapshot of a VM held by the fake.
type FakeVM struct {
	VMID     int
	Node     string
	Status   string
	Name     string
	Tags     string
	Template bool
	Config   map[string]string
	Firewall map[string]string
	IPSets   map[string][]string
}

type node struct {
	name     string
	cpus     int
	memMB    int64
	cpu      float64
	memUsed  int64
	offline  bool
	storages map[string]*storage
}

type storage struct {
	content []string
	shared  bool
	availMB int64
}

type vm struct {
	// listAt hides the template flag from /cluster/resources until then.
	listAt   time.Time
	node     string
	status   string
	config   map[string]string
	firewall map[string]string
	ipsets   map[string][]string
}

type task struct {
	exit  string
	ready time.Time
	apply func() string // run once when due; returns the exitstatus
	done  bool
}

type failure struct {
	pattern string
	status  int
}

// Server is the fake Proxmox endpoint.
type Server struct {
	srv     *httptest.Server
	tokenID string
	secret  string

	mu      sync.Mutex
	nodes   map[string]*node
	vms     map[int]*vm
	tasks   map[string]*task
	volumes map[string][]byte
	tickets map[string]bool
	fails   []failure
	calls   []string
	delay   time.Duration
	down    bool
	seq     int
	rrd     []map[string]any

	versions  map[string]string
	listDelay time.Duration
	downloads []FakeDownload
	failDl    []string
}

// FakeDownload records one download-url request.
type FakeDownload struct{ Node, Storage, URL, Filename, Content, Checksum, Algorithm string }

// New starts a fake PVE over TLS and stops it when the test ends.
func New(t *testing.T) *Server {
	t.Helper()
	s := &Server{tokenID: "root@pam!nebla", secret: "fake-secret", nodes: map[string]*node{},
		vms: map[int]*vm{}, tasks: map[string]*task{}, volumes: map[string][]byte{}, tickets: map[string]bool{}}
	mux := http.NewServeMux()
	s.routes(mux)
	s.srv = httptest.NewTLSServer(s.wrap(mux))
	t.Cleanup(s.srv.Close)
	return s
}

// Conn returns connection settings that pin the fake's certificate.
func (s *Server) Conn() pve.Conn {
	sum := sha256.Sum256(s.srv.Certificate().Raw)
	parts := make([]string, len(sum))
	for i, b := range sum {
		parts[i] = fmt.Sprintf("%02X", b)
	}
	return pve.Conn{BaseURL: s.srv.URL, TokenID: s.tokenID, Secret: s.secret, CAFingerprint: strings.Join(parts, ":"), PollInterval: 5 * time.Millisecond}
}

// HTTPClient trusts the fake's certificate.
func (s *Server) HTTPClient() *http.Client { return s.srv.Client() }

func (s *Server) AddNode(name string, cpus int, memMB int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nodes[name] = &node{name: name, cpus: cpus, memMB: memMB, storages: map[string]*storage{}}
}

func (s *Server) AddStorage(nodeName, id string, content []string, shared bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweep()
	s.nodes[nodeName].storages[id] = &storage{content: content, shared: shared, availMB: 500_000}
}

// AddTemplate adds a template VM with a scsi0 boot disk on storageID (created if missing).
func (s *Server) AddTemplate(nodeName string, vmid int, storageID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweep()
	n := s.nodes[nodeName]
	if _, ok := n.storages[storageID]; !ok {
		n.storages[storageID] = &storage{content: []string{"images", "iso"}, availMB: 500_000}
	}
	s.vms[vmid] = &vm{node: nodeName, status: "stopped", firewall: map[string]string{}, ipsets: map[string][]string{}, config: map[string]string{
		"name": fmt.Sprintf("tmpl-%d", vmid), "template": "1", "memory": "1024", "cores": "1",
		"scsi0": fmt.Sprintf("%s:base-%d-disk-0,size=10G", storageID, vmid),
	}}
}

// SetVMConfig sets one config key on a VM or template, e.g. a cloud-init drive clones then inherit.
func (s *Server) SetVMConfig(vmid int, key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweep()
	s.vms[vmid].config[key] = value
}

// AddVM adds a stopped, non-template VM with a scsi0 disk.
func (s *Server) AddVM(nodeName string, vmid int, name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweep()
	s.vms[vmid] = &vm{node: nodeName, status: "stopped", firewall: map[string]string{}, ipsets: map[string][]string{}, config: map[string]string{
		"name": name, "memory": "1024", "cores": "1", "scsi0": fmt.Sprintf("local-lvm:vm-%d-disk-0,size=10G", vmid),
	}}
}

// RemoveNode drops a node from the cluster listing; its VMs stay.
func (s *Server) RemoveNode(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweep()
	delete(s.nodes, name)
}

// SetRRD replaces the rrddata every VM returns; nil restores the default five points.
func (s *Server) SetRRD(points []map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rrd = points
}

// SetNodeVersion sets the pve-manager version a node reports (default 8.4.1).
func (s *Server) SetNodeVersion(node, v string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.versions == nil {
		s.versions = map[string]string{}
	}
	s.versions[node] = v
}

// Downloads lists the download-url requests received.
func (s *Server) Downloads() []FakeDownload {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.downloads)
}

// FailDownload ends the next download whose URL contains urlSubstring with a checksum mismatch.
func (s *Server) FailDownload(urlSubstring string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failDl = append(s.failDl, urlSubstring)
}

func (s *Server) DeleteVM(vmid int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweep()
	delete(s.vms, vmid)
}

func (s *Server) MoveVM(vmid int, nodeName string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweep()
	if v, ok := s.vms[vmid]; ok {
		v.node = nodeName
	}
}

func (s *Server) SetVMStatus(vmid int, status string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweep()
	if v, ok := s.vms[vmid]; ok {
		v.status = status
	}
}

func (s *Server) SetNodeLoad(name string, cpu float64, memUsedMB int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweep()
	if n, ok := s.nodes[name]; ok {
		n.cpu, n.memUsed = cpu, memUsedMB
	}
}

// SetNodeOffline reports the node offline and hides its VMs from /cluster/resources.
func (s *Server) SetNodeOffline(name string, offline bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweep()
	if n, ok := s.nodes[name]; ok {
		n.offline = offline
	}
}

// SetTemplateListDelay makes /cluster/resources list a converted VM as a template only d after conversion.
func (s *Server) SetTemplateListDelay(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listDelay = d
}

// SetTaskDelay keeps newly created tasks running for d before they finish.
func (s *Server) SetTaskDelay(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.delay = d
}

// SetDown makes every request fail with 503 until it is set back.
func (s *Server) SetDown(down bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.down = down
}

// FailNext fails the next "METHOD path" ending in pattern: a failed task if it creates one (unless 502-504), else an HTTP error.
func (s *Server) FailNext(pattern string, status int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fails = append(s.fails, failure{pattern, status})
}

// Calls lists the requests received as "METHOD path".
func (s *Server) Calls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.calls)
}

func (s *Server) VM(vmid int) (FakeVM, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweep()
	v, ok := s.vms[vmid]
	if !ok {
		return FakeVM{}, false
	}
	f := FakeVM{VMID: vmid, Node: v.node, Status: v.status, Name: v.config["name"], Tags: v.config["tags"],
		Template: v.config["template"] == "1", Config: map[string]string{}, Firewall: map[string]string{}, IPSets: map[string][]string{}}
	for k, val := range v.config {
		f.Config[k] = val
	}
	for k, val := range v.firewall {
		f.Firewall[k] = val
	}
	for k, val := range v.ipsets {
		f.IPSets[k] = slices.Clone(val)
	}
	return f, true
}

// PutVolume stores a volume, e.g. a leftover import file.
func (s *Server) PutVolume(id string, data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweep()
	s.volumes[id] = slices.Clone(data)
}

func (s *Server) Volume(id string) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweep()
	b, ok := s.volumes[id]
	return slices.Clone(b), ok
}

type apiErr struct {
	status int
	msg    string
}

func notExist(vmid int, nodeName string) *apiErr {
	return &apiErr{500, fmt.Sprintf("Configuration file 'nodes/%s/qemu-server/%d.conf' does not exist", nodeName, vmid)}
}

// locked rejects changes to a VM holding a lock, like PVE.
func locked(v *vm, vmid int) *apiErr {
	if l := v.config["lock"]; l != "" {
		return &apiErr{500, fmt.Sprintf("VM %d is locked (%s)", vmid, l)}
	}
	return nil
}

// handler returns data or an error; failTask is set when FailNext hit a task endpoint.
type handler func(r *http.Request, failTask bool) (any, *apiErr)

func (s *Server) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, prefix)
		s.mu.Lock()
		s.calls = append(s.calls, r.Method+" "+p)
		s.sweep()
		down := s.down
		s.mu.Unlock()
		if down {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"data": nil, "message": "proxmox down"})
			return
		}
		if r.Header.Get("Authorization") != "PVEAPIToken="+s.tokenID+"="+s.secret {
			writeJSON(w, 401, map[string]any{"data": nil, "message": "no ticket"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) takeFail(r *http.Request) (failure, bool) {
	key := r.Method + " " + strings.TrimPrefix(r.URL.Path, prefix)
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, f := range s.fails {
		if strings.HasSuffix(key, f.pattern) {
			s.fails = slices.Delete(s.fails, i, i+1)
			return f, true
		}
	}
	return failure{}, false
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) handle(mux *http.ServeMux, pattern string, taskEndpoint bool, h handler) {
	method, path, _ := strings.Cut(pattern, " ")
	mux.HandleFunc(method+" "+prefix+path, func(w http.ResponseWriter, r *http.Request) {
		f, failed := s.takeFail(r)
		if failed && (!taskEndpoint || f.status >= 502 && f.status <= 504) {
			writeJSON(w, f.status, map[string]any{"data": nil, "message": fmt.Sprintf("injected failure (status %d)", f.status)})
			return
		}
		data, e := h(r, failed)
		if e != nil {
			writeJSON(w, e.status, map[string]any{"data": nil, "message": e.msg})
			return
		}
		writeJSON(w, 200, map[string]any{"data": data})
	})
}

// sweep runs the effects of due tasks in start order; caller holds s.mu.
func (s *Server) sweep() { s.sweepAt(time.Now()) }

func (s *Server) sweepAt(now time.Time) {
	var due []string
	for id, tk := range s.tasks {
		if !tk.done && !now.Before(tk.ready) {
			due = append(due, id)
		}
	}
	sort.Slice(due, func(i, j int) bool {
		return s.tasks[due[i]].ready.Before(s.tasks[due[j]].ready) || (s.tasks[due[i]].ready.Equal(s.tasks[due[j]].ready) && due[i] < due[j])
	})
	for _, id := range due {
		tk := s.tasks[id]
		tk.done = true
		if tk.apply != nil {
			tk.exit = tk.apply()
		}
	}
}

// newTask records a task for nodeName; caller holds s.mu.
func (s *Server) newTask(nodeName, kind string, vmid int, fail *failure, apply func() string) string {
	s.seq++
	upid := fmt.Sprintf("UPID:%s:%08X:%08X:%08X:%s:%d:%s:", nodeName, s.seq, s.seq, time.Now().Unix(), kind, vmid, s.tokenID)
	exit := "OK"
	if fail != nil {
		exit, apply = fmt.Sprintf("ERROR: injected failure (status %d)", fail.status), nil
	}
	s.tasks[upid] = &task{exit: exit, ready: time.Now().Add(s.delay), apply: apply}
	return upid
}

func failPtr(failed bool) *failure {
	if failed {
		return &failure{status: 500}
	}
	return nil
}

func (s *Server) routes(mux *http.ServeMux) {
	form := func(r *http.Request) map[string]string {
		_ = r.ParseForm()
		m := map[string]string{}
		for k := range r.Form {
			m[k] = r.Form.Get(k)
		}
		return m
	}
	// vmFor returns the VM on the path's node; caller holds s.mu.
	vmFor := func(r *http.Request) (*vm, int, *apiErr) {
		id, _ := strconv.Atoi(r.PathValue("vmid"))
		v, ok := s.vms[id]
		if !ok || v.node != r.PathValue("node") {
			return nil, id, notExist(id, r.PathValue("node"))
		}
		return v, id, nil
	}
	nodeFor := func(r *http.Request) (*node, *apiErr) {
		n, ok := s.nodes[r.PathValue("node")]
		if !ok {
			return nil, &apiErr{500, fmt.Sprintf("Configuration file 'nodes/%s/storage.cfg' does not exist", r.PathValue("node"))}
		}
		return n, nil
	}

	s.handle(mux, "GET /version", false, func(*http.Request, bool) (any, *apiErr) {
		return map[string]any{"version": "8.4.1", "release": "8.4", "repoid": "fake"}, nil
	})
	s.handle(mux, "GET /nodes/{node}/version", false, func(r *http.Request, _ bool) (any, *apiErr) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if _, e := nodeFor(r); e != nil {
			return nil, e
		}
		v := s.versions[r.PathValue("node")]
		if v == "" {
			v = "8.4.1"
		}
		return map[string]any{"version": v, "release": v, "repoid": "fake"}, nil
	})
	s.handle(mux, "POST /nodes/{node}/storage/{storage}/download-url", true, func(r *http.Request, failed bool) (any, *apiErr) {
		f := form(r)
		s.mu.Lock()
		defer s.mu.Unlock()
		n, e := nodeFor(r)
		if e != nil {
			return nil, e
		}
		st, ok := n.storages[r.PathValue("storage")]
		if !ok {
			return nil, &apiErr{500, "storage '" + r.PathValue("storage") + "' does not exist"}
		}
		if f["url"] == "" || f["filename"] == "" || f["content"] != "import" {
			return nil, &apiErr{400, "parameter verification failed: url/filename/content"}
		}
		if !slices.Contains(st.content, "import") {
			return nil, &apiErr{500, "storage '" + r.PathValue("storage") + "' does not support content type 'import'"}
		}
		id := r.PathValue("storage") + ":import/" + f["filename"]
		if _, dup := s.volumes[id]; dup {
			return nil, &apiErr{500, "volume '" + id + "' already exists"}
		}
		s.downloads = append(s.downloads, FakeDownload{Node: n.name, Storage: r.PathValue("storage"), URL: f["url"], Filename: f["filename"],
			Content: f["content"], Checksum: f["checksum"], Algorithm: f["checksum-algorithm"]})
		if i := slices.IndexFunc(s.failDl, func(sub string) bool { return strings.Contains(f["url"], sub) }); i >= 0 {
			s.failDl = slices.Delete(s.failDl, i, i+1)
			failed = true
		}
		if failed {
			up := s.newTask(n.name, "download", 0, nil, func() string { return "checksum mismatch" })
			return up, nil
		}
		return s.newTask(n.name, "download", 0, nil, func() string {
			s.volumes[id] = []byte(f["url"])
			return "OK"
		}), nil
	})
	s.handle(mux, "POST /nodes/{node}/qemu", true, func(r *http.Request, failed bool) (any, *apiErr) {
		f := form(r)
		s.mu.Lock()
		defer s.mu.Unlock()
		n, e := nodeFor(r)
		if e != nil {
			return nil, e
		}
		id, _ := strconv.Atoi(f["vmid"])
		if id == 0 {
			return nil, &apiErr{400, "parameter verification failed: vmid"}
		}
		if _, dup := s.vms[id]; dup {
			return nil, &apiErr{500, fmt.Sprintf("unable to create VM %d: config file already exists", id)}
		}
		if failed {
			return s.newTask(n.name, "qmcreate", id, failPtr(true), nil), nil
		}
		cfg := map[string]string{"lock": "create"}
		nv := &vm{node: n.name, status: "stopped", firewall: map[string]string{}, ipsets: map[string][]string{}, config: cfg}
		s.vms[id] = nv
		return s.newTask(n.name, "qmcreate", id, nil, func() string {
			if s.vms[id] != nv {
				return fmt.Sprintf("ERROR: unable to create VM %d: config file already exists", id)
			}
			full := map[string]string{}
			for k, val := range f {
				if k != "vmid" {
					full[k] = val
				}
			}
			if sc, ok := full["scsi0"]; ok {
				disk, rest, _ := strings.Cut(sc, ",")
				for _, p := range strings.Split(rest, ",") {
					if vol, ok := strings.CutPrefix(p, "import-from="); ok {
						if _, ok := s.volumes[vol]; !ok {
							delete(s.vms, id)
							return fmt.Sprintf("ERROR: volume '%s' does not exist", vol)
						}
					}
				}
				store, _, _ := strings.Cut(disk, ":")
				full["scsi0"] = fmt.Sprintf("%s:vm-%d-disk-0,size=3G", store, id)
			}
			nv.config = full
			return "OK"
		}), nil
	})
	s.handle(mux, "POST /nodes/{node}/qemu/{vmid}/template", true, func(r *http.Request, failed bool) (any, *apiErr) {
		s.mu.Lock()
		defer s.mu.Unlock()
		v, id, e := vmFor(r)
		if e != nil {
			return nil, e
		}
		if e := locked(v, id); e != nil {
			return nil, e
		}
		if failed {
			return s.newTask(v.node, "qmtemplate", id, failPtr(true), nil), nil
		}
		return s.newTask(v.node, "qmtemplate", id, nil, func() string {
			v.config["template"] = "1"
			v.listAt = time.Now().Add(s.listDelay)
			return "OK"
		}), nil
	})
	s.handle(mux, "GET /nodes", false, func(*http.Request, bool) (any, *apiErr) {
		s.mu.Lock()
		defer s.mu.Unlock()
		out := []map[string]any{}
		for _, n := range s.nodes {
			status := "online"
			if n.offline {
				status = "offline"
			}
			out = append(out, map[string]any{"node": n.name, "status": status, "type": "node", "maxcpu": n.cpus,
				"maxmem": strconv.FormatInt(n.memMB<<20, 10), "cpu": n.cpu, "mem": n.memUsed << 20})
		}
		sort.Slice(out, func(i, j int) bool { return out[i]["node"].(string) < out[j]["node"].(string) })
		return out, nil
	})
	s.handle(mux, "GET /nodes/{node}/storage", false, func(r *http.Request, _ bool) (any, *apiErr) {
		s.mu.Lock()
		defer s.mu.Unlock()
		n, e := nodeFor(r)
		if e != nil {
			return nil, e
		}
		out := []map[string]any{}
		for id, st := range n.storages {
			shared := 0
			if st.shared {
				shared = 1
			}
			out = append(out, map[string]any{"storage": id, "content": strings.Join(st.content, ","), "shared": shared,
				"avail": st.availMB << 20, "active": 1, "enabled": 1})
		}
		sort.Slice(out, func(i, j int) bool { return out[i]["storage"].(string) < out[j]["storage"].(string) })
		return out, nil
	})
	s.handle(mux, "GET /cluster/resources", false, func(*http.Request, bool) (any, *apiErr) {
		s.mu.Lock()
		defer s.mu.Unlock()
		out := []map[string]any{}
		for id, v := range s.vms {
			if n, ok := s.nodes[v.node]; ok && n.offline {
				continue
			}
			tmpl := 0
			if v.config["template"] == "1" && !time.Now().Before(v.listAt) {
				tmpl = 1
			}
			out = append(out, map[string]any{"id": fmt.Sprintf("qemu/%d", id), "type": "qemu", "vmid": id, "node": v.node,
				"name": v.config["name"], "status": v.status, "tags": v.config["tags"], "template": tmpl})
		}
		sort.Slice(out, func(i, j int) bool { return out[i]["vmid"].(int) < out[j]["vmid"].(int) })
		return out, nil
	})
	s.handle(mux, "GET /cluster/nextid", false, func(*http.Request, bool) (any, *apiErr) {
		s.mu.Lock()
		defer s.mu.Unlock()
		id := 100
		for _, ok := s.vms[id]; ok; _, ok = s.vms[id] {
			id++
		}
		return strconv.Itoa(id), nil
	})
	s.handle(mux, "POST /nodes/{node}/qemu/{vmid}/clone", true, func(r *http.Request, failed bool) (any, *apiErr) {
		f := form(r)
		s.mu.Lock()
		defer s.mu.Unlock()
		src, id, e := vmFor(r)
		if e != nil {
			return nil, e
		}
		newID, _ := strconv.Atoi(f["newid"])
		if newID == 0 {
			return nil, &apiErr{400, "parameter verification failed: newid"}
		}
		if _, dup := s.vms[newID]; dup {
			return nil, &apiErr{500, fmt.Sprintf("unable to create VM %d: config file already exists", newID)}
		}
		target := f["target"]
		if target == "" {
			target = src.node
		}
		if _, ok := s.nodes[target]; !ok {
			return nil, &apiErr{500, fmt.Sprintf("Configuration file 'nodes/%s/storage.cfg' does not exist", target)}
		}
		full := map[string]string{}
		for k, val := range src.config {
			full[k] = val
		}
		delete(full, "template")
		if f["name"] != "" {
			full["name"] = f["name"]
		}
		if f["description"] != "" {
			full["description"] = f["description"]
		}
		if failed {
			return s.newTask(src.node, "qmclone", id, failPtr(true), nil), nil
		}
		// Like PVE, the new VMID holds a lock-only config until the clone task finishes.
		nv := &vm{node: target, status: "stopped", firewall: map[string]string{}, ipsets: map[string][]string{}, config: map[string]string{"lock": "clone"}}
		s.vms[newID] = nv
		return s.newTask(src.node, "qmclone", id, nil, func() string {
			if s.vms[newID] != nv {
				return fmt.Sprintf("ERROR: unable to create VM %d: config file already exists", newID)
			}
			nv.config = full
			return "OK"
		}), nil
	})
	s.handle(mux, "GET /nodes/{node}/tasks/{upid}/status", false, func(r *http.Request, _ bool) (any, *apiErr) {
		s.mu.Lock()
		defer s.mu.Unlock()
		now := time.Now()
		s.sweepAt(now)
		tk, ok := s.tasks[r.PathValue("upid")]
		if !ok {
			return nil, &apiErr{500, "no such task"}
		}
		if now.Before(tk.ready) {
			return map[string]any{"status": "running"}, nil
		}
		return map[string]any{"status": "stopped", "exitstatus": tk.exit}, nil
	})
	setConfig := func(r *http.Request, _ bool) (any, *apiErr) {
		f := form(r)
		s.mu.Lock()
		defer s.mu.Unlock()
		v, id, e := vmFor(r)
		if e != nil {
			return nil, e
		}
		if e := locked(v, id); e != nil {
			return nil, e
		}
		for k, val := range f {
			if k != "delete" {
				v.config[k] = val
			}
		}
		for _, k := range strings.Split(f["delete"], ",") {
			delete(v.config, strings.TrimSpace(k))
		}
		return nil, nil
	}
	s.handle(mux, "PUT /nodes/{node}/qemu/{vmid}/config", false, setConfig)
	s.handle(mux, "POST /nodes/{node}/qemu/{vmid}/config", false, setConfig)
	s.handle(mux, "GET /nodes/{node}/qemu/{vmid}/config", false, func(r *http.Request, _ bool) (any, *apiErr) {
		s.mu.Lock()
		defer s.mu.Unlock()
		v, _, e := vmFor(r)
		if e != nil {
			return nil, e
		}
		out := map[string]any{"digest": "fake"}
		for k, val := range v.config {
			if n, err := strconv.Atoi(val); err == nil && k != "name" {
				out[k] = n
			} else {
				out[k] = val
			}
		}
		return out, nil
	})
	s.handle(mux, "PUT /nodes/{node}/qemu/{vmid}/resize", false, func(r *http.Request, _ bool) (any, *apiErr) {
		f := form(r)
		s.mu.Lock()
		defer s.mu.Unlock()
		v, id, e := vmFor(r)
		if e != nil {
			return nil, e
		}
		if e := locked(v, id); e != nil {
			return nil, e
		}
		cur, ok := v.config[f["disk"]]
		if !ok || !strings.HasSuffix(f["size"], "G") {
			return nil, &apiErr{500, "disk '" + f["disk"] + "' does not exist"}
		}
		var keep []string
		oldGB := 0
		for _, p := range strings.Split(cur, ",") {
			if sz, ok := strings.CutPrefix(p, "size="); ok {
				oldGB, _ = strconv.Atoi(strings.TrimSuffix(sz, "G"))
			} else {
				keep = append(keep, p)
			}
		}
		if newGB, _ := strconv.Atoi(strings.TrimSuffix(f["size"], "G")); newGB < oldGB {
			return nil, &apiErr{500, "shrinking disks is not supported"}
		}
		v.config[f["disk"]] = strings.Join(keep, ",") + ",size=" + f["size"]
		return nil, nil
	})
	s.handle(mux, "GET /nodes/{node}/qemu/{vmid}/firewall/options", false, func(r *http.Request, _ bool) (any, *apiErr) {
		s.mu.Lock()
		defer s.mu.Unlock()
		v, _, e := vmFor(r)
		if e != nil {
			return nil, e
		}
		out := map[string]any{}
		for k, val := range v.firewall {
			out[k] = val
		}
		return out, nil
	})
	s.handle(mux, "PUT /nodes/{node}/qemu/{vmid}/firewall/options", false, func(r *http.Request, _ bool) (any, *apiErr) {
		f := form(r)
		s.mu.Lock()
		defer s.mu.Unlock()
		v, _, e := vmFor(r)
		if e != nil {
			return nil, e
		}
		for k, val := range f {
			v.firewall[k] = val
		}
		return nil, nil
	})
	s.handle(mux, "GET /nodes/{node}/qemu/{vmid}/firewall/ipset", false, func(r *http.Request, _ bool) (any, *apiErr) {
		s.mu.Lock()
		defer s.mu.Unlock()
		v, _, e := vmFor(r)
		if e != nil {
			return nil, e
		}
		out := []map[string]any{}
		for name := range v.ipsets {
			out = append(out, map[string]any{"name": name, "digest": "fake"})
		}
		return out, nil
	})
	s.handle(mux, "POST /nodes/{node}/qemu/{vmid}/firewall/ipset", false, func(r *http.Request, _ bool) (any, *apiErr) {
		f := form(r)
		s.mu.Lock()
		defer s.mu.Unlock()
		v, _, e := vmFor(r)
		if e != nil {
			return nil, e
		}
		if _, dup := v.ipsets[f["name"]]; dup {
			return nil, &apiErr{500, "IPSet '" + f["name"] + "' already exists"}
		}
		v.ipsets[f["name"]] = []string{}
		return nil, nil
	})
	s.handle(mux, "GET /nodes/{node}/qemu/{vmid}/firewall/ipset/{name}", false, func(r *http.Request, _ bool) (any, *apiErr) {
		s.mu.Lock()
		defer s.mu.Unlock()
		v, _, e := vmFor(r)
		if e != nil {
			return nil, e
		}
		entries, ok := v.ipsets[r.PathValue("name")]
		if !ok {
			return nil, &apiErr{500, "IPSet '" + r.PathValue("name") + "' does not exist"}
		}
		out := []map[string]any{}
		for _, c := range entries {
			out = append(out, map[string]any{"cidr": c, "digest": "fake"})
		}
		return out, nil
	})
	s.handle(mux, "POST /nodes/{node}/qemu/{vmid}/firewall/ipset/{name}", false, func(r *http.Request, _ bool) (any, *apiErr) {
		f := form(r)
		s.mu.Lock()
		defer s.mu.Unlock()
		v, _, e := vmFor(r)
		if e != nil {
			return nil, e
		}
		name := r.PathValue("name")
		entries, ok := v.ipsets[name]
		if !ok {
			return nil, &apiErr{500, "IPSet '" + name + "' does not exist"}
		}
		if slices.Contains(entries, f["cidr"]) {
			return nil, &apiErr{500, "address '" + f["cidr"] + "' already exists"}
		}
		v.ipsets[name] = append(entries, f["cidr"])
		return nil, nil
	})
	s.handle(mux, "DELETE /nodes/{node}/qemu/{vmid}/firewall/ipset/{name}/{cidr}", false, func(r *http.Request, _ bool) (any, *apiErr) {
		s.mu.Lock()
		defer s.mu.Unlock()
		v, _, e := vmFor(r)
		if e != nil {
			return nil, e
		}
		name := r.PathValue("name")
		if _, ok := v.ipsets[name]; !ok {
			return nil, &apiErr{500, "IPSet '" + name + "' does not exist"}
		}
		v.ipsets[name] = slices.DeleteFunc(v.ipsets[name], func(c string) bool { return c == r.PathValue("cidr") })
		return nil, nil
	})
	for _, op := range []string{"start", "stop", "shutdown", "reboot", "reset"} {
		s.handle(mux, "POST /nodes/{node}/qemu/{vmid}/status/"+op, true, func(r *http.Request, failed bool) (any, *apiErr) {
			s.mu.Lock()
			defer s.mu.Unlock()
			v, id, e := vmFor(r)
			if e != nil {
				return nil, e
			}
			if e := locked(v, id); e != nil {
				return nil, e
			}
			return s.newTask(v.node, "qm"+op, id, failPtr(failed), func() string {
				switch {
				case op == "start" && v.status == "running":
					return fmt.Sprintf("ERROR: VM %d already running", id)
				case op != "start" && v.status != "running":
					return fmt.Sprintf("ERROR: VM %d not running", id)
				case op == "stop" || op == "shutdown":
					v.status = "stopped"
				default:
					v.status = "running"
				}
				return "OK"
			}), nil
		})
	}
	s.handle(mux, "DELETE /nodes/{node}/qemu/{vmid}", true, func(r *http.Request, failed bool) (any, *apiErr) {
		s.mu.Lock()
		defer s.mu.Unlock()
		v, id, e := vmFor(r)
		if e != nil {
			return nil, e
		}
		if e := locked(v, id); e != nil {
			return nil, e
		}
		if v.status == "running" {
			return nil, &apiErr{500, fmt.Sprintf("VM %d is running - destroy failed", id)}
		}
		return s.newTask(v.node, "qmdestroy", id, failPtr(failed), func() string {
			delete(s.vms, id)
			return "OK"
		}), nil
	})
	s.handle(mux, "POST /nodes/{node}/storage/{storage}/upload", false, func(r *http.Request, _ bool) (any, *apiErr) {
		// pveproxy rejects uploads without a Content-Length.
		if slices.Contains(r.TransferEncoding, "chunked") || r.ContentLength < 0 {
			return nil, &apiErr{501, "chunked transfer encoding not supported"}
		}
		mr, err := r.MultipartReader()
		if err != nil {
			return nil, &apiErr{400, "multipart required"}
		}
		var content, fname string
		var data []byte
		for {
			part, err := mr.NextPart()
			if err != nil {
				break
			}
			b, _ := io.ReadAll(part)
			switch part.FormName() {
			case "content":
				content = string(b)
			case "filename":
				fname, data = part.FileName(), b
			}
		}
		if content != "iso" || fname == "" {
			return nil, &apiErr{400, "parameter verification failed: content/filename"}
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		n, e := nodeFor(r)
		if e != nil {
			return nil, e
		}
		st := r.PathValue("storage")
		if _, ok := n.storages[st]; !ok {
			return nil, &apiErr{500, "storage '" + st + "' does not exist"}
		}
		s.volumes[st+":iso/"+fname] = data
		return s.newTask(n.name, "imgcopy", 0, nil, nil), nil
	})
	s.handle(mux, "DELETE /nodes/{node}/storage/{storage}/content/{volid}", false, func(r *http.Request, _ bool) (any, *apiErr) {
		s.mu.Lock()
		defer s.mu.Unlock()
		n, e := nodeFor(r)
		if e != nil {
			return nil, e
		}
		id := r.PathValue("volid")
		if !strings.Contains(id, ":") {
			id = r.PathValue("storage") + ":" + id
		}
		if _, ok := s.volumes[id]; !ok {
			return nil, &apiErr{500, "volume '" + id + "' does not exist"}
		}
		delete(s.volumes, id)
		return s.newTask(n.name, "imgdel", 0, nil, nil), nil
	})
	s.handle(mux, "POST /nodes/{node}/qemu/{vmid}/vncproxy", false, func(r *http.Request, _ bool) (any, *apiErr) {
		s.mu.Lock()
		defer s.mu.Unlock()
		_, id, e := vmFor(r)
		if e != nil {
			return nil, e
		}
		s.seq++
		ticket := fmt.Sprintf("PVEVNC:%d:%d", id, s.seq)
		s.tickets[ticket] = true
		return map[string]any{"port": "5900", "ticket": ticket, "user": s.tokenID, "cert": "fake", "upid": ""}, nil
	})
	mux.HandleFunc("GET "+prefix+"/nodes/{node}/qemu/{vmid}/vncwebsocket", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		_, _, e := vmFor(r)
		ok := e == nil && s.tickets[r.URL.Query().Get("vncticket")] && r.URL.Query().Get("port") == "5900"
		s.mu.Unlock()
		if !ok {
			writeJSON(w, 401, map[string]any{"data": nil, "message": "invalid vnc ticket"})
			return
		}
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		for {
			typ, b, err := c.Read(r.Context())
			if err != nil || c.Write(r.Context(), typ, b) != nil {
				return
			}
		}
	})
	s.handle(mux, "GET /nodes/{node}/qemu/{vmid}/rrddata", false, func(r *http.Request, _ bool) (any, *apiErr) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if _, _, e := vmFor(r); e != nil {
			return nil, e
		}
		if !slices.Contains([]string{"hour", "day", "week", "month", "year"}, r.URL.Query().Get("timeframe")) {
			return nil, &apiErr{400, "parameter verification failed: timeframe"}
		}
		if s.rrd != nil {
			return s.rrd, nil
		}
		out := []map[string]any{}
		for i := range 5 {
			out = append(out, map[string]any{"time": 1_700_000_000 + i*60, "cpu": 0.1 * float64(i+1), "mem": 100_000_000 * (i + 1),
				"maxmem": 1 << 30, "netin": 1000 * i, "netout": 500 * i, "diskread": 0, "diskwrite": 2048 * i})
		}
		return out, nil
	})
}

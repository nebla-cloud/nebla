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

// Package fake is an in-memory Proxmox VE API server for tests.
package fake

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"path"
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
	Rules    []pve.FirewallRule
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
	typ     string // "" lists as lvmthin
	content []string
	shared  bool
	availMB int64
	vols    map[string]*volume // allocated disk images by volid
}

type volume struct {
	size int64 // bytes
	vmid int
}

// pendingChange is a config change held back until the VM restarts.
type pendingChange struct {
	value string
	del   bool
}

type vm struct {
	// listAt hides the template flag from /cluster/resources until then.
	listAt   time.Time
	node     string
	status   string
	config   map[string]string
	firewall map[string]string
	ipsets   map[string][]string
	rules    []pve.FirewallRule
	rev      int // bumped on every config write; it is the digest
	pending  map[string]pendingChange
	snaps    map[string]map[string]string // snapshot configs by name
	unplug   map[string]int               // delete=<key> writes that stay pending
}

// lxcGuest is a container the fake serves config and snapshots for.
type lxcGuest struct {
	config map[string]string
	snaps  map[string]map[string]string
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
	// DenyDatastoreAllocate makes volume deletes fail with a 403 like a token without Datastore.Allocate.
	DenyDatastoreAllocate bool
	// FailVzdump and FailRestore make the next backup or restore tasks end with an error.
	FailVzdump, FailRestore bool

	perms map[string]map[string]int // explicit GET /access/permissions answers by path

	srv     *httptest.Server
	tokenID string
	secret  string
	extra   map[string]string // additional accepted tokens by id

	mu      sync.Mutex
	nodes   map[string]*node
	vms     map[int]*vm
	cts     map[int]string // LXC containers: vmid to node
	lxc     map[int]*lxcGuest
	zones   []pve.SDNZone
	vnets   []pve.SDNVNet
	subnets map[string][]string
	sdnDeny bool
	tasks   map[string]*task
	volumes map[string][]byte
	backups map[string]*archive // by volid
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

	clusterFWOff bool
	failRule     bool
	onFWWrite    func()
	onIPSetWrite func()
	onRuleAdd    func(*pve.FirewallRule)
	needDigest   bool
	conflictDel  bool
}

// FakeDownload records one download-url request.
type FakeDownload struct{ Node, Storage, URL, Filename, Content, Checksum, Algorithm string }

// New starts a fake PVE over TLS and stops it when the test ends.
func New(t *testing.T) *Server {
	t.Helper()
	s := &Server{tokenID: "root@pam!nebla", secret: "fake-secret", nodes: map[string]*node{},
		vms: map[int]*vm{}, cts: map[int]string{}, lxc: map[int]*lxcGuest{}, subnets: map[string][]string{}, tasks: map[string]*task{}, volumes: map[string][]byte{}, tickets: map[string]bool{}}
	mux := http.NewServeMux()
	s.routes(mux)
	s.srv = httptest.NewTLSServer(s.wrap(mux))
	t.Cleanup(s.srv.Close)
	return s
}

// AddToken makes the fake accept the token id and secret as well as its default.
func (s *Server) AddToken(id, secret string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.extra == nil {
		s.extra = map[string]string{}
	}
	s.extra[id] = secret
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

// allPrivs is what the fake grants on a path without an explicit answer.
var allPrivs = []string{"Datastore.Allocate", "Datastore.AllocateSpace", "Datastore.AllocateTemplate", "Datastore.Audit",
	"SDN.Audit", "Sys.Audit", "VM.Allocate", "VM.Audit", "VM.Backup", "VM.Config.Disk"}

// SetPermissions fixes the privileges the fake reports for path; nil restores the default (everything).
func (s *Server) SetPermissions(path string, privs map[string]int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.perms == nil {
		s.perms = map[string]map[string]int{}
	}
	if privs == nil {
		delete(s.perms, path)
		return
	}
	s.perms[path] = maps.Clone(privs)
}

// DenyAllocateOn reports every privilege but Datastore.Allocate on /storage/<id>.
func (s *Server) DenyAllocateOn(storage string) {
	privs := map[string]int{}
	for _, p := range allPrivs {
		if p != "Datastore.Allocate" {
			privs[p] = 1
		}
	}
	s.SetPermissions("/storage/"+storage, privs)
}

// SetStorageType sets the plugin type a node's storage lists; file types (dir, nfs, ...) keep disks as vmid/name.ext.
func (s *Server) SetStorageType(nodeName, id, typ string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nodes[nodeName].storages[id].typ = typ
}

// RemoveStorage drops a storage from a node.
func (s *Server) RemoveStorage(nodeName, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.nodes[nodeName].storages, id)
}

// SetStorageAvail sets the free space a node's storage reports.
func (s *Server) SetStorageAvail(nodeName, id string, availMB int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nodes[nodeName].storages[id].availMB = availMB
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

// SetVMFirewall replaces a VM's firewall options and drops its rules.
func (s *Server) SetVMFirewall(vmid int, opts map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweep()
	s.vms[vmid].firewall = maps.Clone(opts)
	s.vms[vmid].rules = nil
}

// SetVMIPSet replaces the entries of a VM's ipset, creating it if needed.
func (s *Server) SetVMIPSet(vmid int, name string, cidrs []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweep()
	s.vms[vmid].ipsets[name] = slices.Clone(cidrs)
}

// AddContainer lists an LXC container at vmid; only /cluster/resources shows it.
func (s *Server) AddContainer(nodeName string, vmid int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cts[vmid] = nodeName
}

// AddSDN lists a zone and its vnets.
func (s *Server) AddSDN(zone pve.SDNZone, vnets ...pve.SDNVNet) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.zones = append(s.zones, zone)
	s.vnets = append(s.vnets, vnets...)
}

// SetSDNZoneType changes a listed zone's type, as an admin editing it would.
func (s *Server) SetSDNZoneType(zone, typ string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.zones {
		if s.zones[i].Zone == zone {
			s.zones[i].Type = typ
		}
	}
}

// RemoveSDNVNet drops a vnet and its subnets, as an admin deleting it would.
func (s *Server) RemoveSDNVNet(vnet string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.vnets = slices.DeleteFunc(s.vnets, func(v pve.SDNVNet) bool { return v.VNet == vnet })
	delete(s.subnets, vnet)
}

// SetSDNSubnets sets the CIDRs a vnet lists.
func (s *Server) SetSDNSubnets(vnet string, cidrs ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.subnets[vnet] = slices.Clone(cidrs)
}

// DenySDNAudit makes SDN reads answer empty lists and drops SDN.Audit from /sdn, as PVE does without it.
func (s *Server) DenySDNAudit(deny bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sdnDeny = deny
}

// AddForeignGuest adds a guest with the given config and snapshot configs, listed in /cluster/resources.
func (s *Server) AddForeignGuest(g pve.GuestRef, cfg map[string]string, snapshots map[string]map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweep()
	if g.Kind == "lxc" {
		s.cts[g.VMID] = g.Node
		s.lxc[g.VMID] = &lxcGuest{config: maps.Clone(cfg), snaps: maps.Clone(snapshots)}
		return
	}
	s.vms[g.VMID] = &vm{node: g.Node, status: "stopped", firewall: map[string]string{}, ipsets: map[string][]string{},
		config: maps.Clone(cfg), snaps: maps.Clone(snapshots)}
}

// SetPendingUnplug makes the next n delete=<key> writes on vmid leave the key pending.
func (s *Server) SetPendingUnplug(vmid int, key string, n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := s.vms[vmid]
	if v.unplug == nil {
		v.unplug = map[string]int{}
	}
	v.unplug[key] = n
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

// FinishTasks finishes every running task now, in start order.
func (s *Server) FinishTasks() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.sweepAt(now.Add(1 << 62))
	for _, tk := range s.tasks {
		if tk.ready.After(now) {
			tk.ready = now
		}
	}
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

// SetClusterFirewall sets the datacenter firewall switch; it starts enabled.
func (s *Server) SetClusterFirewall(enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clusterFWOff = !enabled
}

// FailNextRuleWrite fails the next rule add or delete with a 500.
func (s *Server) FailNextRuleWrite() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failRule = true
}

// ConflictNextRuleDelete fails the next rule delete as if the config changed since it was read.
func (s *Server) ConflictNextRuleDelete() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.conflictDel = true
}

// OnFirewallWrite runs fn after each successful VM firewall rule or options write.
func (s *Server) OnFirewallWrite(fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onFWWrite = fn
}

// OnIPSetWrite runs fn after each ipset entry add or delete, before the response is sent.
func (s *Server) OnIPSetWrite(fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onIPSetWrite = fn
}

// IPSetHolders lists the VMIDs whose ipset name contains ip, ascending.
func (s *Server) IPSetHolders(ip, name string) []int {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []int
	for id, v := range s.vms {
		if slices.ContainsFunc(v.ipsets[name], func(c string) bool { return strings.Split(c, "/")[0] == ip }) {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return out
}

// OnRuleAdd lets fn change each added rule before it is stored, like a Proxmox normalisation.
func (s *Server) OnRuleAdd(fn func(*pve.FirewallRule)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onRuleAdd = fn
}

// RequireRuleDigest makes rule deletes without a digest fail.
func (s *Server) RequireRuleDigest() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.needDigest = true
}

// fwDigest stands in for the digest of a VM's .fw file: options, ipsets and rules.
func fwDigest(v *vm) string {
	sum := sha256.Sum256(fmt.Appendf(nil, "%v|%v|%+v", v.firewall, v.ipsets, v.rules))
	return fmt.Sprintf("%x", sum[:20])
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
	f.Rules = slices.Clone(v.rules)
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
		ok := r.Header.Get("Authorization") == "PVEAPIToken="+s.tokenID+"="+s.secret
		for id, sec := range s.extra {
			ok = ok || r.Header.Get("Authorization") == "PVEAPIToken="+id+"="+sec
		}
		s.mu.Unlock()
		if down {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"data": nil, "message": "proxmox down"})
			return
		}
		if !ok {
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
		if r.Method != http.MethodGet && (strings.HasSuffix(path, "/firewall/options") || strings.Contains(path, "/firewall/rules")) {
			s.mu.Lock()
			fn := s.onFWWrite
			s.mu.Unlock()
			if fn != nil {
				fn()
			}
		}
		if r.Method != http.MethodGet && strings.Contains(path, "/firewall/ipset/") {
			s.mu.Lock()
			fn := s.onIPSetWrite
			s.mu.Unlock()
			if fn != nil {
				fn()
			}
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

func digestOf(v *vm) string { return fmt.Sprintf("fake%d", v.rev) }

func isVirtio(k string) bool {
	n, ok := strings.CutPrefix(k, "virtio")
	if !ok || n == "" {
		return false
	}
	_, err := strconv.Atoi(n)
	return err == nil
}

// diskHotplug reports whether hotplug includes disk; a missing key means Proxmox's default.
func diskHotplug(v *vm) bool {
	h, ok := v.config["hotplug"]
	if !ok {
		h = "network,disk,usb"
	}
	return slices.Contains(strings.Split(h, ","), "disk")
}

func isNet(k string) bool {
	n, ok := strings.CutPrefix(k, "net")
	if !ok || n == "" {
		return false
	}
	_, err := strconv.Atoi(n)
	return err == nil
}

// netHotplug reports whether hotplug includes network; a missing key means Proxmox's default.
func netHotplug(v *vm) bool {
	h, ok := v.config["hotplug"]
	if !ok {
		h = "network,disk,usb"
	}
	return slices.Contains(strings.Split(h, ","), "network")
}

func (v *vm) setPending(k string, pc pendingChange) {
	if v.pending == nil {
		v.pending = map[string]pendingChange{}
	}
	v.pending[k] = pc
}

// lostVMIDExit is the exitstatus PVE gives a create or clone task whose VMID another VM took meanwhile:
// a lock timeout while that VM still holds its config lock, else already exists.
func lostVMIDExit(cur *vm, vmid int) string {
	if cur != nil && cur.config["lock"] != "" {
		return fmt.Sprintf("can't lock file '/var/lock/qemu-server/lock-%d.conf' - got timeout", vmid)
	}
	return fmt.Sprintf("ERROR: unable to create VM %d: config file already exists", vmid)
}

// storageOf returns the storage on nodeName holding volid; caller holds s.mu.
func (s *Server) storageOf(nodeName, volid string) (*storage, bool) {
	n, ok := s.nodes[nodeName]
	if !ok {
		return nil, false
	}
	id, _, _ := strings.Cut(volid, ":")
	st, ok := n.storages[id]
	if !ok || st.vols[volid] == nil {
		return nil, false
	}
	return st, true
}

// file reports whether the storage keeps disks as files under a vmid directory.
func (st *storage) file() bool {
	switch st.typ {
	case "dir", "nfs", "cifs", "glusterfs", "btrfs", "cephfs":
		return true
	}
	return false
}

// dropOwnedVolumes removes volumes named vm-<vmid>-*; others stay, as in PVE. Caller holds s.mu.
func (s *Server) dropOwnedVolumes(vmid int) {
	pre := fmt.Sprintf("vm-%d-", vmid)
	for _, n := range s.nodes {
		for sid, st := range n.storages {
			for id := range st.vols {
				if strings.HasPrefix(id, sid+":"+pre) || strings.HasPrefix(id, fmt.Sprintf("%s:%d/%s", sid, vmid, pre)) {
					delete(st.vols, id)
				}
			}
		}
	}
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
	s.handle(mux, "GET /access/permissions", false, func(r *http.Request, _ bool) (any, *apiErr) {
		s.mu.Lock()
		defer s.mu.Unlock()
		path := r.URL.Query().Get("path")
		out := map[string]map[string]int{}
		if p, ok := s.perms[path]; ok {
			out[path] = maps.Clone(p)
		} else {
			out[path] = map[string]int{}
			for _, p := range allPrivs {
				if p != "Datastore.Allocate" || !s.DenyDatastoreAllocate || !strings.HasPrefix(path, "/storage/") {
					out[path][p] = 1
				}
			}
		}
		if s.sdnDeny && strings.HasPrefix(path, "/sdn") {
			delete(out[path], "SDN.Audit")
		}
		if p, ok := s.perms["/"]; ok && path != "/" {
			out["/"] = maps.Clone(p)
		}
		return out, nil
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
		if f["archive"] != "" {
			return s.restoreArchive(n, id, f, failed)
		}
		if failed {
			return s.newTask(n.name, "qmcreate", id, failPtr(true), nil), nil
		}
		cfg := map[string]string{"lock": "create"}
		nv := &vm{node: n.name, status: "stopped", firewall: map[string]string{}, ipsets: map[string][]string{}, config: cfg}
		s.vms[id] = nv
		return s.newTask(n.name, "qmcreate", id, nil, func() string {
			if cur := s.vms[id]; cur != nv {
				return lostVMIDExit(cur, id)
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
	s.routesVzdump(mux, form, nodeFor)
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
			typ := st.typ
			if typ == "" {
				typ = "lvmthin"
			}
			out = append(out, map[string]any{"storage": id, "type": typ, "content": strings.Join(st.content, ","), "shared": shared,
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
		for id, n := range s.cts {
			out = append(out, map[string]any{"id": fmt.Sprintf("lxc/%d", id), "type": "lxc", "vmid": id, "node": n,
				"name": fmt.Sprintf("ct%d", id), "status": "running", "template": 0})
		}
		sort.Slice(out, func(i, j int) bool { return out[i]["vmid"].(int) < out[j]["vmid"].(int) })
		return out, nil
	})
	s.handle(mux, "GET /cluster/sdn/zones", false, func(*http.Request, bool) (any, *apiErr) {
		s.mu.Lock()
		defer s.mu.Unlock()
		out := []map[string]any{}
		for _, z := range s.zones {
			if s.sdnDeny {
				break
			}
			row := map[string]any{"zone": z.Zone, "type": z.Type, "digest": "fake"}
			if z.MTU != 0 {
				row["mtu"] = z.MTU
			}
			if len(z.Nodes) > 0 {
				row["nodes"] = strings.Join(z.Nodes, ",")
			}
			out = append(out, row)
		}
		return out, nil
	})
	s.handle(mux, "GET /cluster/sdn/vnets", false, func(*http.Request, bool) (any, *apiErr) {
		s.mu.Lock()
		defer s.mu.Unlock()
		out := []map[string]any{}
		for _, n := range s.vnets {
			if s.sdnDeny {
				break
			}
			row := map[string]any{"vnet": n.VNet, "zone": n.Zone, "type": "vnet", "digest": "fake"}
			if n.VLANAware {
				row["vlanaware"] = 1
			}
			out = append(out, row)
		}
		return out, nil
	})
	s.handle(mux, "GET /cluster/sdn/vnets/{vnet}/subnets", false, func(r *http.Request, _ bool) (any, *apiErr) {
		s.mu.Lock()
		defer s.mu.Unlock()
		out := []map[string]any{}
		if !s.sdnDeny {
			for _, c := range s.subnets[r.PathValue("vnet")] {
				out = append(out, map[string]any{"subnet": c, "cidr": c, "type": "subnet", "vnet": r.PathValue("vnet")})
			}
		}
		return out, nil
	})
	// lxcFor returns the container on the path's node; caller holds s.mu.
	lxcFor := func(r *http.Request) (*lxcGuest, *apiErr) {
		id, _ := strconv.Atoi(r.PathValue("vmid"))
		if g, ok := s.lxc[id]; ok && s.cts[id] == r.PathValue("node") {
			return g, nil
		}
		return nil, notExist(id, r.PathValue("node"))
	}
	snapList := func(snaps map[string]map[string]string) []map[string]any {
		out := []map[string]any{}
		for _, n := range slices.Sorted(maps.Keys(snaps)) {
			out = append(out, map[string]any{"name": n})
		}
		return append(out, map[string]any{"name": "current"})
	}
	snapConfig := func(snaps map[string]map[string]string, name string) (any, *apiErr) {
		cfg, ok := snaps[name]
		if !ok {
			return nil, &apiErr{500, "snapshot '" + name + "' does not exist"}
		}
		out := map[string]any{}
		for k, val := range cfg {
			out[k] = val
		}
		return out, nil
	}
	s.handle(mux, "GET /nodes/{node}/lxc/{vmid}/config", false, func(r *http.Request, _ bool) (any, *apiErr) {
		s.mu.Lock()
		defer s.mu.Unlock()
		g, e := lxcFor(r)
		if e != nil {
			return nil, e
		}
		out := map[string]any{"digest": "fake"}
		for k, val := range g.config {
			out[k] = val
		}
		return out, nil
	})
	s.handle(mux, "GET /nodes/{node}/lxc/{vmid}/pending", false, func(r *http.Request, _ bool) (any, *apiErr) {
		s.mu.Lock()
		defer s.mu.Unlock()
		g, e := lxcFor(r)
		if e != nil {
			return nil, e
		}
		out := []map[string]any{}
		for _, k := range slices.Sorted(maps.Keys(g.config)) {
			out = append(out, map[string]any{"key": k, "value": g.config[k]})
		}
		return out, nil
	})
	s.handle(mux, "GET /nodes/{node}/lxc/{vmid}/snapshot", false, func(r *http.Request, _ bool) (any, *apiErr) {
		s.mu.Lock()
		defer s.mu.Unlock()
		g, e := lxcFor(r)
		if e != nil {
			return nil, e
		}
		return snapList(g.snaps), nil
	})
	s.handle(mux, "GET /nodes/{node}/lxc/{vmid}/snapshot/{name}/config", false, func(r *http.Request, _ bool) (any, *apiErr) {
		s.mu.Lock()
		defer s.mu.Unlock()
		g, e := lxcFor(r)
		if e != nil {
			return nil, e
		}
		return snapConfig(g.snaps, r.PathValue("name"))
	})
	s.handle(mux, "GET /nodes/{node}/qemu/{vmid}/snapshot", false, func(r *http.Request, _ bool) (any, *apiErr) {
		s.mu.Lock()
		defer s.mu.Unlock()
		v, _, e := vmFor(r)
		if e != nil {
			return nil, e
		}
		return snapList(v.snaps), nil
	})
	s.handle(mux, "GET /nodes/{node}/qemu/{vmid}/snapshot/{name}/config", false, func(r *http.Request, _ bool) (any, *apiErr) {
		s.mu.Lock()
		defer s.mu.Unlock()
		v, _, e := vmFor(r)
		if e != nil {
			return nil, e
		}
		return snapConfig(v.snaps, r.PathValue("name"))
	})
	s.handle(mux, "DELETE /nodes/{node}/qemu/{vmid}/firewall/ipset/{name}", false, func(r *http.Request, _ bool) (any, *apiErr) {
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
		if len(entries) > 0 {
			return nil, &apiErr{500, "IPSet '" + name + "' is not empty"}
		}
		delete(v.ipsets, name)
		return nil, nil
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
			if cur := s.vms[newID]; cur != nv {
				return lostVMIDExit(cur, newID)
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
		if d := f["digest"]; d != "" && d != digestOf(v) {
			return nil, &apiErr{500, "detected modified configuration - file changed by other user? Try again. (digest mismatch)"}
		}
		hot := diskHotplug(v)
		netHot := v.status != "running" || netHotplug(v)
		for k, val := range f {
			if k == "delete" || k == "digest" {
				continue
			}
			if isVirtio(k) && !hot || isNet(k) && !netHot {
				v.setPending(k, pendingChange{value: val})
				continue
			}
			v.config[k] = val
			delete(v.pending, k)
		}
		for _, k := range strings.Split(f["delete"], ",") {
			k = strings.TrimSpace(k)
			if _, has := v.config[k]; has && (isVirtio(k) && !hot || isNet(k) && !netHot || v.unplug[k] > 0) {
				if v.unplug[k] > 0 {
					v.unplug[k]--
				}
				v.setPending(k, pendingChange{del: true})
				continue
			}
			delete(v.config, k)
			delete(v.pending, k)
		}
		v.rev++
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
		out := map[string]any{"digest": digestOf(v)}
		for k, val := range v.config {
			if n, err := strconv.Atoi(val); err == nil && k != "name" {
				out[k] = n
			} else {
				out[k] = val
			}
		}
		return out, nil
	})
	s.handle(mux, "GET /nodes/{node}/qemu/{vmid}/pending", false, func(r *http.Request, _ bool) (any, *apiErr) {
		s.mu.Lock()
		defer s.mu.Unlock()
		v, _, e := vmFor(r)
		if e != nil {
			return nil, e
		}
		keys := map[string]bool{}
		for k := range v.config {
			keys[k] = true
		}
		for k := range v.pending {
			keys[k] = true
		}
		out := []map[string]any{}
		for _, k := range slices.Sorted(maps.Keys(keys)) {
			row := map[string]any{"key": k}
			if cur, ok := v.config[k]; ok {
				row["value"] = cur
			}
			if pc, ok := v.pending[k]; ok {
				if pc.del {
					row["delete"] = 1
				} else {
					row["pending"] = pc.value
				}
			}
			out = append(out, row)
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
		newGB, _ := strconv.Atoi(strings.TrimSuffix(f["size"], "G"))
		if newGB < oldGB {
			return nil, &apiErr{500, "shrinking disks is not supported"}
		}
		// PVE 8+ grows the disk in a task.
		disk, size := f["disk"], f["size"]
		return s.newTask(v.node, "resize", id, nil, func() string {
			if s.vms[id] != v {
				return fmt.Sprintf("VM %d does not exist", id)
			}
			v.config[disk] = strings.Join(keep, ",") + ",size=" + size
			v.rev++
			volid, _, _ := strings.Cut(cur, ",")
			if st, ok := s.storageOf(v.node, volid); ok {
				st.vols[volid].size = int64(newGB) << 30
			}
			return "OK"
		}), nil
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
	s.handle(mux, "GET /cluster/firewall/options", false, func(r *http.Request, _ bool) (any, *apiErr) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.clusterFWOff {
			return map[string]any{}, nil
		}
		return map[string]any{"enable": 1}, nil
	})
	s.handle(mux, "GET /nodes/{node}/qemu/{vmid}/firewall/rules", false, func(r *http.Request, _ bool) (any, *apiErr) {
		s.mu.Lock()
		defer s.mu.Unlock()
		v, _, e := vmFor(r)
		if e != nil {
			return nil, e
		}
		out := []map[string]any{}
		for i, x := range v.rules {
			m := map[string]any{"pos": i, "type": x.Type, "action": x.Action, "enable": 0, "digest": fwDigest(v)}
			if x.Enable {
				m["enable"] = 1
			}
			for k, val := range map[string]string{"iface": x.Iface, "proto": x.Proto, "dport": x.Dport, "source": x.Source, "dest": x.Dest, "comment": x.Comment} {
				if val != "" {
					m[k] = val
				}
			}
			out = append(out, m)
		}
		return out, nil
	})
	s.handle(mux, "POST /nodes/{node}/qemu/{vmid}/firewall/rules", false, func(r *http.Request, _ bool) (any, *apiErr) {
		f := form(r)
		s.mu.Lock()
		defer s.mu.Unlock()
		v, _, e := vmFor(r)
		if e != nil {
			return nil, e
		}
		if s.failRule {
			s.failRule = false
			return nil, &apiErr{500, "injected rule write failure"}
		}
		if f["type"] != "in" && f["type"] != "out" || f["action"] == "" {
			return nil, &apiErr{400, "parameter verification failed: type/action"}
		}
		rule := pve.FirewallRule{Type: f["type"], Action: f["action"], Iface: f["iface"], Proto: f["proto"], Dport: f["dport"],
			Source: f["source"], Dest: f["dest"], Comment: f["comment"], Enable: f["enable"] == "1"}
		if s.onRuleAdd != nil {
			s.onRuleAdd(&rule)
		}
		// Real PVE inserts new rules at the top and ignores pos.
		v.rules = slices.Insert(v.rules, 0, rule)
		return nil, nil
	})
	s.handle(mux, "DELETE /nodes/{node}/qemu/{vmid}/firewall/rules/{pos}", false, func(r *http.Request, _ bool) (any, *apiErr) {
		s.mu.Lock()
		defer s.mu.Unlock()
		v, _, e := vmFor(r)
		if e != nil {
			return nil, e
		}
		if s.failRule {
			s.failRule = false
			return nil, &apiErr{500, "injected rule write failure"}
		}
		conflict := s.conflictDel
		s.conflictDel = false
		if d := r.URL.Query().Get("digest"); conflict || d != "" && d != fwDigest(v) || d == "" && s.needDigest {
			return nil, &apiErr{500, "detected modified configuration - file changed by other user? Try again."}
		}
		pos, err := strconv.Atoi(r.PathValue("pos"))
		if err != nil || pos < 0 || pos >= len(v.rules) {
			return nil, &apiErr{500, "no such rule"}
		}
		v.rules = slices.Delete(v.rules, pos, pos+1)
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
			s.dropOwnedVolumes(id)
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
	s.handle(mux, "POST /nodes/{node}/storage/{storage}/content", false, func(r *http.Request, _ bool) (any, *apiErr) {
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
		vmid, _ := strconv.Atoi(f["vmid"])
		gb, err := strconv.Atoi(strings.TrimSuffix(f["size"], "G"))
		if vmid <= 0 || f["filename"] == "" || err != nil || gb <= 0 || !strings.HasSuffix(f["size"], "G") {
			return nil, &apiErr{400, "parameter verification failed: vmid/filename/size"}
		}
		volid := r.PathValue("storage") + ":" + f["filename"]
		if st.file() {
			if ext := path.Ext(f["filename"]); ext != ".raw" && ext != ".qcow2" {
				return nil, &apiErr{500, "unable to parse volume filename '" + f["filename"] + "'"}
			}
			volid = r.PathValue("storage") + ":" + strconv.Itoa(vmid) + "/" + f["filename"]
		}
		if st.vols[volid] != nil {
			return nil, &apiErr{500, "unable to create volume '" + volid + "': volume already exists"}
		}
		if st.vols == nil {
			st.vols = map[string]*volume{}
		}
		st.vols[volid] = &volume{size: int64(gb) << 30, vmid: vmid}
		return volid, nil
	})
	s.handle(mux, "GET /nodes/{node}/storage/{storage}/content", false, func(r *http.Request, _ bool) (any, *apiErr) {
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
		want, _ := strconv.Atoi(r.URL.Query().Get("vmid"))
		if r.URL.Query().Get("content") == "backup" {
			return s.listBackups(r.PathValue("storage"), want), nil
		}
		out := []map[string]any{}
		for _, id := range slices.Sorted(maps.Keys(st.vols)) {
			if vol := st.vols[id]; want <= 0 || vol.vmid == want {
				out = append(out, map[string]any{"volid": id, "size": vol.size, "vmid": vol.vmid, "format": "raw", "content": "images"})
			}
		}
		return out, nil
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
		if s.backups[id] != nil {
			delete(s.backups, id)
			return s.newTask(n.name, "imgdel", 0, nil, nil), nil
		}
		if s.DenyDatastoreAllocate {
			return nil, &apiErr{403, "Permission check failed (/storage/" + r.PathValue("storage") + ", Datastore.Allocate)"}
		}
		if st, ok := n.storages[r.PathValue("storage")]; ok && st.vols[id] != nil {
			delete(st.vols, id)
			return s.newTask(n.name, "imgdel", 0, nil, nil), nil
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

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

package fake

import (
	"fmt"
	"maps"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nebla-cloud/nebla/internal/pve"
)

// archive is one vzdump backup; config has the excluded disks already dropped.
type archive struct {
	vmid     int
	ctime    time.Time
	notes    string
	config   map[string]string
	excluded []string // dropped disk lines, "virtioN: value"
	firewall map[string]string
	ipsets   map[string][]string
	rules    []pve.FirewallRule
}

const archiveSize = 1 << 30

var diskKey = regexp.MustCompile(`^(scsi|virtio|sata|ide)\d+$`)

func noBackup(value string) bool {
	return slices.ContainsFunc(strings.Split(value, ","), func(p string) bool {
		return p == "backup=0" || p == "backup=no" || p == "backup=false"
	})
}

func (s *Server) routesVzdump(mux *http.ServeMux, form func(*http.Request) map[string]string, nodeFor func(*http.Request) (*node, *apiErr)) {
	s.handle(mux, "POST /nodes/{node}/vzdump", true, func(r *http.Request, failed bool) (any, *apiErr) {
		f := form(r)
		s.mu.Lock()
		defer s.mu.Unlock()
		n, e := nodeFor(r)
		if e != nil {
			return nil, e
		}
		id, _ := strconv.Atoi(f["vmid"])
		v, ok := s.vms[id]
		if !ok || v.node != n.name {
			return nil, notExist(id, n.name)
		}
		if e := locked(v, id); e != nil {
			return nil, e
		}
		if _, ok := n.storages[f["storage"]]; !ok {
			return nil, &apiErr{500, "storage '" + f["storage"] + "' does not exist"}
		}
		if f["mode"] != "snapshot" || f["remove"] != "0" {
			return nil, &apiErr{400, "parameter verification failed: mode/remove"}
		}
		if failed || s.FailVzdump {
			return s.newTask(n.name, "vzdump", id, &failure{status: 500}, nil), nil
		}
		a := &archive{vmid: id, notes: f["notes-template"], config: map[string]string{}, firewall: maps.Clone(v.firewall),
			ipsets: map[string][]string{}, rules: slices.Clone(v.rules)}
		for k, val := range v.config {
			if diskKey.MatchString(k) && noBackup(val) {
				a.excluded = append(a.excluded, k+": "+val)
				continue
			}
			a.config[k] = val
		}
		slices.Sort(a.excluded)
		for k, val := range v.ipsets {
			a.ipsets[k] = slices.Clone(val)
		}
		storageID := f["storage"]
		return s.newTask(n.name, "vzdump", id, nil, func() string {
			if s.backups == nil {
				s.backups = map[string]*archive{}
			}
			at := time.Now().UTC().Truncate(time.Second)
			volid := backupVolid(storageID, id, at)
			for s.backups[volid] != nil {
				at = at.Add(time.Second)
				volid = backupVolid(storageID, id, at)
			}
			a.ctime = at
			s.backups[volid] = a
			return "OK"
		}), nil
	})
}

func backupVolid(storage string, vmid int, at time.Time) string {
	return fmt.Sprintf("%s:backup/vm/%d/%s", storage, vmid, at.Format(time.RFC3339))
}

// restoreArchive handles POST /nodes/{node}/qemu with archive set; caller holds s.mu.
func (s *Server) restoreArchive(n *node, id int, f map[string]string, failed bool) (any, *apiErr) {
	a := s.backups[f["archive"]]
	if a == nil {
		return nil, &apiErr{500, "volume '" + f["archive"] + "' does not exist"}
	}
	if _, ok := n.storages[f["storage"]]; !ok {
		return nil, &apiErr{500, "storage '" + f["storage"] + "' does not exist"}
	}
	if f["unique"] != "1" {
		return nil, &apiErr{400, "parameter verification failed: unique"}
	}
	if failed || s.FailRestore {
		return s.newTask(n.name, "qmrestore", id, &failure{status: 500}, nil), nil
	}
	nv := &vm{node: n.name, status: "stopped", firewall: map[string]string{}, ipsets: map[string][]string{},
		config: map[string]string{"lock": "create"}}
	s.vms[id] = nv
	return s.newTask(n.name, "qmrestore", id, nil, func() string {
		if cur := s.vms[id]; cur != nv {
			return lostVMIDExit(cur, id)
		}
		cfg := map[string]string{}
		for k, val := range a.config {
			if k == "lock" {
				continue
			}
			if diskKey.MatchString(k) || k == "efidisk0" || k == "tpmstate0" {
				val = strings.ReplaceAll(val, fmt.Sprintf("vm-%d-", a.vmid), fmt.Sprintf("vm-%d-", id))
			}
			cfg[k] = val
		}
		if len(a.excluded) > 0 {
			cfg["description"] += "\n" + strings.Join(a.excluded, "\n")
		}
		if nic, ok := cfg["net0"]; ok {
			cfg["net0"] = newMAC(nic, s.seq)
		}
		nv.config = cfg
		nv.firewall = maps.Clone(a.firewall)
		for k, val := range a.ipsets {
			nv.ipsets[k] = slices.Clone(val)
		}
		nv.rules = slices.Clone(a.rules)
		return "OK"
	}), nil
}

// newMAC swaps the MAC in a net value for one derived from seq.
func newMAC(nic string, seq int) string {
	parts := strings.Split(nic, ",")
	mac := fmt.Sprintf("BC:24:11:%02X:%02X:%02X", seq>>16&0xff, seq>>8&0xff, seq&0xff)
	for i, p := range parts {
		if m, v, ok := strings.Cut(p, "="); ok && strings.Count(v, ":") == 5 {
			parts[i] = m + "=" + mac
			return strings.Join(parts, ",")
		}
	}
	return nic
}

// listBackups returns the archives visible on a node's storage; caller holds s.mu.
func (s *Server) listBackups(storageID string, vmid int) []map[string]any {
	out := []map[string]any{}
	for _, id := range slices.Sorted(maps.Keys(s.backups)) {
		a := s.backups[id]
		if !strings.HasPrefix(id, storageID+":") || (vmid > 0 && a.vmid != vmid) {
			continue
		}
		out = append(out, map[string]any{"volid": id, "vmid": a.vmid, "size": archiveSize, "ctime": a.ctime.Unix(),
			"notes": a.notes, "format": "pbs-vm", "content": "backup"})
	}
	return out
}

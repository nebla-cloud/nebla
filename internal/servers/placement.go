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

package servers

import (
	"net/http"

	"github.com/nebla-cloud/nebla/internal/apierr"
	"github.com/nebla-cloud/nebla/internal/catalog"
)

// NodeLoad is a node's memory picture; Reserved is the memory of panel servers placed on it. All in bytes.
type NodeLoad struct {
	Name     string
	Enabled  bool
	Status   string
	MaxMem   int64
	Mem      int64
	Reserved int64
}

func (n NodeLoad) free() int64 { return n.MaxMem - n.Mem - n.Reserved }

var errNoCapacity = apierr.New(http.StatusUnprocessableEntity, "resource_unavailable", "no node in this location has capacity for this server")

// Place picks the usable node with a template and the most free memory; a node's own template beats a shared one.
func Place(nodes []NodeLoad, tmpls []catalog.Template, t catalog.ServerType) (string, int, error) {
	shared := 0
	local := map[string]int{}
	for _, tp := range tmpls {
		switch {
		case tp.Node == "":
			if shared == 0 {
				shared = tp.VMID
			}
		case local[tp.Node] == 0:
			local[tp.Node] = tp.VMID
		}
	}
	need := int64(t.MemoryMB) << 20
	var best *NodeLoad
	bestVMID := 0
	for i := range nodes {
		n := &nodes[i]
		if !n.Enabled || n.Status != "online" || n.free() < need {
			continue
		}
		vmid := local[n.Name]
		if vmid == 0 {
			vmid = shared
		}
		if vmid == 0 {
			continue
		}
		if best == nil || n.free() > best.free() || (n.free() == best.free() && n.Name < best.Name) {
			best, bestVMID = n, vmid
		}
	}
	if best == nil {
		return "", 0, errNoCapacity
	}
	return best.Name, bestVMID, nil
}

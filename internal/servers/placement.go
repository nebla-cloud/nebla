package servers

import (
	"net/http"

	"github.com/Hexeption/nebla/internal/apierr"
	"github.com/Hexeption/nebla/internal/catalog"
)

// NodeLoad is a node's memory picture; Reserved is the memory of panel servers placed on it. All in bytes.
type NodeLoad struct {
	Name    string
	Enabled bool
	Status  string
	MaxMem  int64
	Mem     int64
	// Reserved is the memory of panel servers placed on the node.
	Reserved int64
}

func (n NodeLoad) free() int64 { return n.MaxMem - n.Mem - n.Reserved }

var errNoCapacity = apierr.New(http.StatusUnprocessableEntity, "resource_unavailable", "no node in this location has capacity for this server")

// Place picks the usable node with a template and the most free memory, ties to the lowest name.
// A node-specific template wins over a shared one on that node.
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

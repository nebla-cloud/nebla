package servers_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/Hexeption/nebla/internal/apierr"
	"github.com/Hexeption/nebla/internal/catalog"
	"github.com/Hexeption/nebla/internal/servers"
	"github.com/stretchr/testify/require"
)

const gib = int64(1) << 30

func online(name string, maxGiB, usedGiB, reservedGiB int64) servers.NodeLoad {
	return servers.NodeLoad{Name: name, Enabled: true, Status: "online", MaxMem: maxGiB * gib, Mem: usedGiB * gib, Reserved: reservedGiB * gib}
}

func TestPlaceMostFreeMemory(t *testing.T) {
	shared := []catalog.Template{{Node: "", VMID: 9000}}
	typ := catalog.ServerType{MemoryMB: 2048}
	cases := []struct {
		name  string
		nodes []servers.NodeLoad
		want  string
	}{
		{"most free wins", []servers.NodeLoad{online("a", 64, 40, 0), online("b", 64, 10, 0), online("c", 64, 20, 0)}, "b"},
		{"reserved counts", []servers.NodeLoad{online("a", 64, 10, 30), online("b", 64, 20, 0)}, "b"},
		// a: 30 GiB used (running VMs are in mem) + 0 reserved; b: 10 used + 25 reserved for not-yet-running servers.
		{"mem and reserved both subtract", []servers.NodeLoad{online("a", 64, 30, 0), online("b", 64, 10, 25)}, "a"},
		{"tie lowest name", []servers.NodeLoad{online("pve2", 64, 10, 0), online("pve1", 64, 10, 0)}, "pve1"},
		{"skips disabled", []servers.NodeLoad{{Name: "a", Enabled: false, Status: "online", MaxMem: 64 * gib}, online("b", 64, 60, 0)}, "b"},
		{"skips offline", []servers.NodeLoad{{Name: "a", Enabled: true, Status: "offline", MaxMem: 64 * gib}, online("b", 64, 60, 0)}, "b"},
		{"skips missing", []servers.NodeLoad{{Name: "a", Enabled: true, Status: catalog.NodeMissing, MaxMem: 64 * gib}, online("b", 64, 60, 0)}, "b"},
		{"skips node too full", []servers.NodeLoad{online("a", 64, 63, 0), online("b", 8, 2, 0)}, "b"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			node, vmid, err := servers.Place(tc.nodes, shared, typ)
			require.NoError(t, err)
			require.Equal(t, tc.want, node)
			require.Equal(t, 9000, vmid)
		})
	}
}

func TestPlaceSkipsNodeWithoutLocalTemplate(t *testing.T) {
	typ := catalog.ServerType{MemoryMB: 1024}
	nodes := []servers.NodeLoad{online("a", 64, 1, 0), online("b", 64, 30, 0), online("c", 64, 20, 0)}

	node, vmid, err := servers.Place(nodes, []catalog.Template{{Node: "b", VMID: 9100}, {Node: "c", VMID: 9200}}, typ)
	require.NoError(t, err)
	require.Equal(t, "c", node, "a has no template and b has less free memory")
	require.Equal(t, 9200, vmid)

	// A node-local template beats the shared one on that node; other nodes use the shared one.
	node, vmid, err = servers.Place(nodes, []catalog.Template{{Node: "", VMID: 9000}, {Node: "a", VMID: 9001}}, typ)
	require.NoError(t, err)
	require.Equal(t, "a", node)
	require.Equal(t, 9001, vmid)
	node, vmid, err = servers.Place(nodes[1:], []catalog.Template{{Node: "", VMID: 9000}, {Node: "a", VMID: 9001}}, typ)
	require.NoError(t, err)
	require.Equal(t, "c", node)
	require.Equal(t, 9000, vmid)
}

func TestPlaceNoCapacity(t *testing.T) {
	typ := catalog.ServerType{MemoryMB: 8192}
	shared := []catalog.Template{{VMID: 9000}}
	cases := map[string]struct {
		nodes []servers.NodeLoad
		tmpls []catalog.Template
	}{
		"all full":         {[]servers.NodeLoad{online("a", 16, 10, 0), online("b", 16, 4, 6)}, shared},
		"no nodes":         {nil, shared},
		"no templates":     {[]servers.NodeLoad{online("a", 64, 0, 0)}, nil},
		"template offline": {[]servers.NodeLoad{online("a", 64, 0, 0), {Name: "b", Status: "offline", Enabled: true, MaxMem: 64 * gib}}, []catalog.Template{{Node: "b", VMID: 1}}},
		"overcommitted":    {[]servers.NodeLoad{online("a", 16, 10, 20)}, shared},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, _, err := servers.Place(tc.nodes, tc.tmpls, typ)
			var e apierr.E
			require.True(t, errors.As(err, &e), "want apierr, got %v", err)
			require.Equal(t, http.StatusUnprocessableEntity, e.Status)
			require.Equal(t, "resource_unavailable", e.Code)
		})
	}
	// Exactly enough memory fits.
	node, _, err := servers.Place([]servers.NodeLoad{online("a", 16, 4, 4)}, shared, typ)
	require.NoError(t, err)
	require.Equal(t, "a", node)
}

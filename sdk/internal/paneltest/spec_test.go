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

package paneltest

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"
	"github.com/nebla-cloud/nebla/sdk/nebla"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	specPath    = "../../../api/openapi.yaml"
	fixturesDir = "../../nebla/testdata"
)

type call func(context.Context, *nebla.Client) error

func one[T any](_ T, err error) error { return err }

func two[A, B any](_ A, _ B, err error) error { return err }

var (
	srvRef   = &nebla.FirewallResourceServer{ID: 1}
	fwTarget = []nebla.FirewallResource{{Type: nebla.FirewallResourceTypeServer, Server: srvRef}}
)

// calls maps each project-scoped operationId to an SDK call that exercises it.
var calls = map[string]call{
	"getLocations": func(ctx context.Context, c *nebla.Client) error { return two(c.Locations.List(ctx, nebla.ListOpts{})) },
	"getLocation":  func(ctx context.Context, c *nebla.Client) error { return one(c.Locations.Get(ctx, 1)) },
	"getServerTypes": func(ctx context.Context, c *nebla.Client) error {
		return two(c.ServerTypes.List(ctx, nebla.ListOpts{}))
	},
	"getServerType": func(ctx context.Context, c *nebla.Client) error { return one(c.ServerTypes.Get(ctx, 1)) },
	"getImages": func(ctx context.Context, c *nebla.Client) error {
		return two(c.Images.List(ctx, nebla.ImageListOpts{}))
	},
	"getImage":   func(ctx context.Context, c *nebla.Client) error { return one(c.Images.Get(ctx, 1)) },
	"getSshKeys": func(ctx context.Context, c *nebla.Client) error { return two(c.SSHKeys.List(ctx, nebla.ListOpts{})) },
	"postSshKeys": func(ctx context.Context, c *nebla.Client) error {
		return one(c.SSHKeys.Create(ctx, nebla.SSHKeyCreateOpts{Name: "k", PublicKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIA k"}))
	},
	"getSshKey": func(ctx context.Context, c *nebla.Client) error { return one(c.SSHKeys.Get(ctx, 1)) },
	"putSshKey": func(ctx context.Context, c *nebla.Client) error {
		return one(c.SSHKeys.Update(ctx, 1, nebla.SSHKeyUpdateOpts{Name: nebla.Ptr("k2")}))
	},
	"deleteSshKey": func(ctx context.Context, c *nebla.Client) error { return c.SSHKeys.Delete(ctx, 1) },

	"getFirewalls": func(ctx context.Context, c *nebla.Client) error {
		return two(c.Firewalls.List(ctx, nebla.FirewallListOpts{}))
	},
	"postFirewalls": func(ctx context.Context, c *nebla.Client) error {
		return one(c.Firewalls.Create(ctx, nebla.FirewallCreateOpts{Name: "fw", Labels: map[string]string{"a": "b"},
			Rules: []nebla.FirewallRule{{Direction: nebla.FirewallDirectionIn, Protocol: nebla.FirewallProtocolTCP, Port: "22",
				SourceIPs: []string{"0.0.0.0/0"}}}, ApplyTo: fwTarget}))
	},
	"getFirewall": func(ctx context.Context, c *nebla.Client) error { return one(c.Firewalls.Get(ctx, 1)) },
	"putFirewall": func(ctx context.Context, c *nebla.Client) error {
		return one(c.Firewalls.Update(ctx, 1, nebla.FirewallUpdateOpts{Name: nebla.Ptr("fw2")}))
	},
	"deleteFirewall": func(ctx context.Context, c *nebla.Client) error { return one(c.Firewalls.Delete(ctx, 1)) },
	"getFirewallActions": func(ctx context.Context, c *nebla.Client) error {
		return two(c.Firewalls.Actions(ctx, 1, nebla.ActionListOpts{}))
	},
	"setFirewallRules": func(ctx context.Context, c *nebla.Client) error {
		return one(c.Firewalls.SetRules(ctx, 1, []nebla.FirewallRule{{Direction: nebla.FirewallDirectionOut,
			Protocol: nebla.FirewallProtocolICMP, DestinationIPs: []string{"0.0.0.0/0"}}}))
	},
	"applyFirewallToResources": func(ctx context.Context, c *nebla.Client) error {
		return one(c.Firewalls.ApplyToResources(ctx, 1, fwTarget))
	},
	"removeFirewallFromResources": func(ctx context.Context, c *nebla.Client) error {
		return one(c.Firewalls.RemoveFromResources(ctx, 1, fwTarget))
	},

	"getActions": func(ctx context.Context, c *nebla.Client) error {
		return two(c.Actions.List(ctx, nebla.ActionListOpts{}))
	},
	"getAction": func(ctx context.Context, c *nebla.Client) error { return one(c.Actions.Get(ctx, 1)) },

	"getServers": func(ctx context.Context, c *nebla.Client) error {
		return two(c.Servers.List(ctx, nebla.ServerListOpts{}))
	},
	"postServers": func(ctx context.Context, c *nebla.Client) error {
		return one(c.Servers.Create(ctx, nebla.ServerCreateOpts{Name: "s", ServerType: "small", Image: "ubuntu", Location: "eu-1",
			SSHKeys: []string{"k"}, PublicIPv4: nebla.NewPublicIP(), PublicIPv6: nebla.NoPublicIP(), Labels: map[string]string{"a": "b"},
			StartAfterCreate: nebla.Ptr(true), Backups: &nebla.BackupSettings{Enabled: true, Keep: 3}}))
	},
	"getServer": func(ctx context.Context, c *nebla.Client) error { return one(c.Servers.Get(ctx, 1)) },
	"putServer": func(ctx context.Context, c *nebla.Client) error {
		return one(c.Servers.Update(ctx, 1, nebla.ServerUpdateOpts{Name: nebla.Ptr("s2"), Labels: nebla.Ptr(map[string]string{"a": "b"})}))
	},
	"deleteServer": func(ctx context.Context, c *nebla.Client) error { return one(c.Servers.Delete(ctx, 1)) },
	"getServerActions": func(ctx context.Context, c *nebla.Client) error {
		return two(c.Servers.Actions(ctx, 1, nebla.ActionListOpts{}))
	},
	"poweronServer":  func(ctx context.Context, c *nebla.Client) error { return one(c.Servers.PowerOn(ctx, 1)) },
	"poweroffServer": func(ctx context.Context, c *nebla.Client) error { return one(c.Servers.PowerOff(ctx, 1)) },
	"shutdownServer": func(ctx context.Context, c *nebla.Client) error { return one(c.Servers.Shutdown(ctx, 1)) },
	"rebootServer":   func(ctx context.Context, c *nebla.Client) error { return one(c.Servers.Reboot(ctx, 1)) },
	"resetServer":    func(ctx context.Context, c *nebla.Client) error { return one(c.Servers.Reset(ctx, 1)) },
	"rebuildServer": func(ctx context.Context, c *nebla.Client) error {
		return one(c.Servers.Rebuild(ctx, 1, nebla.ServerRebuildOpts{Image: "ubuntu"}))
	},
	"restoreServerBackup": func(ctx context.Context, c *nebla.Client) error { return one(c.Servers.RestoreBackup(ctx, 1, 2)) },
	"getServerBackups": func(ctx context.Context, c *nebla.Client) error {
		return one(c.Backups.List(ctx, 1, nebla.BackupListOpts{}))
	},
	"postServerBackups":  func(ctx context.Context, c *nebla.Client) error { return one(c.Backups.Create(ctx, 1)) },
	"getServerBackup":    func(ctx context.Context, c *nebla.Client) error { return one(c.Backups.Get(ctx, 1, 2)) },
	"deleteServerBackup": func(ctx context.Context, c *nebla.Client) error { return one(c.Backups.Delete(ctx, 1, 2)) },
	"changeServerType": func(ctx context.Context, c *nebla.Client) error {
		return one(c.Servers.ChangeType(ctx, 1, nebla.ServerChangeTypeOpts{ServerType: "large", UpgradeDisk: true}))
	},
	"requestServerConsole": func(ctx context.Context, c *nebla.Client) error { return one(c.Servers.RequestConsole(ctx, 1)) },
	"getServerMetrics": func(ctx context.Context, c *nebla.Client) error {
		return one(c.Servers.Metrics(ctx, 1, nebla.ServerMetricsOpts{Types: []string{"cpu"}, Timeframe: "hour"}))
	},
	"attachServerToNetwork": func(ctx context.Context, c *nebla.Client) error {
		return two(c.Servers.AttachToNetwork(ctx, 1, nebla.ServerAttachNetworkOpts{Network: "n"}))
	},
	"detachServerFromNetwork": func(ctx context.Context, c *nebla.Client) error {
		return two(c.Servers.DetachFromNetwork(ctx, 1, "n"))
	},

	"getVolumes": func(ctx context.Context, c *nebla.Client) error {
		return two(c.Volumes.List(ctx, nebla.VolumeListOpts{}))
	},
	"postVolumes": func(ctx context.Context, c *nebla.Client) error {
		return one(c.Volumes.Create(ctx, nebla.VolumeCreateOpts{Name: "v", Size: 10, Location: "eu-1", Server: "s",
			Labels: map[string]string{"a": "b"}}))
	},
	"getVolume": func(ctx context.Context, c *nebla.Client) error { return one(c.Volumes.Get(ctx, 1)) },
	"putVolume": func(ctx context.Context, c *nebla.Client) error {
		return one(c.Volumes.Update(ctx, 1, nebla.VolumeUpdateOpts{Name: nebla.Ptr("v2")}))
	},
	"deleteVolume": func(ctx context.Context, c *nebla.Client) error { return one(c.Volumes.Delete(ctx, 1)) },
	"getVolumeActions": func(ctx context.Context, c *nebla.Client) error {
		return two(c.Volumes.Actions(ctx, 1, nebla.ActionListOpts{}))
	},
	"attachVolume": func(ctx context.Context, c *nebla.Client) error {
		return one(c.Volumes.Attach(ctx, 1, nebla.VolumeAttachOpts{Server: "s"}))
	},
	"detachVolume": func(ctx context.Context, c *nebla.Client) error { return one(c.Volumes.Detach(ctx, 1)) },
	"resizeVolume": func(ctx context.Context, c *nebla.Client) error { return one(c.Volumes.Resize(ctx, 1, 20)) },

	"getSnapshots": func(ctx context.Context, c *nebla.Client) error {
		return two(c.Snapshots.List(ctx, nebla.SnapshotListOpts{}))
	},
	"postSnapshots": func(ctx context.Context, c *nebla.Client) error {
		return one(c.Snapshots.Create(ctx, nebla.SnapshotCreateOpts{Server: "s", Name: "snap", Description: "d"}))
	},
	"getSnapshot": func(ctx context.Context, c *nebla.Client) error { return one(c.Snapshots.Get(ctx, 1)) },
	"putSnapshot": func(ctx context.Context, c *nebla.Client) error {
		return one(c.Snapshots.Update(ctx, 1, nebla.SnapshotUpdateOpts{Name: nebla.Ptr("snap2")}))
	},
	"deleteSnapshot": func(ctx context.Context, c *nebla.Client) error { return one(c.Snapshots.Delete(ctx, 1)) },
	"getSnapshotActions": func(ctx context.Context, c *nebla.Client) error {
		return two(c.Snapshots.Actions(ctx, 1, nebla.ActionListOpts{}))
	},

	"getFloatingIps": func(ctx context.Context, c *nebla.Client) error {
		return two(c.FloatingIPs.List(ctx, nebla.FloatingIPListOpts{}))
	},
	"postFloatingIps": func(ctx context.Context, c *nebla.Client) error {
		return one(c.FloatingIPs.Create(ctx, nebla.FloatingIPCreateOpts{Name: "f", Type: nebla.IPTypeIPv4, Location: "eu-1", Server: "s"}))
	},
	"getFloatingIp": func(ctx context.Context, c *nebla.Client) error { return one(c.FloatingIPs.Get(ctx, 1)) },
	"putFloatingIp": func(ctx context.Context, c *nebla.Client) error {
		return one(c.FloatingIPs.Update(ctx, 1, nebla.FloatingIPUpdateOpts{Name: nebla.Ptr("f2")}))
	},
	"deleteFloatingIp": func(ctx context.Context, c *nebla.Client) error { return one(c.FloatingIPs.Delete(ctx, 1)) },
	"getFloatingIpActions": func(ctx context.Context, c *nebla.Client) error {
		return two(c.FloatingIPs.Actions(ctx, 1, nebla.ActionListOpts{}))
	},
	"assignFloatingIp":   func(ctx context.Context, c *nebla.Client) error { return one(c.FloatingIPs.Assign(ctx, 1, "s")) },
	"unassignFloatingIp": func(ctx context.Context, c *nebla.Client) error { return one(c.FloatingIPs.Unassign(ctx, 1)) },

	"getNetworks": func(ctx context.Context, c *nebla.Client) error {
		return two(c.Networks.List(ctx, nebla.NetworkListOpts{}))
	},
	"postNetworks": func(ctx context.Context, c *nebla.Client) error {
		return one(c.Networks.Create(ctx, nebla.NetworkCreateOpts{Name: "n", Location: "eu-1", IPRange: "10.1.0.0/16"}))
	},
	"getNetwork": func(ctx context.Context, c *nebla.Client) error { return one(c.Networks.Get(ctx, 1)) },
	"putNetwork": func(ctx context.Context, c *nebla.Client) error {
		return one(c.Networks.Update(ctx, 1, nebla.NetworkUpdateOpts{Name: nebla.Ptr("n2")}))
	},
	"deleteNetwork": func(ctx context.Context, c *nebla.Client) error { return c.Networks.Delete(ctx, 1) },
	"getNetworkActions": func(ctx context.Context, c *nebla.Client) error {
		return two(c.Networks.Actions(ctx, 1, nebla.ActionListOpts{}))
	},

	"getPrimaryIps": func(ctx context.Context, c *nebla.Client) error {
		return two(c.PrimaryIPs.List(ctx, nebla.PrimaryIPListOpts{}))
	},
	"postPrimaryIps": func(ctx context.Context, c *nebla.Client) error {
		return one(c.PrimaryIPs.Create(ctx, nebla.PrimaryIPCreateOpts{Name: "p", Type: nebla.IPTypeIPv4, Location: "eu-1"}))
	},
	"getPrimaryIp": func(ctx context.Context, c *nebla.Client) error { return one(c.PrimaryIPs.Get(ctx, 1)) },
	"putPrimaryIp": func(ctx context.Context, c *nebla.Client) error {
		return one(c.PrimaryIPs.Update(ctx, 1, nebla.PrimaryIPUpdateOpts{Name: nebla.Ptr("p2")}))
	},
	"deletePrimaryIp": func(ctx context.Context, c *nebla.Client) error { return c.PrimaryIPs.Delete(ctx, 1) },
	"getPrimaryIpActions": func(ctx context.Context, c *nebla.Client) error {
		return two(c.PrimaryIPs.Actions(ctx, 1, nebla.ActionListOpts{}))
	},
	"assignPrimaryIp":   func(ctx context.Context, c *nebla.Client) error { return two(c.PrimaryIPs.Assign(ctx, 1, "s")) },
	"unassignPrimaryIp": func(ctx context.Context, c *nebla.Client) error { return two(c.PrimaryIPs.Unassign(ctx, 1)) },
}

// nonOperationFixtures are testdata files used by client unit tests, not by an operation.
var nonOperationFixtures = []string{"client_ok", "error_not_found"}

func loadSpec(t *testing.T) *openapi3.T {
	t.Helper()
	doc, err := openapi3.NewLoader().LoadFromFile(specPath)
	require.NoError(t, err)
	return doc
}

func projectOperations(doc *openapi3.T) map[string]bool {
	ids := map[string]bool{}
	for _, item := range doc.Paths.Map() {
		for _, op := range item.Operations() {
			if scope, _ := op.Extensions["x-nebla-scope"].(string); scope == "project" {
				ids[op.OperationID] = true
			}
		}
	}
	return ids
}

func TestSpecCoverage(t *testing.T) {
	want := projectOperations(loadSpec(t))
	var missing, extra []string
	for id := range want {
		if calls[id] == nil {
			missing = append(missing, id)
		}
	}
	for id := range calls {
		if !want[id] {
			extra = append(extra, id)
		}
	}
	slices.Sort(missing)
	slices.Sort(extra)
	assert.Empty(t, missing, "project operations without an SDK call")
	assert.Empty(t, extra, "SDK calls for operations that are not project-scoped")
}

// firstSuccess returns the lowest 2xx status the operation declares.
func firstSuccess(op *openapi3.Operation) int {
	best := 0
	for code := range op.Responses.Map() {
		n := 0
		if len(code) == 3 && code[0] == '2' {
			n = int(code[0]-'0')*100 + int(code[1]-'0')*10 + int(code[2]-'0')
		}
		if n != 0 && (best == 0 || n < best) {
			best = n
		}
	}
	return best
}

type specServer struct {
	router  routers.Router
	current string
	failure error
}

func (s *specServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	route, params, err := s.router.FindRoute(r)
	if err != nil {
		s.fail(w, "find route: %v", err)
		return
	}
	if route.Operation.OperationID != s.current {
		s.fail(w, "request hit operation %s", route.Operation.OperationID)
		return
	}
	in := &openapi3filter.RequestValidationInput{Request: r, PathParams: params, Route: route,
		Options: &openapi3filter.Options{AuthenticationFunc: func(_ context.Context, ai *openapi3filter.AuthenticationInput) error {
			if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
				return ai.NewError(nil)
			}
			return nil
		}}}
	if err := openapi3filter.ValidateRequest(ctx, in); err != nil {
		s.fail(w, "invalid request: %v", err)
		return
	}
	status := firstSuccess(route.Operation)
	var body []byte
	if status != http.StatusNoContent {
		if body, err = os.ReadFile(filepath.Join(fixturesDir, s.current+".json")); err != nil {
			s.fail(w, "fixture: %v", err)
			return
		}
	}
	rv := &openapi3filter.ResponseValidationInput{RequestValidationInput: in, Status: status,
		Header: http.Header{"Content-Type": []string{"application/json"}}, Options: &openapi3filter.Options{IncludeResponseStatus: true}}
	rv.SetBodyBytes(body)
	if err := openapi3filter.ValidateResponse(ctx, rv); err != nil {
		s.fail(w, "invalid response fixture: %v", err)
		return
	}
	if body != nil {
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func (s *specServer) fail(w http.ResponseWriter, format string, args ...any) {
	s.failure = fmt.Errorf(format, args...)
	http.Error(w, s.failure.Error(), http.StatusTeapot)
}

func TestSpecConformance(t *testing.T) {
	doc := loadSpec(t)
	router, err := gorillamux.NewRouter(doc)
	require.NoError(t, err)
	srv := &specServer{router: router}
	hs := httptest.NewServer(srv)
	defer hs.Close()
	c := nebla.NewClient(nebla.WithEndpoint(hs.URL), nebla.WithToken("tok"))

	for id, fn := range calls {
		t.Run(id, func(t *testing.T) {
			srv.current, srv.failure = id, nil
			err := fn(t.Context(), c)
			require.NoError(t, srv.failure, "operation %s", id)
			require.NoError(t, err, "operation %s", id)
		})
	}

	entries, err := os.ReadDir(fixturesDir)
	require.NoError(t, err)
	for _, e := range entries {
		id := strings.TrimSuffix(e.Name(), ".json")
		if slices.Contains(nonOperationFixtures, id) {
			continue
		}
		assert.Contains(t, calls, id, "fixture %s has no call", e.Name())
	}
}

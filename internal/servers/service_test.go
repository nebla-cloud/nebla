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

package servers_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/apierr"
	"github.com/nebla-cloud/nebla/internal/catalog"
	"github.com/nebla-cloud/nebla/internal/db"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/ipaddress"
	"github.com/nebla-cloud/nebla/internal/ent/ipallocation"
	"github.com/nebla-cloud/nebla/internal/ent/node"
	"github.com/nebla-cloud/nebla/internal/ent/server"
	"github.com/nebla-cloud/nebla/internal/httpapi"
	"github.com/nebla-cloud/nebla/internal/projects"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/stretchr/testify/require"
)

type serverJSON struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Status   string `json:"status"`
	Location struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	} `json:"location"`
	ServerType struct {
		ID    int64  `json:"id"`
		Name  string `json:"name"`
		Cores int    `json:"cores"`
	} `json:"server_type"`
	Image struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	} `json:"image"`
	PublicNet struct {
		IPv4 *struct {
			IP string `json:"ip"`
		} `json:"ipv4"`
		IPv6 *struct {
			IP string `json:"ip"`
		} `json:"ipv6"`
	} `json:"public_net"`
	Labels  map[string]string `json:"labels"`
	Created string            `json:"created"`
	DiskGB  int               `json:"disk_gb"`
}

type actionJSON struct {
	ID        int64  `json:"id"`
	Command   string `json:"command"`
	Status    string `json:"status"`
	Resources []struct {
		ID   int64  `json:"id"`
		Type string `json:"type"`
	} `json:"resources"`
}

type createJSON struct {
	Server      serverJSON   `json:"server"`
	Action      actionJSON   `json:"action"`
	NextActions []actionJSON `json:"next_actions"`
}

type listJSON struct {
	Servers []serverJSON `json:"servers"`
	Meta    struct {
		Pagination struct {
			TotalEntries int `json:"total_entries"`
		} `json:"pagination"`
	} `json:"meta"`
}

func newEnv(t *testing.T) *testutil.ServerEnv {
	t.Helper()
	return testutil.NewServerEnv(testutil.NewApp(t, testutil.QueueOnlyJobs(t)))
}

func body(t *testing.T, res *http.Response) string {
	t.Helper()
	defer res.Body.Close()
	return mustRead(t, res)
}

func mustRead(t *testing.T, res *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	return string(b)
}

func expect[T any](t *testing.T, res *http.Response, status int) (T, string) {
	t.Helper()
	raw := body(t, res)
	require.Equal(t, status, res.StatusCode, raw)
	var v T
	require.NoError(t, json.Unmarshal([]byte(raw), &v), raw)
	return v, raw
}

func expectErr(t *testing.T, res *http.Response, status int, code string) {
	t.Helper()
	v, raw := expect[struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}](t, res, status)
	require.Equal(t, code, v.Error.Code, raw)
}

func post(e *testutil.ServerEnv, b map[string]any) *http.Response {
	full := map[string]any{"server_type": "small", "image": "ubuntu", "location": "eu-1"}
	for k, v := range b {
		full[k] = v
	}
	return e.C.Send(http.MethodPost, "/v1/servers", full)
}

func sid(id int64) string { return strconv.FormatInt(id, 10) }

func TestCreateServerValidatesName(t *testing.T) {
	e := newEnv(t)
	for _, name := range []string{"", "Web", "-web", "web-", "a_b", "a.b", strings.Repeat("a", 64)} {
		expectErr(t, post(e, map[string]any{"name": name}), http.StatusUnprocessableEntity, "invalid_input")
	}
	e.CreateServer(strings.Repeat("a", 63), nil)
	e.CreateServer("web-1", nil)
	expectErr(t, post(e, map[string]any{"name": "web-1"}), http.StatusConflict, "uniqueness_error")

	// Names are unique per project only.
	other := e.Admin.CreateProject("other")
	res := e.Admin.WithProject(other).Send(http.MethodPost, "/v1/servers",
		map[string]any{"name": "web-1", "server_type": "small", "image": "ubuntu", "location": "eu-1"})
	expect[createJSON](t, res, http.StatusCreated)
}

func TestCreateServerReturnsActionAndAllocatesIP(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	key := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl test@example"
	res := e.C.Send(http.MethodPost, "/v1/ssh_keys", map[string]string{"name": "laptop", "public_key": key})
	k, _ := expect[struct {
		ID int64 `json:"id"`
	}](t, res, http.StatusCreated)

	res = post(e, map[string]any{"name": "web-1", "ssh_keys": []string{"laptop", sid(k.ID)}, "user_data": "#cloud-config\nhostname: x\n",
		"labels": map[string]string{"env": "prod"}, "public_net": map[string]bool{"ipv4": true, "ipv6": true}, "start_after_create": false})
	out, raw := expect[createJSON](t, res, http.StatusCreated)
	require.NotContains(t, raw, "user_data")
	require.NotContains(t, raw, "vmid")
	require.NotContains(t, raw, "cloud-config")

	s := out.Server
	require.Equal(t, "web-1", s.Name)
	require.Equal(t, "initializing", s.Status)
	require.Equal(t, "eu-1", s.Location.Name)
	require.Equal(t, e.Type, s.ServerType.ID)
	require.Equal(t, 2, s.ServerType.Cores)
	require.Equal(t, "ubuntu", s.Image.Name)
	require.Equal(t, 20, s.DiskGB)
	require.Equal(t, map[string]string{"env": "prod"}, s.Labels)
	require.NotNil(t, s.PublicNet.IPv4)
	require.Equal(t, "10.0.0.10", s.PublicNet.IPv4.IP)
	require.NotNil(t, s.PublicNet.IPv6)
	require.Equal(t, "2001:db8::10", s.PublicNet.IPv6.IP)
	require.NotEmpty(t, s.Created)

	require.Equal(t, "create_server", out.Action.Command)
	require.Equal(t, "running", out.Action.Status)
	require.Len(t, out.Action.Resources, 1)
	require.Equal(t, s.ID, out.Action.Resources[0].ID)
	require.Equal(t, "server", out.Action.Resources[0].Type)
	require.NotNil(t, out.NextActions)
	require.Empty(t, out.NextActions)

	row, err := e.App.Ent.Server.Get(ctx, s.ID)
	require.NoError(t, err)
	require.Regexp(t, regexp.MustCompile(`^BC:24:11:[0-9A-F]{2}:[0-9A-F]{2}:[0-9A-F]{2}$`), row.MAC)
	require.Equal(t, "pve1", row.Node, "equal free memory breaks ties by name")
	require.Equal(t, testutil.TemplateVMID, row.TemplateVmid)
	require.Nil(t, row.Vmid)
	require.Equal(t, "#cloud-config\nhostname: x\n", row.UserData)
	require.Equal(t, []string{key}, row.SSHKeys)
	require.Equal(t, e.Project, row.ProjectID)
	require.Equal(t, 2, row.Cores)
	require.Equal(t, 2048, row.MemoryMB)
	require.Equal(t, 20, row.DiskGB)

	a, err := actions.Get(ctx, e.App.Ent, e.Project, out.Action.ID)
	require.NoError(t, err)
	require.NotNil(t, a.RiverJobID)
	job, err := e.App.Jobs.Client.JobGet(ctx, *a.RiverJobID)
	require.NoError(t, err)
	require.Equal(t, servers.KindCreateServer, job.Kind)
	var args servers.CreateServerArgs
	require.NoError(t, json.Unmarshal(job.EncodedArgs, &args))
	require.Equal(t, servers.CreateServerArgs{ServerID: s.ID, ActionID: a.ID, StartAfter: false}, args)

	primaries, err := e.App.Ent.IPAddress.Query().Where(ipaddress.KindEQ(ipaddress.KindPrimary), ipaddress.ServerID(s.ID),
		ipaddress.AutoDelete(true)).All(ctx)
	require.NoError(t, err)
	require.Len(t, primaries, 2)
	for _, p := range primaries {
		require.Equal(t, "web-1-"+p.Family, p.Name)
		require.Equal(t, ipallocation.OwnerPrimary, e.App.Ent.IPAllocation.GetX(ctx, p.AllocationID).Owner)
	}

	got, raw := expect[serverJSON](t, e.C.Get("/v1/servers/"+sid(s.ID)), http.StatusOK)
	require.Equal(t, s, got)
	require.NotContains(t, raw, "cloud-config")

	// Reserved memory spreads later servers: pve1 has 2 GiB reserved now.
	second := e.CreateServer("web-2", nil)
	row2, err := e.App.Ent.Server.Get(ctx, second.ServerID)
	require.NoError(t, err)
	require.Equal(t, "pve2", row2.Node)
	require.NotEqual(t, row.MAC, row2.MAC)
	g2, _ := expect[serverJSON](t, e.C.Get("/v1/servers/"+sid(second.ServerID)), http.StatusOK)
	require.Equal(t, "10.0.0.11", g2.PublicNet.IPv4.IP)
	require.Nil(t, g2.PublicNet.IPv6, "ipv6 defaults to off")
	var startArgs servers.CreateServerArgs
	a2, err := actions.Get(ctx, e.App.Ent, e.Project, second.ActionID)
	require.NoError(t, err)
	job2, err := e.App.Jobs.Client.JobGet(ctx, *a2.RiverJobID)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(job2.EncodedArgs, &startArgs))
	require.True(t, startArgs.StartAfter, "start_after_create defaults to true")

	// A disabled node is never chosen.
	_, err = e.App.Ent.Node.Update().Where(node.Name("pve2")).SetEnabled(false).Save(ctx)
	require.NoError(t, err)
	third := e.CreateServer("web-3", nil)
	row3, err := e.App.Ent.Server.Get(ctx, third.ServerID)
	require.NoError(t, err)
	require.Equal(t, "pve1", row3.Node)
}

func TestCreateServerNoCapacityRollsBack(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.Admin.CreateServerType("huge", 4, 100_000, 20)
	expectErr(t, post(e, map[string]any{"name": "big", "server_type": "huge"}), http.StatusUnprocessableEntity, "resource_unavailable")
	n, err := e.App.Ent.Server.Query().Count(ctx)
	require.NoError(t, err)
	require.Zero(t, n)
	n, err = e.App.Ent.IPAllocation.Query().Count(ctx)
	require.NoError(t, err)
	require.Zero(t, n)
	n, err = e.App.Ent.Action.Query().Count(ctx)
	require.NoError(t, err)
	require.Zero(t, n)
}

func TestCreateServerRejectsBadInput(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	other := e.Admin.WithProject(e.Admin.CreateProject("other"))
	key := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl test@example"
	res := other.Send(http.MethodPost, "/v1/ssh_keys", map[string]string{"name": "theirs", "public_key": key})
	theirs, _ := expect[struct {
		ID int64 `json:"id"`
	}](t, res, http.StatusCreated)

	loc2 := e.Admin.CreateLocation("eu-2")
	res = e.Admin.Send(http.MethodPost, "/v1/admin/server_types",
		map[string]any{"name": "eu2-only", "cores": 1, "memory_mb": 512, "disk_gb": 10, "locations": []int64{loc2}})
	require.Equal(t, http.StatusCreated, res.StatusCode, body(t, res))
	res = e.Admin.Send(http.MethodPost, "/v1/admin/server_types",
		map[string]any{"name": "old", "cores": 1, "memory_mb": 512, "disk_gb": 10, "deprecated": true})
	require.Equal(t, http.StatusCreated, res.StatusCode, body(t, res))

	cases := []struct {
		name   string
		body   map[string]any
		status int
		code   string
	}{
		{"unknown type", map[string]any{"server_type": "nope"}, 404, "not_found"},
		{"unknown image", map[string]any{"image": "nope"}, 404, "not_found"},
		{"unknown location", map[string]any{"location": "nope"}, 404, "not_found"},
		{"type by id works", map[string]any{"server_type": sid(e.Type), "image": sid(e.Image), "location": sid(e.Location)}, 201, ""},
		{"deprecated type", map[string]any{"server_type": "old"}, 422, "invalid_input"},
		{"type not in location", map[string]any{"server_type": "eu2-only"}, 422, "invalid_input"},
		{"image not in location", map[string]any{"location": "eu-2", "server_type": "eu2-only"}, 422, "invalid_input"},
		{"other project's key by id", map[string]any{"ssh_keys": []string{sid(theirs.ID)}}, 404, "not_found"},
		{"other project's key by name", map[string]any{"ssh_keys": []string{"theirs"}}, 404, "not_found"},
		{"user_data too big", map[string]any{"user_data": strings.Repeat("x", 32<<10+1)}, 422, "invalid_input"},
		{"user_data bad yaml", map[string]any{"user_data": "#cloud-config\npackages: [htop\n"}, 422, "invalid_input"},
		{"user_data not a mapping", map[string]any{"user_data": "#cloud-config\n- a\n- b\n"}, 422, "invalid_input"},
		{"user_data keys not a list", map[string]any{"user_data": "#cloud-config\nssh_authorized_keys: x\n"}, 422, "invalid_input"},
		{"bad label key", map[string]any{"labels": map[string]string{"bad key": "v"}}, 422, "invalid_input"},
		{"bad label value", map[string]any{"labels": map[string]string{"k": "-v"}}, 422, "invalid_input"},
		{"too many labels", map[string]any{"labels": manyLabels(65)}, 422, "invalid_input"},
	}
	for i, tc := range cases {
		b := map[string]any{"name": "s-" + strconv.Itoa(i)}
		for k, v := range tc.body {
			b[k] = v
		}
		res := post(e, b)
		if tc.status == 201 {
			expect[createJSON](t, res, 201)
			continue
		}
		raw := body(t, res)
		require.Equal(t, tc.status, res.StatusCode, "%s: %s", tc.name, raw)
		require.Contains(t, raw, `"`+tc.code+`"`, tc.name)
	}
	e.CreateServer("max-labels", map[string]any{"labels": manyLabels(64)})
	e.CreateServer("user-data-max", map[string]any{"user_data": strings.Repeat("x", 32<<10)})
	e.CreateServer("user-data-script", map[string]any{"user_data": "#!/bin/sh\necho hi\n"})

	// A read-only token cannot create.
	ro := e.App.TokenClient(e.Admin.CreateToken(e.Project, "read"))
	res = ro.Send(http.MethodPost, "/v1/servers", map[string]any{"name": "ro", "server_type": "small", "image": "ubuntu", "location": "eu-1"})
	expectErr(t, res, http.StatusForbidden, "forbidden")
	n, err := e.App.Ent.Server.Query().Where(server.Name("ro")).Count(ctx)
	require.NoError(t, err)
	require.Zero(t, n)
}

func manyLabels(n int) map[string]string {
	m := map[string]string{}
	for i := range n {
		m["k"+strconv.Itoa(i)] = "v"
	}
	return m
}

func TestQuotaConcurrentCreates(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.Admin.SetQuota(e.Project, 1, 0, 0, 0, 0)

	start := make(chan struct{})
	var wg sync.WaitGroup
	statuses := make([]int, 2)
	bodies := make([]string, 2)
	for i := range 2 {
		wg.Go(func() {
			<-start
			res := post(e, map[string]any{"name": "s" + strconv.Itoa(i)})
			defer res.Body.Close()
			statuses[i] = res.StatusCode
			b, _ := io.ReadAll(res.Body)
			bodies[i] = string(b)
		})
	}
	close(start)
	wg.Wait()
	slices.Sort(statuses)
	require.Equal(t, []int{http.StatusCreated, http.StatusUnprocessableEntity}, statuses, bodies)
	require.True(t, slices.ContainsFunc(bodies, func(b string) bool { return strings.Contains(b, `"quota_exceeded"`) }), bodies)

	n, err := e.App.Ent.Server.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	n, err = e.App.Ent.IPAllocation.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	n, err = e.App.Ent.Action.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n)
}

func TestQuotaCountsResources(t *testing.T) {
	e := newEnv(t)
	e.Admin.SetQuota(e.Project, 0, 3, 0, 0, 0)
	e.CreateServer("a", nil)
	res := post(e, map[string]any{"name": "b"})
	raw := body(t, res)
	require.Equal(t, http.StatusUnprocessableEntity, res.StatusCode, raw)
	require.Contains(t, raw, "max_vcpu")

	e.Admin.SetQuota(e.Project, 0, 0, 0, 0, 2)
	res = post(e, map[string]any{"name": "c", "public_net": map[string]bool{"ipv4": true, "ipv6": true}})
	raw = body(t, res)
	require.Equal(t, http.StatusUnprocessableEntity, res.StatusCode, raw)
	require.Contains(t, raw, "max_ips")
	e.CreateServer("d", nil)

	e.Admin.SetQuota(e.Project, 0, 0, 4096, 0, 0)
	expectErr(t, post(e, map[string]any{"name": "e"}), http.StatusUnprocessableEntity, "quota_exceeded")
	e.Admin.SetQuota(e.Project, 0, 0, 0, 40, 0)
	expectErr(t, post(e, map[string]any{"name": "f"}), http.StatusUnprocessableEntity, "quota_exceeded")
}

func TestLabelSelector(t *testing.T) {
	e := newEnv(t)
	noIP := map[string]bool{"ipv4": false}
	e.CreateServer("a", map[string]any{"labels": map[string]string{"env": "prod", "tier": "web"}, "public_net": noIP})
	e.CreateServer("b", map[string]any{"labels": map[string]string{"env": "dev", "tier": "web", "example.com/role": "db"}, "public_net": noIP})
	e.CreateServer("c", map[string]any{"labels": map[string]string{"env": "prod"}, "public_net": noIP})
	e.CreateServer("d", map[string]any{"public_net": noIP})
	other := e.Admin.WithProject(e.Admin.CreateProject("other"))
	res := other.Send(http.MethodPost, "/v1/servers", map[string]any{"name": "x", "server_type": "small", "image": "ubuntu",
		"location": "eu-1", "labels": map[string]string{"env": "prod"}, "public_net": noIP})
	expect[createJSON](t, res, http.StatusCreated)

	names := func(q string) []string {
		t.Helper()
		l, _ := expect[listJSON](t, e.C.Get("/v1/servers"+q), http.StatusOK)
		out := []string{}
		for _, s := range l.Servers {
			out = append(out, s.Name)
		}
		return out
	}
	sel := func(s string) string { return "?label_selector=" + url.QueryEscape(s) }
	require.Equal(t, []string{"a", "b", "c", "d"}, names(""))
	require.Equal(t, []string{"a", "c"}, names(sel("env=prod")))
	require.Equal(t, []string{"b", "d"}, names(sel("env!=prod")))
	require.Equal(t, []string{"a", "b"}, names(sel("tier")))
	require.Equal(t, []string{"c", "d"}, names(sel("!tier")))
	require.Equal(t, []string{"a"}, names(sel("env=prod,tier")))
	require.Equal(t, []string{"c"}, names(sel("env=prod, !tier")))
	require.Equal(t, []string{"b"}, names(sel("example.com/role=db")))
	require.Equal(t, []string{}, names(sel("env=staging")))
	require.Equal(t, []string{"b"}, names("?name=b"))
	require.Equal(t, []string{"a", "b", "c", "d"}, names("?status=initializing"))
	require.Equal(t, []string{"a", "b", "c", "d"}, names("?status=running,initializing"))
	require.Equal(t, []string{"a", "b", "c", "d"}, names("?status=running&status=initializing"))
	require.Equal(t, []string{}, names("?status=running"))

	l, _ := expect[listJSON](t, e.C.Get("/v1/servers?per_page=3&page=2"), http.StatusOK)
	require.Equal(t, 4, l.Meta.Pagination.TotalEntries)
	require.Len(t, l.Servers, 1)
	require.Equal(t, "d", l.Servers[0].Name)

	for _, bad := range []string{sel("bad key=x"), sel("k=-v"), sel(",,"), sel("a=b=c"), sel("!"), "?status=bogus"} {
		expectErr(t, e.C.Get("/v1/servers"+bad), http.StatusUnprocessableEntity, "invalid_input")
	}
}

func TestUpdateAndDeleteServer(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	s := e.CreateServer("web", map[string]any{"labels": map[string]string{"a": "1"}})
	e.CreateServer("taken", nil)
	path := "/v1/servers/" + sid(s.ServerID)
	// The create action is still running.
	expectErr(t, e.C.Send(http.MethodPut, path, map[string]any{"name": "web-2"}), http.StatusConflict, "locked")
	expectErr(t, e.C.Send(http.MethodDelete, path, nil), http.StatusConflict, "locked")
	require.NoError(t, actions.Succeed(ctx, e.App.Ent, s.ActionID))

	got, _ := expect[serverJSON](t, e.C.Send(http.MethodPut, path, map[string]any{"labels": map[string]string{"b": "2"}}), http.StatusOK)
	require.Equal(t, "web", got.Name)
	require.Equal(t, map[string]string{"b": "2"}, got.Labels)
	got, _ = expect[serverJSON](t, e.C.Send(http.MethodPut, path, map[string]any{"name": "web-2"}), http.StatusOK)
	require.Equal(t, "web-2", got.Name)
	require.Equal(t, map[string]string{"b": "2"}, got.Labels, "omitted labels are kept")
	expectErr(t, e.C.Send(http.MethodPut, path, map[string]any{"name": "taken"}), http.StatusConflict, "uniqueness_error")
	expectErr(t, e.C.Send(http.MethodPut, path, map[string]any{"name": "Bad"}), http.StatusUnprocessableEntity, "invalid_input")
	expectErr(t, e.C.Send(http.MethodPut, path, map[string]any{"labels": map[string]string{"": "x"}}), http.StatusUnprocessableEntity, "invalid_input")

	del, _ := expect[struct {
		Action actionJSON `json:"action"`
	}](t, e.C.Send(http.MethodDelete, path, nil), http.StatusOK)
	require.Equal(t, "delete_server", del.Action.Command)
	require.Equal(t, "running", del.Action.Status)
	require.Equal(t, s.ServerID, del.Action.Resources[0].ID)
	got, _ = expect[serverJSON](t, e.C.Get(path), http.StatusOK)
	require.Equal(t, "deleting", got.Status)
	a, err := actions.Get(ctx, e.App.Ent, e.Project, del.Action.ID)
	require.NoError(t, err)
	job, err := e.App.Jobs.Client.JobGet(ctx, *a.RiverJobID)
	require.NoError(t, err)
	require.Equal(t, servers.KindDeleteServer, job.Kind)
	var args servers.DeleteServerArgs
	require.NoError(t, json.Unmarshal(job.EncodedArgs, &args))
	require.Equal(t, servers.DeleteServerArgs{ServerID: s.ServerID, ActionID: a.ID}, args)
	expectErr(t, e.C.Send(http.MethodDelete, path, nil), http.StatusConflict, "locked")

	l, _ := expect[struct {
		Actions []actionJSON `json:"actions"`
	}](t, e.C.Get(path+"/actions"), http.StatusOK)
	require.Len(t, l.Actions, 2)
	require.Equal(t, "delete_server", l.Actions[0].Command)
	require.Equal(t, "create_server", l.Actions[1].Command)
	l, _ = expect[struct {
		Actions []actionJSON `json:"actions"`
	}](t, e.C.Get(path+"/actions?status=success"), http.StatusOK)
	require.Len(t, l.Actions, 1)
	require.Equal(t, s.ActionID, l.Actions[0].ID)
}

func TestServerCrossProjectNotFound(t *testing.T) {
	e := newEnv(t)
	s := e.CreateServer("web", nil)
	path := "/v1/servers/" + sid(s.ServerID)
	other := e.Admin.WithProject(e.Admin.CreateProject("other"))
	expectErr(t, other.Get(path), http.StatusNotFound, "not_found")
	expectErr(t, other.Send(http.MethodPut, path, map[string]any{"name": "x"}), http.StatusNotFound, "not_found")
	expectErr(t, other.Send(http.MethodDelete, path, nil), http.StatusNotFound, "not_found")
	expectErr(t, other.Get(path+"/actions"), http.StatusNotFound, "not_found")
	expectErr(t, e.C.Get("/v1/servers/999999"), http.StatusNotFound, "not_found")
	l, _ := expect[listJSON](t, other.Get("/v1/servers"), http.StatusOK)
	require.Empty(t, l.Servers)
	// No Project-ID header.
	expectErr(t, e.Admin.Get(path), http.StatusNotFound, "not_found")
}

func TestServersBlockCatalogAndProjectDeletes(t *testing.T) {
	e := newEnv(t)
	e.CreateServer("web", nil)
	expectErr(t, e.Admin.Send(http.MethodDelete, "/v1/admin/server_types/"+sid(e.Type), nil), http.StatusConflict, "resource_in_use")
	expectErr(t, e.Admin.Send(http.MethodDelete, "/v1/admin/images/"+sid(e.Image), nil), http.StatusConflict, "resource_in_use")
	expectErr(t, e.Admin.Send(http.MethodDelete, "/v1/admin/locations/"+sid(e.Location), nil), http.StatusConflict, "location_in_use")
	expectErr(t, e.Admin.Send(http.MethodDelete, "/v1/projects/"+sid(e.Project), nil), http.StatusConflict, "project_not_empty")
}

func TestCreateWithoutJobRuntime(t *testing.T) {
	client, sqlDB := testutil.NewEntSQL(t)
	cat := catalog.New(sqlDB, client, testutil.CatalogOptions(catalog.Hooks{}))
	svc := servers.New(servers.Options{SQL: sqlDB, Ent: client, Catalog: cat, Projects: projects.New(client, projects.Options{})})
	_, _, err := svc.Create(context.Background(), 1, servers.CreateInput{Name: "x"})
	var ae apierr.E
	require.True(t, errors.As(err, &ae), "got %v", err)
	require.Equal(t, http.StatusServiceUnavailable, ae.Status)
	require.Equal(t, "service_unavailable", ae.Code)
	_, err = svc.Delete(context.Background(), 1, 1)
	require.True(t, errors.As(err, &ae), "got %v", err)
	require.Equal(t, "service_unavailable", ae.Code)
}

// Running servers are already in the node's mem, so only not-running ones (except missing/error) reserve memory.
func TestPlacementReservedMemory(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	noIP := map[string]any{"public_net": map[string]bool{"ipv4": false}}
	nodeOf := func(id int64) string {
		t.Helper()
		r, err := e.App.Ent.Server.Get(ctx, id)
		require.NoError(t, err)
		return r.Node
	}
	setStatus := func(id int64, st server.Status) {
		t.Helper()
		require.NoError(t, e.App.Ent.Server.UpdateOneID(id).SetStatus(st).Exec(ctx))
	}

	a := e.CreateServer("a", noIP)
	require.Equal(t, "pve1", nodeOf(a.ServerID))
	setStatus(a.ServerID, server.StatusRunning)
	b := e.CreateServer("b", noIP)
	require.Equal(t, "pve1", nodeOf(b.ServerID), "a running server is not counted on top of node mem")

	setStatus(b.ServerID, server.StatusOff)
	c := e.CreateServer("c", noIP)
	require.Equal(t, "pve2", nodeOf(c.ServerID), "an off server still reserves its memory")

	for _, st := range []server.Status{server.StatusStarting, server.StatusStopping, server.StatusRebuilding, server.StatusDeleting, server.StatusInitializing} {
		setStatus(b.ServerID, st)
		setStatus(c.ServerID, server.StatusRunning)
		d := e.CreateServer("d-"+string(st), noIP)
		require.Equal(t, "pve2", nodeOf(d.ServerID), "%s reserves memory on pve1", st)
		require.NoError(t, e.App.Ent.Server.DeleteOneID(d.ServerID).Exec(ctx))
	}
	for _, st := range []server.Status{server.StatusMissing, server.StatusError} {
		setStatus(b.ServerID, st)
		setStatus(c.ServerID, server.StatusOff)
		d := e.CreateServer("d-"+string(st), noIP)
		require.Equal(t, "pve1", nodeOf(d.ServerID), "%s does not reserve memory", st)
		require.NoError(t, e.App.Ent.Server.DeleteOneID(d.ServerID).Exec(ctx))
	}
}

func TestUsageUsesServerSnapshot(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.CreateServer("a", nil)
	want := projects.Usage{Servers: 1, VCPU: 2, MemoryMB: 2048, DiskGB: 20}
	got, err := servers.Usage(ctx, e.App.Ent, e.Project)
	require.NoError(t, err)
	require.Equal(t, want, got)

	res := e.Admin.Send(http.MethodPut, "/v1/admin/server_types/"+sid(e.Type),
		map[string]any{"name": "small", "cores": 8, "memory_mb": 16384, "disk_gb": 80})
	require.Equal(t, http.StatusOK, res.StatusCode, body(t, res))
	got, err = servers.Usage(ctx, e.App.Ent, e.Project)
	require.NoError(t, err)
	require.Equal(t, want, got, "editing the type leaves existing servers' usage alone")

	// The quota check sees the snapshot too: 2 + 1 cores fits a limit of 3.
	e.Admin.CreateServerType("tiny", 1, 512, 10)
	e.Admin.SetQuota(e.Project, 0, 3, 0, 0, 0)
	e.CreateServer("b", map[string]any{"server_type": "tiny"})
}

func TestLockServerBackupInProgress(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	s := e.CreateServer("a", nil)
	lock := func() error {
		return db.WithTx(ctx, e.App.SQL, func(_ *sql.Tx, etx *ent.Client) error {
			_, err := servers.LockServer(ctx, etx, e.Project, s.ServerID)
			return err
		})
	}
	var ae apierr.E
	// The create action is running.
	require.ErrorAs(t, lock(), &ae)
	require.Equal(t, "locked", ae.Code)

	require.NoError(t, actions.Succeed(ctx, e.App.Ent, s.ActionID))
	require.NoError(t, lock())

	_, err := actions.Create(ctx, e.App.Ent, e.Project, servers.KindCreateBackup, []actions.Resource{{ID: s.ServerID, Type: servers.ResourceType}})
	require.NoError(t, err)
	require.ErrorAs(t, lock(), &ae)
	require.Equal(t, http.StatusConflict, ae.Status)
	require.Equal(t, "backup_in_progress", ae.Code)
	require.Equal(t, "A backup of this server is running", ae.Msg)
}

func TestUsageCountsPrimaryOnce(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.CreateServer("web", map[string]any{"public_net": map[string]bool{"ipv4": true, "ipv6": true}})
	res := e.C.Send(http.MethodPost, "/v1/floating_ips", map[string]any{"type": "ipv4", "location": "eu-1"})
	require.Equal(t, http.StatusCreated, res.StatusCode, body(t, res))

	own, err := servers.Usage(ctx, e.App.Ent, e.Project)
	require.NoError(t, err)
	require.Zero(t, own.IPs, "servers no longer count addresses")
	hooks, _ := httpapi.ServerHooks(projects.Hooks{}, catalog.Hooks{})
	u, err := hooks.Usage(ctx, e.App.Ent, e.Project)
	require.NoError(t, err)
	require.Equal(t, 3, u.IPs)
}

func TestServerDeleteAutoDelete(t *testing.T) {
	ctx := context.Background()
	e := workerEnv(t, nil)
	s := e.CreateServer("web", map[string]any{"public_net": map[string]bool{"ipv4": true, "ipv6": true}})
	waitOK(t, e, s.ActionID)
	of := func(family string) *ent.IPAddress {
		t.Helper()
		return e.App.Ent.IPAddress.Query().Where(ipaddress.KindEQ(ipaddress.KindPrimary), ipaddress.ServerID(s.ServerID),
			ipaddress.Family(family)).OnlyX(ctx)
	}
	v4, v6 := of("ipv4"), of("ipv6")
	require.True(t, v4.AutoDelete)
	require.NoError(t, e.App.Ent.IPAddress.UpdateOneID(v6.ID).SetAutoDelete(false).Exec(ctx))

	del, _ := expect[actionBody](t, e.C.Send(http.MethodDelete, spath(s.ServerID), nil), http.StatusOK)
	waitOK(t, e, del.Action.ID)
	_, err := e.App.Ent.IPAddress.Get(ctx, v4.ID)
	require.True(t, ent.IsNotFound(err), "auto_delete row is deleted")
	_, err = e.App.Ent.IPAllocation.Get(ctx, v4.AllocationID)
	require.True(t, ent.IsNotFound(err), "and its address freed")
	kept, err := e.App.Ent.IPAddress.Get(ctx, v6.ID)
	require.NoError(t, err)
	require.Nil(t, kept.ServerID)
	require.False(t, kept.AutoDelete)
	_, err = e.App.Ent.IPAllocation.Get(ctx, v6.AllocationID)
	require.NoError(t, err, "the kept address stays allocated")
}

func TestServerOSFactsSurviveImageAndSnapshotDelete(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	c := e.App.Ent
	debian := c.Image.Create().SetName("debian").SetOsFlavor("debian").SetOsVersion("13").SaveX(ctx)
	snap := c.Snapshot.Create().SetProjectID(e.Project).SetLocationID(e.Location).SetName("s").SetLabels(map[string]string{}).
		SetImageID(debian.ID).SetOsFlavor("debian").SetOsVersion("13").SetSizeGB(10).SaveX(ctx)

	// Only the snapshot references the image, so the delete works and nulls the snapshot's image.
	c.Image.DeleteOneID(debian.ID).ExecX(ctx)
	require.Nil(t, c.Snapshot.GetX(ctx, snap.ID).ImageID)

	made := e.CreateServer("web", nil)
	c.Server.UpdateOneID(made.ServerID).ClearImageID().SetSnapshotID(snap.ID).SetOsFlavor("debian").SetOsVersion("13").ExecX(ctx)
	c.Snapshot.DeleteOneID(snap.ID).ExecX(ctx)

	row := c.Server.GetX(ctx, made.ServerID)
	require.Nil(t, row.ImageID)
	require.Nil(t, row.SnapshotID)
	flavor, version, err := servers.OSFacts(ctx, c, row)
	require.NoError(t, err)
	require.Equal(t, "debian", flavor)
	require.Equal(t, "13", version)

	got, _ := expect[struct {
		Image struct {
			OsFlavor  string `json:"os_flavor"`
			OsVersion string `json:"os_version"`
		} `json:"image"`
	}](t, e.C.Get("/v1/servers/"+sid(made.ServerID)), http.StatusOK)
	require.Equal(t, "debian", got.Image.OsFlavor)
	require.Equal(t, "13", got.Image.OsVersion)
}

func TestImageDeleteRefusedWhileServerUsesIt(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.CreateServer("web", nil)
	err := e.App.Ent.Image.DeleteOneID(e.Image).Exec(ctx)
	require.Error(t, err)
	expectErr(t, e.Admin.Send(http.MethodDelete, "/v1/admin/images/"+sid(e.Image), nil), http.StatusConflict, "resource_in_use")
}

func TestOSFactsFallBackToImage(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	made := e.CreateServer("web", nil)
	row := e.App.Ent.Server.GetX(ctx, made.ServerID)
	flavor, version, err := servers.OSFacts(ctx, e.App.Ent, row)
	require.NoError(t, err)
	require.Equal(t, "ubuntu", flavor)
	require.Equal(t, "24.04", version)
	require.Equal(t, "ubuntu", row.OsFlavor, "create copies the image's facts onto the row")
}

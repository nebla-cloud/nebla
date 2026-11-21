package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/nebla-cloud/nebla/internal/ent/volume"
	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/stretchr/testify/require"
)

// TestCrossProjectIsolation: a project-A token never sees or acts on project B's server, firewall, actions, metrics or console.
func TestCrossProjectIsolation(t *testing.T) {
	ctx := context.Background()
	e := testutil.NewServerEnv(testutil.NewApp(t, testutil.QueueOnlyJobs(t)))
	b := e.RunningServer("web")
	res := e.C.Send(http.MethodPost, "/v1/firewalls", map[string]any{"name": "fw", "apply_to": []any{serverRes(b.ServerID)}})
	require.Equal(t, http.StatusCreated, res.StatusCode)
	fwp := "/v1/firewalls/" + strconv.FormatInt(decode[fwCreated](t, e.App, res).Firewall.ID, 10)
	projA := e.Admin.CreateProject("a")
	tokA := e.App.TokenClient(e.Admin.CreateToken(projA, "read_write"))
	before, err := e.App.Ent.Action.Query().Count(ctx)
	require.NoError(t, err)

	srv := "/v1/servers/" + strconv.FormatInt(b.ServerID, 10)
	for _, r := range []struct {
		method, path string
		body         any
	}{
		{method: http.MethodGet, path: srv},
		{method: http.MethodPost, path: srv + "/actions/poweroff"},
		{method: http.MethodGet, path: "/v1/actions/" + strconv.FormatInt(b.ActionID, 10)},
		{method: http.MethodGet, path: srv + "/actions"},
		{method: http.MethodGet, path: srv + "/metrics?type=cpu&timeframe=hour"},
		{method: http.MethodPost, path: srv + "/actions/request_console"},
		{method: http.MethodGet, path: fwp},
		{method: http.MethodPut, path: fwp, body: map[string]any{"name": "x"}},
		{method: http.MethodDelete, path: fwp},
		{method: http.MethodGet, path: fwp + "/actions"},
		{method: http.MethodPost, path: fwp + "/actions/set_rules", body: map[string]any{"rules": []any{}}},
		{method: http.MethodPost, path: fwp + "/actions/apply_to_resources", body: map[string]any{"apply_to": []any{serverRes(b.ServerID)}}},
		{method: http.MethodPost, path: fwp + "/actions/remove_from_resources", body: map[string]any{"remove_from": []any{serverRes(b.ServerID)}}},
	} {
		res := tokA.Send(r.method, r.path, r.body)
		body := e.App.Body(res)
		require.Equal(t, http.StatusNotFound, res.StatusCode, "%s %s: %s", r.method, r.path, body)
		require.JSONEq(t, `{"error":{"code":"not_found","message":"not found"}}`, body)
	}
	after, err := e.App.Ent.Action.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, before, after, "no action started in project B")
	tokens, err := e.App.Ent.ConsoleToken.Query().Count(ctx)
	require.NoError(t, err)
	require.Zero(t, tokens, "no console token minted for project B")

	// The owner's own token still works.
	tokB := e.App.TokenClient(e.Admin.CreateToken(e.Project, "read_write"))
	res = tokB.Get(srv)
	require.Equal(t, http.StatusOK, res.StatusCode, e.App.Body(res))
}

// TestVolumesIsolation: a project-A token never sees or acts on project B's volumes or attaches to B's servers.
func TestVolumesIsolation(t *testing.T) {
	ctx := context.Background()
	e := newVolEnv(t)
	web := e.RunningServer("web")
	vb, a := e.createVolume(e.C, map[string]any{"name": "data", "size": 10, "location": "eu-1"})
	e.settle(a, vb.ID, volume.StatusAvailable, nil, "")
	actB := a.ID
	projA := e.Admin.CreateProject("a")
	tokA := e.App.TokenClient(e.Admin.CreateToken(projA, "read_write"))
	va, aa := e.createVolume(tokA, map[string]any{"name": "mine", "size": 10, "location": "eu-1"})
	e.settle(aa, va.ID, volume.StatusAvailable, nil, "")
	before, err := e.App.Ent.Action.Query().Count(ctx)
	require.NoError(t, err)

	vp := "/v1/volumes/" + strconv.FormatInt(vb.ID, 10)
	for _, r := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, vp, nil},
		{http.MethodPut, vp, map[string]any{"name": "x"}},
		{http.MethodDelete, vp, nil},
		{http.MethodGet, vp + "/actions", nil},
		{http.MethodPost, vp + "/actions/attach", map[string]string{"server": "web"}},
		{http.MethodPost, vp + "/actions/detach", nil},
		{http.MethodPost, vp + "/actions/resize", map[string]int{"size": 20}},
		{http.MethodGet, "/v1/actions/" + strconv.FormatInt(actB, 10), nil},
	} {
		res := tokA.Send(r.method, r.path, r.body)
		body := e.App.Body(res)
		require.Equal(t, http.StatusNotFound, res.StatusCode, "%s %s: %s", r.method, r.path, body)
		require.Equal(t, "not_found", errCode2(t, body))
	}
	// A's own volume cannot reach B's server, by name or id.
	for _, ref := range []string{"web", strconv.FormatInt(web.ServerID, 10)} {
		res := tokA.Send(http.MethodPost, "/v1/volumes/"+strconv.FormatInt(va.ID, 10)+"/actions/attach", map[string]string{"server": ref})
		require.Equal(t, http.StatusNotFound, res.StatusCode, e.App.Body(res))
		res.Body.Close()
		res = tokA.Send(http.MethodPost, "/v1/volumes", map[string]any{"name": "x" + ref, "size": 10, "server": ref})
		require.Equal(t, http.StatusNotFound, res.StatusCode, e.App.Body(res))
		res.Body.Close()
	}
	require.Equal(t, []string{"mine"}, e.list(tokA, ""))
	require.Empty(t, e.list(tokA, "server=web"))
	after, err := e.App.Ent.Action.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, before, after, "no action started")
	got, err := e.App.Ent.Volume.Get(ctx, vb.ID)
	require.NoError(t, err)
	require.Equal(t, "data", got.Name)
}

func errCode2(t *testing.T, body string) string {
	t.Helper()
	var e struct{ Error struct{ Code string } }
	require.NoError(t, json.Unmarshal([]byte(body), &e))
	return e.Error.Code
}

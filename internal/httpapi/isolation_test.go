package httpapi_test

import (
	"context"
	"net/http"
	"strconv"
	"testing"

	"github.com/Hexeption/nebla/internal/testutil"
	"github.com/stretchr/testify/require"
)

// TestCrossProjectIsolation: a project-A token never sees or acts on project B's server, actions, metrics or console.
func TestCrossProjectIsolation(t *testing.T) {
	ctx := context.Background()
	e := testutil.NewServerEnv(testutil.NewApp(t, testutil.QueueOnlyJobs(t)))
	b := e.RunningServer("web")
	projA := e.Admin.CreateProject("a")
	tokA := e.App.TokenClient(e.Admin.CreateToken(projA, "read_write"))
	before, err := e.App.Ent.Action.Query().Count(ctx)
	require.NoError(t, err)

	srv := "/v1/servers/" + strconv.FormatInt(b.ServerID, 10)
	for _, r := range []struct{ method, path string }{
		{http.MethodGet, srv},
		{http.MethodPost, srv + "/actions/poweroff"},
		{http.MethodGet, "/v1/actions/" + strconv.FormatInt(b.ActionID, 10)},
		{http.MethodGet, srv + "/actions"},
		{http.MethodGet, srv + "/metrics?type=cpu&timeframe=hour"},
		{http.MethodPost, srv + "/actions/request_console"},
	} {
		res := tokA.Send(r.method, r.path, nil)
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
	res := tokB.Get(srv)
	require.Equal(t, http.StatusOK, res.StatusCode, e.App.Body(res))
}

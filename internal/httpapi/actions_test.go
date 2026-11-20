package httpapi_test

import (
	"context"
	"net/http"
	"strconv"
	"testing"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/stretchr/testify/require"
)

type actionList struct {
	Actions []struct {
		ID       int64  `json:"id"`
		Status   string `json:"status"`
		Progress int    `json:"progress"`
		Error    *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		Finished *string `json:"finished"`
	} `json:"actions"`
	Meta struct {
		Pagination struct {
			Page         int `json:"page"`
			PerPage      int `json:"per_page"`
			TotalEntries int `json:"total_entries"`
		} `json:"pagination"`
	} `json:"meta"`
}

func TestListActionsHTTP(t *testing.T) {
	ctx := context.Background()
	app := testutil.NewApp(t)
	admin := app.LoginAdmin()
	pid := admin.CreateProject("p")
	var ids []int64
	for range 3 {
		a, err := actions.Create(ctx, app.Ent, pid, "x", nil)
		require.NoError(t, err)
		ids = append(ids, a.ID)
	}
	require.NoError(t, actions.Succeed(ctx, app.Ent, ids[0]))
	require.NoError(t, actions.Fail(ctx, app.Ent, ids[1], "boom", "it broke"))
	c := admin.WithProject(pid)
	idsOf := func(l actionList) []int64 {
		out := []int64{}
		for _, a := range l.Actions {
			out = append(out, a.ID)
		}
		return out
	}
	get := func(q string) actionList {
		t.Helper()
		res := c.Get("/v1/actions" + q)
		require.Equal(t, http.StatusOK, res.StatusCode)
		return decode[actionList](t, app, res)
	}

	l := get("")
	require.Equal(t, []int64{ids[2], ids[1], ids[0]}, idsOf(l))
	require.Equal(t, 3, l.Meta.Pagination.TotalEntries)
	require.Equal(t, actions.CodeFailed, l.Actions[1].Error.Code)
	require.Equal(t, actions.Message("x", actions.CodeFailed), l.Actions[1].Error.Message)
	require.Nil(t, l.Actions[0].Error)
	require.Nil(t, l.Actions[0].Finished)
	require.Equal(t, 100, l.Actions[2].Progress)

	require.Equal(t, ids, idsOf(get("?sort=id:asc")))
	require.Equal(t, ids, idsOf(get("?sort=id")))
	require.Equal(t, []int64{ids[2], ids[1], ids[0]}, idsOf(get("?sort=id:desc")))
	require.Equal(t, []int64{ids[1], ids[0]}, idsOf(get("?status=success&status=error")))
	require.Equal(t, []int64{ids[1], ids[0]}, idsOf(get("?status=success,error")))
	require.Equal(t, []int64{ids[2]}, idsOf(get("?status=running")))
	l = get("?per_page=2&page=2")
	require.Equal(t, []int64{ids[0]}, idsOf(l))
	require.Equal(t, 2, l.Meta.Pagination.Page)

	for _, q := range []string{"?status=bogus", "?sort=name", "?sort=id:sideways"} {
		res := c.Get("/v1/actions" + q)
		require.Equal(t, http.StatusUnprocessableEntity, res.StatusCode, q)
		require.Equal(t, "invalid_input", errCode(t, app, res))
	}

	res := c.Get("/v1/actions/" + strconv.FormatInt(ids[1], 10))
	require.Equal(t, http.StatusOK, res.StatusCode)
	res = c.Get("/v1/actions/999999")
	require.Equal(t, http.StatusNotFound, res.StatusCode)
	require.Equal(t, "not_found", errCode(t, app, res))
}

type adminAction struct {
	ID        int64 `json:"id"`
	ProjectID int64 `json:"project_id"`
	Error     *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Detail  string `json:"detail"`
	} `json:"error"`
}

func TestActionErrorDoesNotLeak(t *testing.T) {
	ctx := context.Background()
	e := testutil.NewServerEnv(testutil.NewApp(t, testutil.QueueOnlyJobs(t)))
	s := e.CreateServer("web", nil)
	detail := "clone: vm 4242 on pve-node-7: storage local-lvm: HTTP 500 qmclone"
	require.NoError(t, actions.Fail(ctx, e.App.Ent, s.ActionID, actions.CodeServerMissing, detail))
	leaks := []string{"4242", "pve-node-7", "local-lvm", "qmclone", "HTTP 500", "detail"}
	id := strconv.FormatInt(s.ActionID, 10)
	sid := strconv.FormatInt(s.ServerID, 10)
	member := e.App.TokenClient(e.C.CreateToken(e.Project, "read"))
	for _, c := range []*testutil.Client{e.C, member} {
		for _, path := range []string{"/v1/actions", "/v1/actions/" + id, "/v1/servers/" + sid + "/actions"} {
			res := c.Get(path)
			b := body(res)
			require.Equal(t, http.StatusOK, res.StatusCode, b)
			require.Contains(t, b, `"server_missing"`, path)
			for _, l := range leaks {
				require.NotContains(t, b, l, path)
			}
		}
	}

	res := e.Admin.Get("/v1/admin/actions/" + id)
	require.Equal(t, http.StatusOK, res.StatusCode)
	one := decode[adminAction](t, e.App, res)
	require.Equal(t, e.Project, one.ProjectID)
	require.Equal(t, actions.CodeServerMissing, one.Error.Code)
	require.Equal(t, actions.Message("create_server", actions.CodeServerMissing), one.Error.Message)
	require.Equal(t, detail, one.Error.Detail)

	res = e.Admin.Get("/v1/admin/actions?status=error&project_id=" + strconv.FormatInt(e.Project, 10))
	require.Equal(t, http.StatusOK, res.StatusCode)
	list := decode[struct {
		Actions []adminAction `json:"actions"`
	}](t, e.App, res)
	require.Len(t, list.Actions, 1)
	require.Equal(t, s.ActionID, list.Actions[0].ID)
	require.Equal(t, detail, list.Actions[0].Error.Detail)

	require.Equal(t, http.StatusNotFound, e.Admin.Get("/v1/admin/actions/999999").StatusCode)
	require.Equal(t, http.StatusUnauthorized, member.Get("/v1/admin/actions").StatusCode, "project tokens never reach admin ops")
	e.App.CreateUser("u@example.com", false)
	require.Equal(t, http.StatusForbidden, e.App.Login("u@example.com").Get("/v1/admin/actions/"+id).StatusCode)
}

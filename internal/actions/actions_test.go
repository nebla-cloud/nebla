package actions_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/Hexeption/nebla/internal/actions"
	"github.com/Hexeption/nebla/internal/apierr"
	"github.com/Hexeption/nebla/internal/db"
	"github.com/Hexeption/nebla/internal/ent"
	"github.com/Hexeption/nebla/internal/testutil"
	"github.com/stretchr/testify/require"
)

func project(t *testing.T, c *ent.Client) int64 {
	t.Helper()
	return c.Project.Create().SetName("p").SaveX(context.Background()).ID
}

func TestActionLifecycle(t *testing.T) {
	ctx := context.Background()
	c := testutil.NewEnt(t)
	pid := project(t, c)
	res := []actions.Resource{{ID: 7, Type: "server"}}

	a, err := actions.Create(ctx, c, pid, "create_server", res)
	require.NoError(t, err)
	require.Equal(t, actions.StatusRunning, a.Status)
	require.Zero(t, a.Progress)
	require.Equal(t, res, a.Resources)
	require.Nil(t, a.Finished)
	require.False(t, a.Started.IsZero())

	require.NoError(t, actions.Progress(ctx, c, a.ID, 50))
	got, err := actions.Get(ctx, c, pid, a.ID)
	require.NoError(t, err)
	require.Equal(t, 50, got.Progress)

	require.NoError(t, actions.Succeed(ctx, c, a.ID))
	got, err = actions.Get(ctx, c, pid, a.ID)
	require.NoError(t, err)
	require.Equal(t, actions.StatusSuccess, got.Status)
	require.Equal(t, 100, got.Progress)
	require.NotNil(t, got.Finished)
	require.Nil(t, got.Error)
}

func TestActionFail(t *testing.T) {
	ctx := context.Background()
	c := testutil.NewEnt(t)
	pid := project(t, c)
	a, err := actions.Create(ctx, c, pid, "start_server", nil)
	require.NoError(t, err)
	require.Equal(t, []actions.Resource{}, a.Resources)

	require.NoError(t, actions.Progress(ctx, c, a.ID, 30))
	require.NoError(t, actions.Fail(ctx, c, a.ID, "pve_unreachable", "Proxmox did not answer"))
	got, err := actions.Get(ctx, c, pid, a.ID)
	require.NoError(t, err)
	require.Equal(t, actions.StatusError, got.Status)
	require.Equal(t, 30, got.Progress)
	require.NotNil(t, got.Finished)
	require.Equal(t, &actions.Error{Code: "pve_unreachable", Message: "Proxmox did not answer"}, got.Error)
}

func TestProgressClampsAndNeverDecreases(t *testing.T) {
	ctx := context.Background()
	c := testutil.NewEnt(t)
	pid := project(t, c)
	a, err := actions.Create(ctx, c, pid, "x", nil)
	require.NoError(t, err)
	progress := func() int {
		got, err := actions.Get(ctx, c, pid, a.ID)
		require.NoError(t, err)
		return got.Progress
	}

	require.NoError(t, actions.Progress(ctx, c, a.ID, -5))
	require.Equal(t, 0, progress())
	require.NoError(t, actions.Progress(ctx, c, a.ID, 60))
	require.NoError(t, actions.Progress(ctx, c, a.ID, 40))
	require.Equal(t, 60, progress())
	require.NoError(t, actions.Progress(ctx, c, a.ID, 250))
	require.Equal(t, 100, progress())
	require.Equal(t, actions.StatusRunning, mustGet(t, c, pid, a.ID).Status)
}

func mustGet(t *testing.T, c *ent.Client, pid, id int64) actions.Action {
	t.Helper()
	a, err := actions.Get(context.Background(), c, pid, id)
	require.NoError(t, err)
	return a
}

func TestFinishedActionIsFrozen(t *testing.T) {
	ctx := context.Background()
	c := testutil.NewEnt(t)
	pid := project(t, c)
	a, err := actions.Create(ctx, c, pid, "x", nil)
	require.NoError(t, err)
	require.NoError(t, actions.Fail(ctx, c, a.ID, "boom", "first"))
	before := mustGet(t, c, pid, a.ID)

	require.NoError(t, actions.Succeed(ctx, c, a.ID))
	require.NoError(t, actions.Fail(ctx, c, a.ID, "other", "second"))
	require.NoError(t, actions.Progress(ctx, c, a.ID, 90))
	require.Equal(t, before, mustGet(t, c, pid, a.ID))
}

func TestUnknownActionNotFound(t *testing.T) {
	ctx := context.Background()
	c := testutil.NewEnt(t)
	pid := project(t, c)
	for _, err := range []error{
		actions.Succeed(ctx, c, 999),
		actions.Fail(ctx, c, 999, "a", "b"),
		actions.Progress(ctx, c, 999, 1),
		actions.AttachJob(ctx, c, 999, 1),
		func() error { _, err := actions.Get(ctx, c, pid, 999); return err }(),
	} {
		var ae apierr.E
		require.True(t, errors.As(err, &ae), "%v", err)
		require.Equal(t, "not_found", ae.Code)
	}
}

func TestCreateAndAttachInTx(t *testing.T) {
	ctx := context.Background()
	c, sqlDB := testutil.NewEntSQL(t)
	pid := project(t, c)
	var id int64
	require.NoError(t, db.WithTx(ctx, sqlDB, func(_ *sql.Tx, etx *ent.Client) error {
		a, err := actions.Create(ctx, etx, pid, "x", nil)
		if err != nil {
			return err
		}
		id = a.ID
		return actions.AttachJob(ctx, etx, a.ID, 42)
	}))
	got := mustGet(t, c, pid, id)
	require.Equal(t, int64(42), *got.RiverJobID)
}

func TestListFiltersAndSorts(t *testing.T) {
	ctx := context.Background()
	c := testutil.NewEnt(t)
	pid, other := project(t, c), project(t, c)
	var ids []int64
	for range 3 {
		a, err := actions.Create(ctx, c, pid, "x", nil)
		require.NoError(t, err)
		ids = append(ids, a.ID)
	}
	_, err := actions.Create(ctx, c, other, "x", nil)
	require.NoError(t, err)
	require.NoError(t, actions.Succeed(ctx, c, ids[0]))
	require.NoError(t, actions.Fail(ctx, c, ids[1], "e", "m"))

	list := func(f actions.ListFilter, page, per int) ([]int64, int) {
		as, total, err := actions.List(ctx, c, pid, f, page, per)
		require.NoError(t, err)
		out := []int64{}
		for _, a := range as {
			out = append(out, a.ID)
		}
		return out, total
	}
	got, total := list(actions.ListFilter{}, 1, 25)
	require.Equal(t, []int64{ids[2], ids[1], ids[0]}, got)
	require.Equal(t, 3, total)
	got, _ = list(actions.ListFilter{Ascending: true}, 1, 25)
	require.Equal(t, ids, got)
	got, total = list(actions.ListFilter{Statuses: []string{"success", "error"}}, 1, 25)
	require.Equal(t, []int64{ids[1], ids[0]}, got)
	require.Equal(t, 2, total)
	got, total = list(actions.ListFilter{}, 2, 2)
	require.Equal(t, []int64{ids[0]}, got)
	require.Equal(t, 3, total)
}

func TestActionNotVisibleToOtherProject(t *testing.T) {
	app := testutil.NewApp(t)
	alice := app.LoginAdmin()
	pa, pb := alice.CreateProject("a"), alice.CreateProject("b")
	a, err := actions.Create(context.Background(), app.Ent, pa, "create_server", []actions.Resource{{ID: 1, Type: "server"}})
	require.NoError(t, err)
	path := "/v1/actions/" + strconv.FormatInt(a.ID, 10)

	tokA := app.TokenClient(alice.CreateToken(pa, "read"))
	res := tokA.Get(path)
	body := app.Body(res)
	require.Equal(t, http.StatusOK, res.StatusCode, body)
	var m map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &m))
	started, err := time.Parse(time.RFC3339Nano, m["started"].(string))
	require.NoError(t, err)
	require.True(t, started.Equal(a.Started), "%v != %v", started, a.Started)
	delete(m, "started")
	want := `{"id":` + strconv.FormatInt(a.ID, 10) + `,"command":"create_server","status":"running","progress":0,
		"resources":[{"id":1,"type":"server"}],"error":null,"finished":null}`
	b, _ := json.Marshal(m)
	require.JSONEq(t, want, string(b))

	tokB := app.TokenClient(alice.CreateToken(pb, "read"))
	for _, p := range []string{path, "/v1/actions"} {
		res = tokB.Get(p)
		body = app.Body(res)
		if p == path {
			require.Equal(t, http.StatusNotFound, res.StatusCode, body)
			require.JSONEq(t, `{"error":{"code":"not_found","message":"not found"}}`, body)
		} else {
			require.Equal(t, http.StatusOK, res.StatusCode, body)
			require.JSONEq(t, `{"actions":[],"meta":{"pagination":{"page":1,"per_page":25,"total_entries":0}}}`, body)
		}
	}
	res = alice.WithProject(pb).Get(path)
	require.Equal(t, http.StatusNotFound, res.StatusCode, app.Body(res))
	res = alice.Get(path)
	require.Equal(t, http.StatusNotFound, res.StatusCode, app.Body(res))
}

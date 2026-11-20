package httpapi_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/httpapi"
	"github.com/nebla-cloud/nebla/internal/projects"
	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/stretchr/testify/require"
)

type adminProjectJSON struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	OwnerEmail  string    `json:"owner_email"`
	MemberCount int       `json:"member_count"`
	Created     time.Time `json:"created"`
	Usage       struct {
		Servers int `json:"servers"`
		Vcpu    int `json:"vcpu"`
	} `json:"usage"`
	Members []struct {
		UserID int64  `json:"user_id"`
		Email  string `json:"email"`
		Role   string `json:"role"`
	} `json:"members"`
}

func TestAdminProjectsHTTP(t *testing.T) {
	busy := map[int64]bool{}
	app := testutil.NewApp(t, func(d *httpapi.Deps) {
		d.ProjectHooks.HasServers = func(_ context.Context, _ *ent.Client, id int64) (bool, error) { return busy[id], nil }
		d.ProjectHooks.Usage = func(_ context.Context, _ *ent.Client, id int64) (projects.Usage, error) {
			return projects.Usage{Servers: 2, VCPU: 4}, nil
		}
	})
	admin := app.LoginAdmin()
	app.CreateUser("o@example.com", false)
	o := app.Login("o@example.com")
	pid := o.CreateProject("Web")
	path := "/v1/admin/projects/" + itoa(pid)

	res := admin.Get("/v1/admin/projects")
	require.Equal(t, http.StatusOK, res.StatusCode)
	list := decode[struct {
		Projects []adminProjectJSON `json:"projects"`
	}](t, app, res)
	require.Len(t, list.Projects, 1)
	require.Equal(t, 1, list.Projects[0].MemberCount)
	require.Equal(t, 2, list.Projects[0].Usage.Servers)

	res = admin.Get(path)
	require.Equal(t, http.StatusOK, res.StatusCode)
	got := decode[adminProjectJSON](t, app, res)
	require.Equal(t, "Web", got.Name)
	require.Equal(t, "o@example.com", got.OwnerEmail)
	require.Equal(t, 4, got.Usage.Vcpu)
	require.Equal(t, 1, got.MemberCount)
	require.False(t, got.Created.IsZero())
	require.Len(t, got.Members, 1)
	require.Equal(t, "owner", got.Members[0].Role)
	res = admin.Get("/v1/admin/projects/99999")
	require.Equal(t, http.StatusNotFound, res.StatusCode)
	require.Equal(t, "not_found", errCode(t, app, res))

	busy[pid] = true
	res = admin.Send(http.MethodDelete, path, nil)
	require.Equal(t, http.StatusConflict, res.StatusCode)
	require.Equal(t, "project_not_empty", errCode(t, app, res))
	busy[pid] = false
	res = admin.Send(http.MethodDelete, path, nil)
	require.Equal(t, http.StatusNoContent, res.StatusCode)
	res.Body.Close()
	res = admin.Send(http.MethodDelete, path, nil)
	require.Equal(t, http.StatusNotFound, res.StatusCode)
	res.Body.Close()
}

func TestAdminActionsProjectName(t *testing.T) {
	ctx := context.Background()
	app := testutil.NewApp(t)
	admin := app.LoginAdmin()
	pid := admin.CreateProject("Homelab")
	a, err := actions.Create(ctx, app.Ent, pid, "x", nil)
	require.NoError(t, err)

	type row struct {
		ProjectName string `json:"project_name"`
	}
	res := admin.Get("/v1/admin/actions")
	require.Equal(t, http.StatusOK, res.StatusCode)
	list := decode[struct {
		Actions []row `json:"actions"`
	}](t, app, res)
	require.Len(t, list.Actions, 1)
	require.Equal(t, "Homelab", list.Actions[0].ProjectName)
	res = admin.Get("/v1/admin/actions/" + itoa(a.ID))
	require.Equal(t, http.StatusOK, res.StatusCode)
	require.Equal(t, "Homelab", decode[row](t, app, res).ProjectName)
}

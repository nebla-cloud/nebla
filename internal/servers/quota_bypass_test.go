package servers_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Hexeption/nebla/internal/config"
	"github.com/Hexeption/nebla/internal/httpapi"
	"github.com/Hexeption/nebla/internal/projects"
	"github.com/Hexeption/nebla/internal/testutil"
	"github.com/stretchr/testify/require"
)

func quotaEnv(t *testing.T, dq projects.Quota, maxProjects int) *testutil.ServerEnv {
	t.Helper()
	app := testutil.NewApp(t, testutil.QueueOnlyJobs(t), func(d *httpapi.Deps) {
		d.Config = config.Config{SecretKey: d.Config.SecretKey, DefaultQuota: dq, MaxProjectsPerUser: maxProjects}
	})
	return testutil.NewServerEnv(app)
}

func createServer(c *testutil.Client, name string) *http.Response {
	return c.Send(http.MethodPost, "/v1/servers", map[string]any{"name": name, "server_type": "small", "image": "ubuntu", "location": "eu-1"})
}

// A non-admin cannot dodge an admin-set project quota by creating a fresh project.
func TestQuotaBypassViaNewProjectBlocked(t *testing.T) {
	e := quotaEnv(t, projects.Quota{MaxServers: 1}, 0)
	e.App.CreateUser("u@example.com", false)
	u := e.App.Login("u@example.com")
	p1 := u.CreateProject("p1")
	u1 := u.WithProject(p1)

	res := createServer(u1, "a1")
	require.Equal(t, http.StatusCreated, res.StatusCode, body(t, res))
	res.Body.Close()
	res = createServer(u1, "a2")
	raw := body(t, res)
	require.Equal(t, http.StatusUnprocessableEntity, res.StatusCode, raw)
	require.Contains(t, raw, "quota_exceeded")

	u2 := u.WithProject(u.CreateProject("p2"))
	res = createServer(u2, "b1")
	require.Equal(t, http.StatusCreated, res.StatusCode, body(t, res))
	res.Body.Close()
	res = createServer(u2, "b2")
	raw = body(t, res)
	require.Equal(t, http.StatusUnprocessableEntity, res.StatusCode, raw)
	require.Contains(t, raw, "quota_exceeded")
}

func TestProjectCapViaAPI(t *testing.T) {
	e := quotaEnv(t, projects.Quota{}, 2)
	e.App.CreateUser("u@example.com", false)
	u := e.App.Login("u@example.com")
	u.CreateProject("p1")
	u.CreateProject("p2")
	res := u.Send(http.MethodPost, "/v1/projects", map[string]string{"name": "p3"})
	raw := body(t, res)
	require.Equal(t, http.StatusUnprocessableEntity, res.StatusCode, raw)
	require.Contains(t, raw, "project limit reached")
}

func TestAdminProjectsUnlimitedViaAPI(t *testing.T) {
	e := quotaEnv(t, projects.Quota{MaxServers: 1}, 1)
	for _, n := range []string{"x", "y", "z"} {
		e.Admin.CreateProject(n)
	}
	res := e.Admin.Send(http.MethodGet, "/v1/admin/projects", nil)
	var out struct {
		Projects []struct {
			Name  string         `json:"name"`
			Quota map[string]int `json:"quota"`
		} `json:"projects"`
	}
	require.Equal(t, http.StatusOK, res.StatusCode)
	require.NoError(t, json.NewDecoder(res.Body).Decode(&out))
	res.Body.Close()
	for _, p := range out.Projects {
		require.Equal(t, map[string]int{"max_servers": 0, "max_vcpu": 0, "max_memory_mb": 0, "max_disk_gb": 0, "max_ips": 0}, p.Quota, p.Name)
	}
	require.GreaterOrEqual(t, len(out.Projects), 4)
}

func TestAdminListProjects(t *testing.T) {
	e := newEnv(t)
	e.App.CreateUser("u@example.com", false)
	u := e.App.Login("u@example.com")
	u.CreateProject("mine")

	type list struct {
		Projects []struct {
			ID         int64  `json:"id"`
			Name       string `json:"name"`
			OwnerEmail string `json:"owner_email"`
		} `json:"projects"`
		Meta struct {
			Pagination struct {
				TotalEntries int `json:"total_entries"`
			} `json:"pagination"`
		} `json:"meta"`
	}
	got, _ := expect[list](t, e.Admin.Send(http.MethodGet, "/v1/admin/projects", nil), http.StatusOK)
	require.Equal(t, 2, got.Meta.Pagination.TotalEntries)
	require.Equal(t, "p", got.Projects[0].Name)
	require.Equal(t, "mine", got.Projects[1].Name)
	require.Equal(t, "u@example.com", got.Projects[1].OwnerEmail)

	res := u.Send(http.MethodGet, "/v1/admin/projects", nil)
	require.Equal(t, http.StatusForbidden, res.StatusCode, body(t, res))
}

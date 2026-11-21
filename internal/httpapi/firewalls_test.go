package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/stretchr/testify/require"
)

type fwRule struct {
	Direction      string   `json:"direction"`
	Protocol       string   `json:"protocol"`
	Port           *string  `json:"port"`
	SourceIPs      []string `json:"source_ips"`
	DestinationIPs []string `json:"destination_ips"`
	Description    string   `json:"description"`
}

type fwResource struct {
	Type   string `json:"type"`
	Server *struct {
		ID int64 `json:"id"`
	} `json:"server"`
	LabelSelector *struct {
		Selector string `json:"selector"`
	} `json:"label_selector"`
	AppliedToResources []struct {
		Type   string `json:"type"`
		Server struct {
			ID int64 `json:"id"`
		} `json:"server"`
	} `json:"applied_to_resources"`
}

type fwBody struct {
	ID        int64             `json:"id"`
	Name      string            `json:"name"`
	Labels    map[string]string `json:"labels"`
	Rules     []fwRule          `json:"rules"`
	AppliedTo []fwResource      `json:"applied_to"`
	Created   string            `json:"created"`
}

type fwCreated struct {
	Firewall fwBody `json:"firewall"`
	fwActions
}

type fwList struct {
	Firewalls []fwBody `json:"firewalls"`
	Meta      struct {
		Pagination struct {
			TotalEntries int `json:"total_entries"`
		} `json:"pagination"`
	} `json:"meta"`
}

type fwActions struct {
	Actions []struct {
		ID        int64  `json:"id"`
		Command   string `json:"command"`
		Resources []struct {
			ID   int64  `json:"id"`
			Type string `json:"type"`
		} `json:"resources"`
	} `json:"actions"`
}

func fwPath(id int64, rest string) string { return "/v1/firewalls/" + itoa(id) + rest }

func serverRes(id int64) map[string]any {
	return map[string]any{"type": "server", "server": map[string]any{"id": id}}
}

func selectorRes(sel string) map[string]any {
	return map[string]any{"type": "label_selector", "label_selector": map[string]any{"selector": sel}}
}

func sshRule() map[string]any {
	return map[string]any{"direction": "in", "protocol": "tcp", "port": "22", "source_ips": []string{"0.0.0.0/0", "::/0"}, "description": "ssh"}
}

func TestFirewallsCRUD(t *testing.T) {
	e := testutil.NewServerEnv(testutil.NewApp(t, testutil.QueueOnlyJobs(t)))
	web := e.RunningServer("web")
	db := e.RunningServer("db")
	res := e.C.Send(http.MethodPut, "/v1/servers/"+itoa(db.ServerID), map[string]any{"labels": map[string]string{"role": "db"}})
	require.Equal(t, http.StatusOK, res.StatusCode, e.App.Body(res))

	res = e.C.Send(http.MethodPost, "/v1/firewalls", map[string]any{
		"name": "web", "labels": map[string]string{"env": "prod"},
		"rules":    []any{sshRule(), map[string]any{"direction": "out", "protocol": "icmp", "destination_ips": []string{"10.0.0.0/8"}}},
		"apply_to": []any{serverRes(web.ServerID), selectorRes("role=db")},
	})
	body := e.App.Body(res)
	require.Equal(t, http.StatusCreated, res.StatusCode, body)
	var created fwCreated
	require.NoError(t, json.Unmarshal([]byte(body), &created))
	fw := created.Firewall
	require.Equal(t, "web", fw.Name)
	require.Equal(t, map[string]string{"env": "prod"}, fw.Labels)
	require.Len(t, fw.Rules, 2)
	require.Equal(t, "22", *fw.Rules[0].Port)
	require.Equal(t, []string{"0.0.0.0/0", "::/0"}, fw.Rules[0].SourceIPs)
	require.Nil(t, fw.Rules[0].DestinationIPs)
	require.Equal(t, "ssh", fw.Rules[0].Description)
	require.Nil(t, fw.Rules[1].Port)
	require.Equal(t, []string{"10.0.0.0/8"}, fw.Rules[1].DestinationIPs)
	require.Nil(t, fw.Rules[1].SourceIPs)
	require.Len(t, fw.AppliedTo, 2)
	require.Equal(t, web.ServerID, fw.AppliedTo[0].Server.ID)
	require.Nil(t, fw.AppliedTo[0].AppliedToResources)
	require.Equal(t, "role=db", fw.AppliedTo[1].LabelSelector.Selector)
	require.Len(t, fw.AppliedTo[1].AppliedToResources, 1)
	require.Equal(t, db.ServerID, fw.AppliedTo[1].AppliedToResources[0].Server.ID)
	require.Len(t, created.Actions, 2)
	for _, a := range created.Actions {
		require.Equal(t, "apply_firewalls", a.Command)
	}

	// The server shows its effective firewalls and why.
	res = e.C.Get("/v1/servers/" + itoa(db.ServerID))
	body = e.App.Body(res)
	require.Equal(t, http.StatusOK, res.StatusCode, body)
	var srv struct {
		Firewalls []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
			Via  string `json:"via"`
		} `json:"firewalls"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &srv))
	require.Len(t, srv.Firewalls, 1)
	require.Equal(t, fw.ID, srv.Firewalls[0].ID)
	require.Equal(t, "role=db", srv.Firewalls[0].Via)

	res = e.C.Get(fwPath(fw.ID, ""))
	got := decode[fwBody](t, e.App, res)
	require.Equal(t, fw.ID, got.ID)

	res = e.C.Get("/v1/firewalls?name=web")
	list := decode[fwList](t, e.App, res)
	require.Len(t, list.Firewalls, 1)
	require.Equal(t, 1, list.Meta.Pagination.TotalEntries)
	res = e.C.Get("/v1/firewalls?label_selector=env%3Dstaging")
	require.Empty(t, decode[fwList](t, e.App, res).Firewalls)

	res = e.C.Send(http.MethodPut, fwPath(fw.ID, ""), map[string]any{"name": "edge"})
	got = decode[fwBody](t, e.App, res)
	require.Equal(t, "edge", got.Name)
	require.Equal(t, map[string]string{"env": "prod"}, got.Labels, "labels kept when omitted")

	res = e.C.Send(http.MethodPost, fwPath(fw.ID, "/actions/set_rules"), map[string]any{"rules": []any{sshRule()}})
	require.Equal(t, http.StatusCreated, res.StatusCode)
	require.Len(t, decode[fwActions](t, e.App, res).Actions, 2)

	res = e.C.Send(http.MethodPost, fwPath(fw.ID, "/actions/remove_from_resources"),
		map[string]any{"remove_from": []any{serverRes(web.ServerID)}})
	require.Equal(t, http.StatusCreated, res.StatusCode)
	acts := decode[fwActions](t, e.App, res).Actions
	require.Len(t, acts, 1)
	require.Equal(t, web.ServerID, acts[0].Resources[0].ID)

	res = e.C.Send(http.MethodPost, fwPath(fw.ID, "/actions/apply_to_resources"),
		map[string]any{"apply_to": []any{serverRes(web.ServerID)}})
	require.Equal(t, http.StatusCreated, res.StatusCode)
	require.Len(t, decode[fwActions](t, e.App, res).Actions, 1)

	res = e.C.Get(fwPath(fw.ID, "/actions"))
	require.Equal(t, http.StatusOK, res.StatusCode)
	all := decode[actionList](t, e.App, res)
	require.Equal(t, 6, all.Meta.Pagination.TotalEntries)

	res = e.C.Send(http.MethodDelete, fwPath(fw.ID, ""), nil)
	require.Equal(t, http.StatusOK, res.StatusCode)
	require.Len(t, decode[fwActions](t, e.App, res).Actions, 2)
	res = e.C.Get(fwPath(fw.ID, ""))
	require.Equal(t, http.StatusNotFound, res.StatusCode)
	res.Body.Close()
}

func TestFirewallRuleDirectionLists(t *testing.T) {
	e := testutil.NewServerEnv(testutil.NewApp(t, testutil.QueueOnlyJobs(t)))
	for _, r := range []map[string]any{
		{"direction": "in", "protocol": "tcp", "port": "22", "destination_ips": []string{"0.0.0.0/0"}},
		{"direction": "out", "protocol": "tcp", "port": "22", "source_ips": []string{"0.0.0.0/0"}},
		{"direction": "in", "protocol": "tcp", "port": "22"},
	} {
		res := e.C.Send(http.MethodPost, "/v1/firewalls", map[string]any{"name": "fw", "rules": []any{r}})
		require.Equal(t, http.StatusUnprocessableEntity, res.StatusCode)
		require.Equal(t, "invalid_input", errCode(t, e.App, res))
	}
	res := e.C.Send(http.MethodPost, "/v1/firewalls", map[string]any{"name": "fw"})
	require.Equal(t, http.StatusCreated, res.StatusCode, e.App.Body(res))
	res = e.C.Send(http.MethodPost, "/v1/firewalls", map[string]any{"name": "fw"})
	require.Equal(t, http.StatusConflict, res.StatusCode)
	require.Equal(t, "name_taken", errCode(t, e.App, res))
}

func TestFirewallsPermissions(t *testing.T) {
	e := testutil.NewServerEnv(testutil.NewApp(t, testutil.QueueOnlyJobs(t)))
	s := e.RunningServer("web")
	res := e.C.Send(http.MethodPost, "/v1/firewalls", map[string]any{"name": "fw"})
	require.Equal(t, http.StatusCreated, res.StatusCode)
	fw := decode[fwCreated](t, e.App, res)
	id := fw.Firewall.ID

	e.App.CreateUser("ro@example.com", false)
	token := e.Admin.Invite(e.Project, "ro@example.com", "read_only")
	ro := e.App.Login("ro@example.com")
	res = ro.Send(http.MethodPost, "/v1/invites/"+token+"/accept", nil)
	require.Equal(t, http.StatusOK, res.StatusCode, e.App.Body(res))
	readToken := e.App.TokenClient(e.Admin.CreateToken(e.Project, "read"))

	for name, c := range map[string]*testutil.Client{"read_only member": ro.WithProject(e.Project), "read token": readToken} {
		for _, p := range []string{"/v1/firewalls", fwPath(id, ""), fwPath(id, "/actions")} {
			res := c.Get(p)
			require.Equal(t, http.StatusOK, res.StatusCode, "%s GET %s", name, p)
			res.Body.Close()
		}
		for _, r := range []struct {
			method, path string
			body         any
		}{
			{http.MethodPost, "/v1/firewalls", map[string]any{"name": "x"}},
			{http.MethodPut, fwPath(id, ""), map[string]any{"name": "x"}},
			{http.MethodDelete, fwPath(id, ""), nil},
			{http.MethodPost, fwPath(id, "/actions/set_rules"), map[string]any{"rules": []any{}}},
			{http.MethodPost, fwPath(id, "/actions/apply_to_resources"), map[string]any{"apply_to": []any{serverRes(s.ServerID)}}},
			{http.MethodPost, fwPath(id, "/actions/remove_from_resources"), map[string]any{"remove_from": []any{serverRes(s.ServerID)}}},
		} {
			res := c.Send(r.method, r.path, r.body)
			require.Equal(t, http.StatusForbidden, res.StatusCode, "%s %s %s", name, r.method, r.path)
			require.Equal(t, "forbidden", errCode(t, e.App, res))
		}
	}
	res = e.C.Get(fwPath(id, ""))
	require.Equal(t, "fw", decode[fwBody](t, e.App, res).Name, "nothing changed")
}

func TestFirewallsCrossProject(t *testing.T) {
	ctx := context.Background()
	e := testutil.NewServerEnv(testutil.NewApp(t, testutil.QueueOnlyJobs(t)))
	b := e.RunningServer("web")
	res := e.C.Send(http.MethodPost, "/v1/firewalls", map[string]any{"name": "fw"})
	require.Equal(t, http.StatusCreated, res.StatusCode)
	bFW := decode[fwCreated](t, e.App, res).Firewall.ID

	projA := e.Admin.CreateProject("a")
	a := e.Admin.WithProject(projA)
	res = a.Send(http.MethodPost, "/v1/firewalls", map[string]any{"name": "fw"})
	require.Equal(t, http.StatusCreated, res.StatusCode)
	aFW := decode[fwCreated](t, e.App, res).Firewall.ID
	before, err := e.App.Ent.Action.Query().Count(ctx)
	require.NoError(t, err)

	for _, r := range []struct {
		method, path string
		body         any
	}{
		{http.MethodPost, "/v1/firewalls", map[string]any{"name": "x", "apply_to": []any{serverRes(b.ServerID)}}},
		{http.MethodPost, fwPath(aFW, "/actions/apply_to_resources"), map[string]any{"apply_to": []any{serverRes(b.ServerID)}}},
		{http.MethodPost, fwPath(aFW, "/actions/remove_from_resources"), map[string]any{"remove_from": []any{serverRes(b.ServerID)}}},
		{http.MethodPost, "/v1/servers", map[string]any{"name": "x", "server_type": "small", "image": "ubuntu", "location": "eu-1",
			"firewalls": []any{map[string]any{"firewall": bFW}}}},
	} {
		res := a.Send(r.method, r.path, r.body)
		require.Equal(t, http.StatusNotFound, res.StatusCode, "%s %s: %s", r.method, r.path, e.App.Body(res))
	}
	after, err := e.App.Ent.Action.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, before, after)
	res = a.Get("/v1/firewalls")
	require.Len(t, decode[fwList](t, e.App, res).Firewalls, 1)
}

func TestCreateServerWithFirewall(t *testing.T) {
	e := testutil.NewServerEnv(testutil.NewApp(t, testutil.QueueOnlyJobs(t)))
	res := e.C.Send(http.MethodPost, "/v1/firewalls", map[string]any{"name": "fw", "rules": []any{sshRule()}})
	require.Equal(t, http.StatusCreated, res.StatusCode)
	id := decode[fwCreated](t, e.App, res).Firewall.ID
	s := e.CreateServer("web", map[string]any{"firewalls": []any{map[string]any{"firewall": id}}})
	res = e.C.Get("/v1/servers/" + itoa(s.ServerID))
	body := e.App.Body(res)
	require.Equal(t, http.StatusOK, res.StatusCode, body)
	require.JSONEq(t, `[{"id":`+itoa(id)+`,"name":"fw","via":"direct"}]`, string(jsonField(t, body, "firewalls")))
}

func TestAdminLocationFirewallEnabled(t *testing.T) {
	e := testutil.NewServerEnv(testutil.NewApp(t, testutil.QueueOnlyJobs(t)))
	res := e.Admin.Get("/v1/admin/locations/" + itoa(e.Location))
	body := e.App.Body(res)
	require.Equal(t, http.StatusOK, res.StatusCode, body)
	require.Equal(t, "null", string(jsonField(t, body, "firewall_enabled")))
	require.NoError(t, e.App.Ent.Location.UpdateOneID(e.Location).SetFirewallEnabled(false).Exec(context.Background()))
	res = e.Admin.Get("/v1/admin/locations/" + itoa(e.Location))
	require.Equal(t, "false", string(jsonField(t, e.App.Body(res), "firewall_enabled")))
}

func jsonField(t *testing.T, body, key string) json.RawMessage {
	t.Helper()
	var m map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(body), &m))
	v, ok := m[key]
	require.True(t, ok, "missing %s in %s", key, body)
	return v
}

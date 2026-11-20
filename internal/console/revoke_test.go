package console_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/nebla-cloud/nebla/internal/httpapi"
	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/stretchr/testify/require"
)

const recheck = 100 * time.Millisecond

type revokeEnv struct {
	e      *testutil.ServerEnv
	server int64
	member *testutil.Client
	userID int64
}

// newRevokeEnv adds an "admin" member to the project; consoles re-check access every 100ms.
func newRevokeEnv(t *testing.T) revokeEnv {
	t.Helper()
	app := testutil.NewApp(t, testutil.QueueOnlyJobs(t), func(d *httpapi.Deps) { d.ConsoleRecheck = recheck })
	e := testutil.NewServerEnv(app)
	s := e.RunningServer("web")
	u := app.CreateUser("m@example.test", false)
	inv := e.C.Invite(e.Project, "m@example.test", "admin")
	mc := app.Login("m@example.test")
	res := mc.Send(http.MethodPost, "/v1/invites/"+inv+"/accept", nil)
	require.Equal(t, http.StatusOK, res.StatusCode, app.Body(res))
	res.Body.Close()
	return revokeEnv{e: e, server: s.ServerID, member: mc.WithProject(e.Project), userID: u.ID}
}

func (r revokeEnv) memberPath() string {
	return "/v1/projects/" + strconv.FormatInt(r.e.Project, 10) + "/members/" + strconv.FormatInt(r.userID, 10)
}

// open starts a console as c and returns a channel that gets the close status.
func open(t *testing.T, r revokeEnv, c *testutil.Client) <-chan websocket.StatusCode {
	t.Helper()
	g := request(t, r.e, c, r.server)
	conn, _, err := dial(t, g.WSSURL, nil)
	require.NoError(t, err)
	t.Cleanup(func() { conn.CloseNow() })
	out := make(chan websocket.StatusCode, 1)
	go func() {
		for {
			if _, _, err := conn.Read(context.Background()); err != nil {
				var ce websocket.CloseError
				if errors.As(err, &ce) {
					out <- ce.Code
				} else {
					out <- -1
				}
				return
			}
		}
	}()
	return out
}

func requireClosed(t *testing.T, ch <-chan websocket.StatusCode) {
	t.Helper()
	select {
	case code := <-ch:
		require.Equal(t, websocket.StatusPolicyViolation, code)
	case <-time.After(5 * time.Second):
		t.Fatal("console stayed open after access was revoked")
	}
}

func TestConsoleStaysOpenWhileAllowed(t *testing.T) {
	r := newRevokeEnv(t)
	ch := open(t, r, r.member)
	select {
	case code := <-ch:
		t.Fatalf("console closed with %d", code)
	case <-time.After(5 * recheck):
	}
}

func TestConsoleClosesWhenMemberRemoved(t *testing.T) {
	r := newRevokeEnv(t)
	ch := open(t, r, r.member)
	res := r.e.C.Send(http.MethodDelete, r.memberPath(), nil)
	require.Equal(t, http.StatusNoContent, res.StatusCode, r.e.App.Body(res))
	requireClosed(t, ch)
}

func TestConsoleClosesWhenMemberMadeReadOnly(t *testing.T) {
	r := newRevokeEnv(t)
	ch := open(t, r, r.member)
	res := r.e.C.Send(http.MethodPut, r.memberPath(), map[string]string{"role": "read_only"})
	require.Equal(t, http.StatusOK, res.StatusCode, r.e.App.Body(res))
	requireClosed(t, ch)
}

func TestConsoleClosesWhenSessionEnds(t *testing.T) {
	r := newRevokeEnv(t)
	ch := open(t, r, r.member)
	res := r.member.Send(http.MethodPost, "/v1/auth/logout", nil)
	require.Less(t, res.StatusCode, 300, r.e.App.Body(res))
	requireClosed(t, ch)
}

func TestConsoleClosesWhenTokenRevoked(t *testing.T) {
	r := newRevokeEnv(t)
	tok := r.e.C.CreateToken(r.e.Project, "read_write")
	ch := open(t, r, r.e.App.TokenClient(tok))
	res := r.e.C.Get("/v1/projects/" + strconv.FormatInt(r.e.Project, 10) + "/tokens")
	var list struct {
		Tokens []struct{ ID int64 } `json:"tokens"`
	}
	require.NoError(t, json.Unmarshal([]byte(r.e.App.Body(res)), &list))
	require.NotEmpty(t, list.Tokens)
	res = r.e.C.Send(http.MethodDelete, "/v1/projects/"+strconv.FormatInt(r.e.Project, 10)+"/tokens/"+
		strconv.FormatInt(list.Tokens[len(list.Tokens)-1].ID, 10), nil)
	require.Equal(t, http.StatusNoContent, res.StatusCode, r.e.App.Body(res))
	requireClosed(t, ch)
}

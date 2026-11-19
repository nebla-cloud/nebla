package console_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Hexeption/nebla/internal/auth"
	"github.com/Hexeption/nebla/internal/ent/consoletoken"
	"github.com/Hexeption/nebla/internal/testutil"
	"github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

type grant struct {
	WSSURL   string `json:"wss_url"`
	Password string `json:"password"`
}

func env(t *testing.T) (*testutil.ServerEnv, testutil.CreatedServer) {
	t.Helper()
	e := testutil.NewServerEnv(testutil.NewApp(t, testutil.QueueOnlyJobs(t)))
	return e, e.RunningServer("web")
}

func consolePath(id int64) string {
	return "/v1/servers/" + strconv.FormatInt(id, 10) + "/actions/request_console"
}

func request(t *testing.T, e *testutil.ServerEnv, c *testutil.Client, id int64) grant {
	t.Helper()
	res := c.Send(http.MethodPost, consolePath(id), nil)
	body := e.App.Body(res)
	require.Equal(t, http.StatusCreated, res.StatusCode, body)
	var g grant
	require.NoError(t, json.Unmarshal([]byte(body), &g))
	return g
}

func dial(t *testing.T, url string, opts *websocket.DialOptions) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return websocket.Dial(ctx, url, opts)
}

func require404(t *testing.T, res *http.Response, err error) {
	t.Helper()
	require.Error(t, err)
	require.NotNil(t, res)
	require.Equal(t, http.StatusNotFound, res.StatusCode)
	b, _ := io.ReadAll(res.Body)
	require.JSONEq(t, `{"error":{"code":"not_found","message":"not found"}}`, string(b))
}

func TestConsoleRoundTrip(t *testing.T) {
	e, s := env(t)
	g := request(t, e, e.C, s.ServerID)
	require.True(t, strings.HasPrefix(g.WSSURL, "ws"+strings.TrimPrefix(e.App.URL, "http")+"/v1/console/"), g.WSSURL)
	require.True(t, strings.HasPrefix(g.Password, "PVEVNC:"), g.Password)
	var full map[string]any
	res := e.C.Send(http.MethodPost, consolePath(s.ServerID), nil)
	require.NoError(t, json.Unmarshal([]byte(e.App.Body(res)), &full))
	require.NotContains(t, full, "action")

	c, _, err := dial(t, g.WSSURL, &websocket.DialOptions{Subprotocols: []string{"binary"}})
	require.NoError(t, err)
	defer c.CloseNow()
	require.Equal(t, "binary", c.Subprotocol())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, msg := range []string{"RFB 003.008\n", "second frame"} {
		require.NoError(t, c.Write(ctx, websocket.MessageBinary, []byte(msg)))
		typ, b, err := c.Read(ctx)
		require.NoError(t, err)
		require.Equal(t, websocket.MessageBinary, typ)
		require.Equal(t, msg, string(b))
	}
	require.NoError(t, c.Close(websocket.StatusNormalClosure, ""))
}

func TestConsoleTokenSingleUse(t *testing.T) {
	ctx := context.Background()
	e, s := env(t)
	g := request(t, e, e.C, s.ServerID)
	c, _, err := dial(t, g.WSSURL, nil)
	require.NoError(t, err)
	c.Close(websocket.StatusNormalClosure, "")

	_, res, err := dial(t, g.WSSURL, nil)
	require404(t, res, err)

	_, res, err = dial(t, strings.Replace(e.App.URL, "http", "ws", 1)+"/v1/console/not-a-token", nil)
	require404(t, res, err)

	// Only the hash is stored.
	g = request(t, e, e.C, s.ServerID)
	raw := g.WSSURL[strings.LastIndex(g.WSSURL, "/")+1:]
	n, err := e.App.Ent.ConsoleToken.Query().Where(consoletoken.ID(auth.HashSecret(raw))).Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n)

	// An expired token is rejected.
	_, err = e.App.SQL.ExecContext(ctx, `update console_tokens set expires_at = now() - interval '1 second' where token_hash = $1`, auth.HashSecret(raw))
	require.NoError(t, err)
	_, res, err = dial(t, g.WSSURL, nil)
	require404(t, res, err)
}

func TestConsoleConcurrentUseOnce(t *testing.T) {
	e, s := env(t)
	g := request(t, e, e.C, s.ServerID)
	type out struct {
		c   *websocket.Conn
		err error
	}
	ch := make(chan out, 4)
	for range 4 {
		go func() {
			c, _, err := dial(t, g.WSSURL, nil)
			ch <- out{c, err}
		}()
	}
	ok := 0
	for range 4 {
		o := <-ch
		if o.err == nil {
			ok++
			o.c.CloseNow()
		}
	}
	require.Equal(t, 1, ok)
}

func TestConsoleRejectsOtherProject(t *testing.T) {
	ctx := context.Background()
	e, s := env(t)
	projB := e.Admin.CreateProject("b")
	tokB := e.App.TokenClient(e.Admin.CreateToken(projB, "read_write"))
	res := tokB.Send(http.MethodPost, consolePath(s.ServerID), nil)
	body := e.App.Body(res)
	require.Equal(t, http.StatusNotFound, res.StatusCode, body)
	require.JSONEq(t, `{"error":{"code":"not_found","message":"not found"}}`, body)

	// A token whose project no longer owns the server is rejected at connect time.
	g := request(t, e, e.C, s.ServerID)
	raw := g.WSSURL[strings.LastIndex(g.WSSURL, "/")+1:]
	_, err := e.App.SQL.ExecContext(ctx, `update console_tokens set project_id = $1 where token_hash = $2`, projB, auth.HashSecret(raw))
	require.NoError(t, err)
	_, res, err = dial(t, g.WSSURL, nil)
	require404(t, res, err)

	// A token for a server deleted since is rejected.
	g = request(t, e, e.C, s.ServerID)
	res = e.C.Send(http.MethodDelete, "/v1/servers/"+strconv.FormatInt(s.ServerID, 10), nil)
	require.Equal(t, http.StatusOK, res.StatusCode, e.App.Body(res))
	_, res, err = dial(t, g.WSSURL, nil)
	require404(t, res, err)
}

func TestConsoleRequiresRunningAndWrite(t *testing.T) {
	e, s := env(t)
	ro := e.App.TokenClient(e.Admin.CreateToken(e.Project, "read"))
	res := ro.Send(http.MethodPost, consolePath(s.ServerID), nil)
	require.Equal(t, http.StatusForbidden, res.StatusCode, e.App.Body(res))

	require.NoError(t, e.App.Ent.Server.UpdateOneID(s.ServerID).SetStatus("off").Exec(context.Background()))
	res = e.C.Send(http.MethodPost, consolePath(s.ServerID), nil)
	body := e.App.Body(res)
	require.Equal(t, http.StatusConflict, res.StatusCode, body)
	require.Contains(t, body, `"server_not_running"`)

	// A token minted while running is refused once the server stops.
	require.NoError(t, e.App.Ent.Server.UpdateOneID(s.ServerID).SetStatus("running").Exec(context.Background()))
	g := request(t, e, e.C, s.ServerID)
	require.NoError(t, e.App.Ent.Server.UpdateOneID(s.ServerID).SetStatus("off").Exec(context.Background()))
	_, res, err := dial(t, g.WSSURL, nil)
	require404(t, res, err)
}

func TestConsoleRejectsForeignOrigin(t *testing.T) {
	e, s := env(t)
	g := request(t, e, e.C, s.ServerID)
	_, res, err := dial(t, g.WSSURL, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {"https://evil.example"}}})
	require.Error(t, err)
	require.NotNil(t, res)
	require.Equal(t, http.StatusForbidden, res.StatusCode)

	g = request(t, e, e.C, s.ServerID)
	c, _, err := dial(t, g.WSSURL, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {e.App.URL}}})
	require.NoError(t, err)
	c.CloseNow()
}

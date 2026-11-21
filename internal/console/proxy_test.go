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

package console_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/nebla-cloud/nebla/internal/auth"
	"github.com/nebla-cloud/nebla/internal/console"
	"github.com/nebla-cloud/nebla/internal/ent/consoletoken"
	"github.com/nebla-cloud/nebla/internal/httpapi"
	"github.com/nebla-cloud/nebla/internal/testutil"
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

// syncBuf is a goroutine-safe log sink.
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func TestConsoleDialFailureDoesNotLogTicket(t *testing.T) {
	ctx := context.Background()
	logs := &syncBuf{}
	e := testutil.NewServerEnv(testutil.NewApp(t, testutil.QueueOnlyJobs(t), func(d *httpapi.Deps) {
		d.Logger = slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}))
	s := e.RunningServer("web")
	g := request(t, e, e.C, s.ServerID)

	// Point the location at a closed port so the upstream dial fails.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	closed := "https://" + l.Addr().String()
	require.NoError(t, l.Close())
	require.NoError(t, e.App.Ent.Location.UpdateOneID(e.Location).SetPveURL(closed).Exec(ctx))

	_, res, err := dial(t, g.WSSURL, nil)
	require.Error(t, err)
	require.NotNil(t, res)
	require.Equal(t, http.StatusBadGateway, res.StatusCode)
	out := logs.String()
	require.Contains(t, out, "console: dial proxmox")
	require.NotContains(t, out, g.Password)
	require.NotContains(t, out, url.QueryEscape(g.Password))
	require.NotContains(t, out, "vncticket")
}

func TestConsolePlainGETKeepsToken(t *testing.T) {
	e, s := env(t)
	g := request(t, e, e.C, s.ServerID)
	res, err := http.Get("http" + strings.TrimPrefix(g.WSSURL, "ws"))
	require.NoError(t, err)
	body := e.App.Body(res)
	require.Equal(t, http.StatusBadRequest, res.StatusCode, body)
	require.Contains(t, body, `"invalid_input"`)

	c, _, err := dial(t, g.WSSURL, nil)
	require.NoError(t, err)
	c.CloseNow()
}

func TestConsoleTokenTTL(t *testing.T) {
	ctx := context.Background()
	e, s := env(t)
	before := time.Now()
	request(t, e, e.C, s.ServerID)
	row, err := e.App.Ent.ConsoleToken.Query().Only(ctx)
	require.NoError(t, err)
	require.WithinDuration(t, before.Add(console.TokenTTL), row.ExpiresAt, 2*time.Second)
	require.Equal(t, 10*time.Second, console.TokenTTL)
}

// TestConsoleRequiresOwnedVM checks a reused vmid holding a foreign or untagged VM gets no console.
func TestConsoleRequiresOwnedVM(t *testing.T) {
	e, s := env(t)
	row, err := e.App.Ent.Server.Get(context.Background(), s.ServerID)
	require.NoError(t, err)
	for _, tags := range []string{"nebla;nb-srv-99999", ""} {
		e.App.PVE.SetVMConfig(*row.Vmid, "tags", tags)
		res := e.C.Send(http.MethodPost, consolePath(s.ServerID), nil)
		body := e.App.Body(res)
		require.Equal(t, http.StatusConflict, res.StatusCode, "tags %q: %s", tags, body)
		require.Contains(t, body, `"server_missing"`)
	}
}

// TestConsoleFollowsVMNode checks the ticket and the dial use the node Proxmox lists, not a stale row.
func TestConsoleFollowsVMNode(t *testing.T) {
	e, s := env(t)
	row, err := e.App.Ent.Server.Get(context.Background(), s.ServerID)
	require.NoError(t, err)
	e.App.PVE.MoveVM(*row.Vmid, "pve2")
	g := request(t, e, e.C, s.ServerID)
	c, _, err := dial(t, g.WSSURL, &websocket.DialOptions{Subprotocols: []string{"binary"}})
	require.NoError(t, err)
	c.CloseNow()
}

// TestConsoleServeRechecksOwnership refuses a token whose VM lost its tags before the redeem.
func TestConsoleServeRechecksOwnership(t *testing.T) {
	e, s := env(t)
	row, err := e.App.Ent.Server.Get(context.Background(), s.ServerID)
	require.NoError(t, err)
	g := request(t, e, e.C, s.ServerID)
	e.App.PVE.SetVMConfig(*row.Vmid, "tags", "nebla;nb-srv-99999")
	_, res, err := dial(t, g.WSSURL, nil)
	require404(t, res, err)
}

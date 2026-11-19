package console

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

// pipeEnv runs an upstream that echoes and closes with 4000 on "close-me", and a proxy piping to it.
func pipeEnv(t *testing.T, s *Service) string {
	t.Helper()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		for {
			typ, b, err := c.Read(context.Background())
			if err != nil {
				return
			}
			if string(b) == "close-me" {
				c.Close(4000, "bye")
				return
			}
			if c.Write(context.Background(), typ, b) != nil {
				return
			}
		}
	}))
	t.Cleanup(upstream.Close)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up, _, err := websocket.Dial(r.Context(), "ws"+strings.TrimPrefix(upstream.URL, "http"), nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		b, err := websocket.Accept(w, r, nil)
		if err != nil {
			up.CloseNow()
			return
		}
		s.pipe(b, up)
	}))
	t.Cleanup(proxy.Close)
	return "ws" + strings.TrimPrefix(proxy.URL, "http")
}

func dialPipe(t *testing.T, url string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, url, nil)
	require.NoError(t, err)
	t.Cleanup(func() { c.CloseNow() })
	return c
}

func TestPipeForwardsUpstreamClose(t *testing.T) {
	c := dialPipe(t, pipeEnv(t, New(Options{})))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, c.Write(ctx, websocket.MessageBinary, []byte("hi")))
	_, b, err := c.Read(ctx)
	require.NoError(t, err)
	require.Equal(t, "hi", string(b))
	require.NoError(t, c.Write(ctx, websocket.MessageBinary, []byte("close-me")))
	_, _, err = c.Read(ctx)
	require.Equal(t, websocket.StatusCode(4000), websocket.CloseStatus(err), "%v", err)
}

func TestPipeIdleTimeout(t *testing.T) {
	c := dialPipe(t, pipeEnv(t, New(Options{IdleTimeout: 200 * time.Millisecond})))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _, err := c.Read(ctx)
	require.Equal(t, websocket.StatusPolicyViolation, websocket.CloseStatus(err), "%v", err)
}

func TestPipeMaxSession(t *testing.T) {
	c := dialPipe(t, pipeEnv(t, New(Options{MaxSession: 300 * time.Millisecond})))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	for {
		require.NoError(t, c.Write(ctx, websocket.MessageBinary, []byte("ping")))
		if _, _, err := c.Read(ctx); err != nil {
			require.Equal(t, websocket.StatusPolicyViolation, websocket.CloseStatus(err), "%v", err)
			break
		}
		require.Less(t, time.Since(start), 3*time.Second)
		time.Sleep(20 * time.Millisecond)
	}
}

func TestPipeCapsBrowserMessages(t *testing.T) {
	c := dialPipe(t, pipeEnv(t, New(Options{})))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = c.Write(ctx, websocket.MessageBinary, make([]byte, MaxBrowserMessage+1))
	_, _, err := c.Read(ctx)
	require.Equal(t, websocket.StatusMessageTooBig, websocket.CloseStatus(err), "%v", err)
}

func TestNewBuildsWSBase(t *testing.T) {
	require.Equal(t, "wss://panel.example", New(Options{BaseURL: "https://panel.example/"}).wsBase)
	require.Equal(t, "ws://127.0.0.1:8080", New(Options{BaseURL: "http://127.0.0.1:8080"}).wsBase)
	require.Equal(t, "panel.example", New(Options{BaseURL: "https://panel.example"}).baseHost)
}

package servers_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"testing"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/pve"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/stretchr/testify/require"
)

func TestActionErrorHidesProxmoxURL(t *testing.T) {
	ctx := context.Background()
	e, cw := directEnv(t)
	s := builtServer(t, e, cw, "web")
	row, err := e.App.Ent.Server.Get(ctx, s.ServerID)
	require.NoError(t, err)
	_, err = e.App.Ent.Location.UpdateOneID(row.LocationID).SetPveURL("https://127.0.0.1:1").Save(ctx)
	require.NoError(t, err)
	id := startAction(t, e, s.ServerID, "reboot", nil)
	w := servers.NewPowerServerWorker(opts(e))
	_ = w.Work(ctx, jobOf(queuedArgs[servers.PowerServerArgs](t, e, id), servers.MaxAttempts))
	a, err := actions.Get(ctx, e.App.Ent, e.Project, id)
	require.NoError(t, err)
	require.NotNil(t, a.Error)
	require.Contains(t, a.Error.Message, "could not reach Proxmox")
	require.NotContains(t, a.Error.Message, "127.0.0.1:1")
}

func TestErrorMessageKeepsPVEMessages(t *testing.T) {
	urlErr := &url.Error{Op: "Get", URL: "https://10.0.0.1:8006/x", Err: errors.New("refused")}
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"pve error", &pve.Error{Status: 500, Message: "boom"}, "boom"},
		{"task error", &pve.TaskError{ExitStatus: "task failed"}, "task failed"},
		{"permanent", servers.Permanent("no capacity"), "no capacity"},
		{"url error", urlErr, "could not reach Proxmox"},
		{"wrapped url error", fmt.Errorf("get vms: %w", urlErr), "could not reach Proxmox"},
		{"net error", &net.OpError{Op: "dial", Err: errors.New("refused")}, "could not reach Proxmox"},
		{"deadline", fmt.Errorf("get: %w", context.DeadlineExceeded), "could not reach Proxmox"},
		{"timeout url error", &url.Error{Op: "Get", URL: "https://h:1", Err: context.DeadlineExceeded}, "could not reach Proxmox"},
		{"unknown", errors.New("dial tcp 10.0.0.1:8006: boom"), "unexpected error"},
		{"pve error wrapping url error", &wrapsURL{pe: &pve.Error{Status: 502, Message: "bad gateway"}, ue: urlErr}, "bad gateway"},
		{"permanent wrapping url error", &wrapsPerm{ue: urlErr}, "no capacity"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, servers.ErrorMessage(tt.err))
			require.Equal(t, "step: "+tt.want, servers.ErrorMessage(servers.StepError("step", tt.err)))
		})
	}
}

// wrapsURL is a pve.Error that also unwraps to a transport error.
type wrapsURL struct {
	pe *pve.Error
	ue *url.Error
}

func (w *wrapsURL) Error() string   { return w.pe.Error() }
func (w *wrapsURL) Unwrap() []error { return []error{w.pe, w.ue} }

type wrapsPerm struct{ ue *url.Error }

func (w *wrapsPerm) Error() string   { return "x" }
func (w *wrapsPerm) Unwrap() []error { return []error{servers.Permanent("no capacity"), w.ue} }

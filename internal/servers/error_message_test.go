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
	require.Equal(t, actions.CodeUnreachable, a.Error.Code)
	require.NotContains(t, a.Error.Message, "127.0.0.1")
	require.Contains(t, a.Error.Detail, "127.0.0.1:1", "admins see the real cause")
}

func TestFailure(t *testing.T) {
	urlErr := &url.Error{Op: "Get", URL: "https://10.0.0.1:8006/x", Err: errors.New("refused")}
	tests := []struct {
		name string
		err  error
		code string
	}{
		{"pve error", &pve.Error{Status: 500, Message: "storage local-lvm full"}, actions.CodeFailed},
		{"task error", &pve.TaskError{ExitStatus: "task failed"}, actions.CodeFailed},
		{"permanent", servers.Permanent("no capacity"), actions.CodeFailed},
		{"missing", servers.Missing("vm %d is missing", 4242), actions.CodeServerMissing},
		{"timed out", servers.TimedOut("vm %d did not reach running", 4242), actions.CodeTimedOut},
		{"url error", urlErr, actions.CodeUnreachable},
		{"wrapped url error", fmt.Errorf("get vms: %w", urlErr), actions.CodeUnreachable},
		{"net error", &net.OpError{Op: "dial", Err: errors.New("refused")}, actions.CodeUnreachable},
		{"deadline", fmt.Errorf("get: %w", context.DeadlineExceeded), actions.CodeUnreachable},
		{"timeout url error", &url.Error{Op: "Get", URL: "https://h:1", Err: context.DeadlineExceeded}, actions.CodeUnreachable},
		{"unknown", errors.New("dial tcp 10.0.0.1:8006: boom"), actions.CodeFailed},
		{"pve error wrapping url error", &wrapsURL{pe: &pve.Error{Status: 502, Message: "bad gateway"}, ue: urlErr}, actions.CodeFailed},
		{"permanent wrapping url error", &wrapsPerm{ue: urlErr}, actions.CodeFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := servers.Classify(tt.err)
			code, detail := f.Code, f.Detail
			require.Equal(t, tt.code, code)
			require.Equal(t, tt.err.Error(), detail)
			f = servers.Classify(servers.StepError("clone", tt.err))
			code, detail = f.Code, f.Detail
			require.Equal(t, tt.code, code)
			require.Equal(t, "clone: "+tt.err.Error(), detail)
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

func TestPowerOnMissingVMIsServerMissing(t *testing.T) {
	ctx := context.Background()
	e, cw := directEnv(t)
	s := builtServer(t, e, cw, "web")
	row, err := e.App.Ent.Server.Get(ctx, s.ServerID)
	require.NoError(t, err)
	e.App.PVE.DeleteVM(*row.Vmid)
	id := startAction(t, e, s.ServerID, "reboot", nil)
	w := servers.NewPowerServerWorker(opts(e))
	_ = w.Work(ctx, jobOf(queuedArgs[servers.PowerServerArgs](t, e, id), 1))
	a, err := actions.Get(ctx, e.App.Ent, e.Project, id)
	require.NoError(t, err)
	require.Equal(t, actions.CodeServerMissing, a.Error.Code)
	require.Contains(t, a.Error.Detail, fmt.Sprint(*row.Vmid))
	require.NotContains(t, a.Error.Message, fmt.Sprint(*row.Vmid))
}

func TestDeletedServerIsServerGone(t *testing.T) {
	ctx := context.Background()
	e, cw := directEnv(t)
	s := builtServer(t, e, cw, "web")
	id := startAction(t, e, s.ServerID, "reboot", nil)
	args := queuedArgs[servers.PowerServerArgs](t, e, id)
	_, err := e.App.Ent.IPAllocation.Delete().Exec(ctx)
	require.NoError(t, err)
	require.NoError(t, e.App.Ent.Server.DeleteOneID(s.ServerID).Exec(ctx))
	_ = servers.NewPowerServerWorker(opts(e)).Work(ctx, jobOf(args, 1))
	a, err := actions.Get(ctx, e.App.Ent, e.Project, id)
	require.NoError(t, err)
	require.Equal(t, actions.CodeServerGone, a.Error.Code)
	require.Equal(t, "The server no longer exists.", a.Error.Message)
}

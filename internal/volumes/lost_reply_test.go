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

package volumes_test

import (
	"context"
	"testing"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/ent/volume"
	"github.com/nebla-cloud/nebla/internal/pve"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/nebla-cloud/nebla/internal/volumes"
	"github.com/stretchr/testify/require"
)

// lostReplyPVE applies every config write and then reports a gateway error, as if the reply was lost.
type lostReplyPVE struct{ pve.Client }

func (l *lostReplyPVE) SetConfigDigest(ctx context.Context, node string, vmid int, kv map[string]string, digest string) error {
	if err := l.Client.SetConfigDigest(ctx, node, vmid, kv, digest); err != nil {
		return err
	}
	return &pve.Error{Status: 503, Message: "reply lost"}
}

func (e *env) lostReply(o volumes.WorkerOptions) volumes.WorkerOptions {
	return volumes.SetPVE(o, func(ctx context.Context, loc int64) (pve.Client, error) {
		pc, err := e.App.Catalog.ProxmoxFor(ctx, loc)
		return &lostReplyPVE{pc}, err
	})
}

func TestAttachLostReplyStaysTracked(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.pinPVE1(t)
	o := e.opts(nil)
	s := e.RunningServer("web")
	vmid := vmidOf(s)
	v, act, err := e.svc.Create(ctx, e.Project, volumes.CreateInput{Name: "data", SizeGB: 10, Location: "eu-1"})
	e.ok(t, o, act, err)

	act, err = e.svc.Attach(ctx, e.Project, v.ID, "web")
	require.NoError(t, err)
	// The last attempt: the write lands, its reply is lost, and the action fails.
	_ = e.work(ctx, t, e.lostReply(o), act.ID, servers.MaxAttempts)
	got, err := actions.Get(ctx, e.App.Ent, e.Project, act.ID)
	require.NoError(t, err)
	require.Equal(t, actions.StatusError, got.Status)
	require.Len(t, e.keysFor(t, vmid, v.ID), 1)
	r := e.row(t, v.ID)
	require.Equal(t, volume.StatusAttached, r.Status)
	require.NotNil(t, r.ServerID)
	require.Equal(t, s.ServerID, *r.ServerID)
	require.Contains(t, e.keysFor(t, vmid, v.ID), r.Device)

	act, err = e.svc.Detach(ctx, e.Project, v.ID)
	e.ok(t, o, act, err)
	require.Empty(t, e.keysFor(t, vmid, v.ID))
	r = e.row(t, v.ID)
	require.Equal(t, volume.StatusAvailable, r.Status)
	require.Nil(t, r.ServerID)
}

func TestCreateAttachLostReplyStaysTracked(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.pinPVE1(t)
	o := e.opts(nil)
	s := e.RunningServer("web")
	vmid := vmidOf(s)
	v, act, err := e.svc.Create(ctx, e.Project, volumes.CreateInput{Name: "data", SizeGB: 10, Server: "web"})
	require.NoError(t, err)
	_ = e.work(ctx, t, e.lostReply(o), act.ID, servers.MaxAttempts)
	got, err := actions.Get(ctx, e.App.Ent, e.Project, act.ID)
	require.NoError(t, err)
	require.Equal(t, actions.StatusError, got.Status)
	r := e.row(t, v.ID)
	require.Equal(t, volume.StatusAttached, r.Status)
	require.Equal(t, s.ServerID, *r.ServerID)
	require.Contains(t, e.keysFor(t, vmid, v.ID), r.Device)

	act, err = e.svc.Detach(ctx, e.Project, v.ID)
	e.ok(t, o, act, err)
	require.Empty(t, e.keysFor(t, vmid, v.ID))
	require.Equal(t, volume.StatusAvailable, e.row(t, v.ID).Status)
}

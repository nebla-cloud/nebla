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
	"testing"

	"github.com/nebla-cloud/nebla/internal/ent/ipaddress"
	"github.com/nebla-cloud/nebla/internal/protection"
	"github.com/nebla-cloud/nebla/internal/serverlock"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/stretchr/testify/require"
)

func TestDelete_Protected(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	s := e.RunningServer("web")
	e.App.Ent.Server.UpdateOneID(s.ServerID).SetDeleteProtected(true).ExecX(ctx)
	before := e.App.Ent.Action.Query().CountX(ctx)
	_, err := busService(e, &fipBus{}).Delete(ctx, e.Project, s.ServerID)
	require.ErrorIs(t, err, protection.ErrDelete)
	require.Equal(t, servers.StatusRunning, status(t, e, s.ServerID))
	require.Equal(t, before, e.App.Ent.Action.Query().CountX(ctx))
}

func TestDelete_ProtectedBeatsBusy(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	s := e.CreateServer("web", nil)
	e.App.Ent.Server.UpdateOneID(s.ServerID).SetDeleteProtected(true).ExecX(ctx)
	_, err := busService(e, &fipBus{}).Delete(ctx, e.Project, s.ServerID)
	require.ErrorIs(t, err, protection.ErrDelete)
}

func TestRebuild_Protected(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	s := e.RunningServer("web")
	e.App.Ent.Server.UpdateOneID(s.ServerID).SetRebuildProtected(true).ExecX(ctx)
	_, err := busService(e, &fipBus{}).Rebuild(ctx, e.Project, s.ServerID, servers.RebuildSource{Image: "ubuntu"})
	require.ErrorIs(t, err, protection.ErrRebuild)
	require.Equal(t, servers.StatusRunning, status(t, e, s.ServerID))
}

func TestRebuild_ProtectedSnapshotSource(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	s := e.RunningServer("web")
	e.App.Ent.Server.UpdateOneID(s.ServerID).SetRebuildProtected(true).ExecX(ctx)
	_, err := busService(e, &fipBus{}).Rebuild(ctx, e.Project, s.ServerID, servers.RebuildSource{Snapshot: "1"})
	require.ErrorIs(t, err, protection.ErrRebuild)
}

func TestUpdate_ProtectionOnlyWhileBusy(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	s := e.CreateServer("web", nil)
	got, err := busService(e, &fipBus{}).Update(ctx, e.Project, s.ServerID, servers.UpdateInput{DeleteProtected: new(true)})
	require.NoError(t, err)
	require.True(t, got.DeleteProtected)
	require.True(t, e.App.Ent.Server.GetX(ctx, s.ServerID).DeleteProtected)
}

func TestUpdate_MixedWhileBusyRefused(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	s := e.CreateServer("web", nil)
	_, err := busService(e, &fipBus{}).Update(ctx, e.Project, s.ServerID,
		servers.UpdateInput{Name: new("renamed"), DeleteProtected: new(true)})
	require.ErrorIs(t, err, serverlock.ErrLocked)
	row := e.App.Ent.Server.GetX(ctx, s.ServerID)
	require.Equal(t, "web", row.Name)
	require.False(t, row.DeleteProtected)
}

func TestUpdate_PartialKeepsOther(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	s := e.RunningServer("web")
	e.App.Ent.Server.UpdateOneID(s.ServerID).SetDeleteProtected(true).SetRebuildProtected(true).ExecX(ctx)
	got, err := busService(e, &fipBus{}).Update(ctx, e.Project, s.ServerID, servers.UpdateInput{RebuildProtected: new(false)})
	require.NoError(t, err)
	require.True(t, got.DeleteProtected)
	require.False(t, got.RebuildProtected)
}

func TestCreate_WithProtection(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	in := createIn("web", servers.IPChoice{}, servers.IPChoice{})
	in.DeleteProtected, in.RebuildProtected = true, true
	srv, _, err := busService(e, &fipBus{}).Create(ctx, e.Project, in)
	require.NoError(t, err)
	require.True(t, srv.DeleteProtected)
	require.True(t, srv.RebuildProtected)
	row := e.App.Ent.Server.GetX(ctx, srv.ID)
	require.True(t, row.DeleteProtected)
	require.True(t, row.RebuildProtected)
}

func TestReleasePrimaries_ProtectedKept(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	bus := &fipBus{}
	svc := busService(e, bus)
	srv, act, err := svc.Create(ctx, e.Project, createIn("web", servers.IPChoice{New: true}, servers.IPChoice{New: true}))
	require.NoError(t, err)
	o := opts(e)
	o.Bus = bus
	require.NoError(t, servers.NewCreateServerWorker(o).Work(ctx, jobOf(queuedArgs[servers.CreateServerArgs](t, e, act.ID), 1)))
	v4 := e.App.Ent.IPAddress.Query().Where(ipaddress.ServerID(srv.ID), ipaddress.Family("ipv4")).OnlyX(ctx)
	v6 := e.App.Ent.IPAddress.Query().Where(ipaddress.ServerID(srv.ID), ipaddress.Family("ipv6")).OnlyX(ctx)
	require.True(t, v4.AutoDelete)
	require.True(t, v6.AutoDelete)
	e.App.Ent.IPAddress.UpdateOneID(v4.ID).SetDeleteProtected(true).ExecX(ctx)

	delAct, err := svc.Delete(ctx, e.Project, srv.ID)
	require.NoError(t, err)
	require.NoError(t, servers.NewDeleteServerWorker(o).Work(ctx, jobOf(queuedArgs[servers.DeleteServerArgs](t, e, delAct.ID), 1)))

	kept := e.App.Ent.IPAddress.GetX(ctx, v4.ID)
	require.Nil(t, kept.ServerID)
	require.True(t, kept.AutoDelete)
	require.Equal(t, 1, e.App.Ent.IPAllocation.Query().CountX(ctx), "the protected allocation is still held")
	_, err = e.App.Ent.IPAddress.Get(ctx, v6.ID)
	require.Error(t, err)
}

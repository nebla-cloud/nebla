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

package snapshots_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/apierr"
	"github.com/nebla-cloud/nebla/internal/ent/action"
	"github.com/nebla-cloud/nebla/internal/ent/server"
	"github.com/nebla-cloud/nebla/internal/ent/snapshot"
	"github.com/nebla-cloud/nebla/internal/projects"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/nebla-cloud/nebla/internal/snapshots"
	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/stretchr/testify/require"
)

type env struct {
	*testutil.ServerEnv
	svc *snapshots.Service
}

var fixedNow = time.Date(2026, 11, 23, 14, 5, 40, 0, time.UTC)

func newEnv(t *testing.T) *env {
	t.Helper()
	e := testutil.NewServerEnv(testutil.NewApp(t, testutil.QueueOnlyJobs(t)))
	a := e.App
	proj := projects.New(a.Ent, projects.Options{Hooks: projects.Hooks{Usage: snapshots.UsageWith(servers.Usage)}})
	svc := snapshots.New(snapshots.Options{SQL: a.SQL, Ent: a.Ent, Catalog: a.Catalog, Projects: proj, Jobs: a.Jobs,
		Now: func() time.Time { return fixedNow }})
	return &env{ServerEnv: e, svc: svc}
}

func requireAPIErr(t *testing.T, err error, status int, code string) apierr.E {
	t.Helper()
	var ae apierr.E
	require.True(t, errors.As(err, &ae), "%v", err)
	require.Equal(t, status, ae.Status, ae.Msg)
	require.Equal(t, code, ae.Code, ae.Msg)
	return ae
}

func (e *env) setQuota(t *testing.T, maxSnaps, maxGB int) {
	t.Helper()
	require.NoError(t, e.App.Ent.Project.UpdateOneID(e.Project).SetMaxSnapshots(maxSnaps).SetMaxSnapshotGB(maxGB).Exec(context.Background()))
}

// settle ends every running action, as if the jobs finished.
func (e *env) settle(t *testing.T) {
	t.Helper()
	e.App.Ent.Action.Update().SetStatus(action.StatusSuccess).ExecX(context.Background())
}

// requireJob asserts the action's river job has kind and args.
func (e *env) requireJob(t *testing.T, act actions.Action, kind string, snapshotID int64) {
	t.Helper()
	require.NotNil(t, act.RiverJobID)
	var gotKind string
	var args []byte
	require.NoError(t, e.App.SQL.QueryRow(`SELECT kind, args FROM river_job WHERE id = $1`, *act.RiverJobID).Scan(&gotKind, &args))
	require.Equal(t, kind, gotKind)
	require.JSONEq(t, fmt.Sprintf(`{"snapshot_id":%d,"action_id":%d}`, snapshotID, act.ID), string(args))
}

func TestCreateSnapshotReservesRealSize(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	s := e.RunningServer("web1")
	e.App.PVE.SetVMConfig(100+int(s.ServerID), "scsi0", "ceph:vm-1-disk-0,size=64G")

	snap, act, err := e.svc.Create(ctx, e.Project, snapshots.CreateInput{Server: "web1", Name: "before", ForceStop: true,
		Description: "d", Labels: map[string]string{"env": "prod"}})
	require.NoError(t, err)
	require.Equal(t, 64, snap.SizeGB)
	require.Equal(t, "creating", snap.Status)
	require.Equal(t, "web1", snap.ServerName)
	require.Equal(t, map[string]string{"env": "prod"}, snap.Labels)
	require.Equal(t, "create_snapshot", act.Command)
	e.requireJob(t, act, "create_snapshot", snap.ID)
	require.Equal(t, []actions.Resource{{ID: s.ServerID, Type: servers.ResourceType}, {ID: snap.ID, Type: snapshots.ResourceType}}, act.Resources)

	u, err := snapshots.UsageWith(servers.Usage)(ctx, e.App.Ent, e.Project)
	require.NoError(t, err)
	require.Equal(t, 1, u.Snapshots)
	require.Equal(t, 64, u.SnapshotGB)

	row := e.App.Ent.Snapshot.GetX(ctx, snap.ID)
	require.True(t, row.ForceStop)
	require.Equal(t, e.Location, row.LocationID)
	require.Equal(t, s.ServerID, *row.ServerID)
	require.Equal(t, 0, row.Step)
	require.NotEmpty(t, row.NetworkFingerprint)
	srv := e.App.Ent.Server.GetX(ctx, s.ServerID)
	require.Equal(t, servers.InstanceGeneration(srv), row.InstanceGeneration)
	require.Equal(t, srv.ImageID, row.ImageID)
}

func TestCreateSnapshotRoundsUp(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	s := e.RunningServer("web1")
	e.App.PVE.SetVMConfig(100+int(s.ServerID), "scsi0", "ceph:vm-1-disk-0,size=10.5G")
	snap, _, err := e.svc.Create(ctx, e.Project, snapshots.CreateInput{Server: "web1", Name: "x"})
	require.NoError(t, err)
	require.Equal(t, 11, snap.SizeGB)
}

func TestCreateSnapshotQuotaBeforeTouch(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	s := e.RunningServer("web1")
	e.App.PVE.SetVMConfig(100+int(s.ServerID), "scsi0", "ceph:vm-1-disk-0,size=64G")
	e.setQuota(t, 0, 50)

	_, _, err := e.svc.Create(ctx, e.Project, snapshots.CreateInput{Server: "web1", Name: "big"})
	requireAPIErr(t, err, http.StatusUnprocessableEntity, "quota_exceeded")
	for _, c := range e.App.PVE.Calls() {
		require.False(t, strings.Contains(c, "/status/"), "power call %s", c)
	}
	n, err := e.App.Ent.Snapshot.Query().Count(ctx)
	require.NoError(t, err)
	require.Zero(t, n)
}

func TestCreateSnapshotDefaultName(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.RunningServer("web1")
	snap, _, err := e.svc.Create(ctx, e.Project, snapshots.CreateInput{Server: "web1"})
	require.NoError(t, err)
	require.Equal(t, "web1-20261123-1405", snap.Name)
	e.settle(t)

	_, _, err = e.svc.Create(ctx, e.Project, snapshots.CreateInput{Server: "web1"})
	requireAPIErr(t, err, http.StatusConflict, "uniqueness_error")

	_, _, err = e.svc.Create(ctx, e.Project, snapshots.CreateInput{Server: "web1", Name: "Bad Name"})
	requireAPIErr(t, err, http.StatusUnprocessableEntity, "invalid_input")
}

func TestCreateSnapshotServerBusy(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	s := e.RunningServer("web1")
	_, err := actions.Create(ctx, e.App.Ent, e.Project, "stop_server", []actions.Resource{{ID: s.ServerID, Type: servers.ResourceType}})
	require.NoError(t, err)

	_, _, err = e.svc.Create(ctx, e.Project, snapshots.CreateInput{Server: "web1", Name: "x"})
	requireAPIErr(t, err, http.StatusConflict, "locked")
}

func TestCreateSnapshotWrongStatus(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	s := e.RunningServer("web1")
	require.NoError(t, e.App.Ent.Server.UpdateOneID(s.ServerID).SetStatus(server.StatusRebuilding).Exec(ctx))

	_, _, err := e.svc.Create(ctx, e.Project, snapshots.CreateInput{Server: "web1", Name: "x"})
	requireAPIErr(t, err, http.StatusConflict, "server_not_ready")

	_, _, err = e.svc.Create(ctx, e.Project, snapshots.CreateInput{Server: "nope", Name: "x"})
	requireAPIErr(t, err, http.StatusNotFound, "not_found")
}

func TestCreateSnapshotUnavailable(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.RunningServer("web1")
	for _, n := range []string{"pve1", "pve2"} {
		e.App.PVE.AddStorage(n, "ceph", []string{"iso"}, true)
	}
	_, _, err := e.svc.Create(ctx, e.Project, snapshots.CreateInput{Server: "web1", Name: "x"})
	requireAPIErr(t, err, http.StatusUnprocessableEntity, "snapshots_unavailable")
}

func TestUpdateSnapshot(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.RunningServer("web1")
	snap, _, err := e.svc.Create(ctx, e.Project, snapshots.CreateInput{Server: "web1", Name: "a"})
	require.NoError(t, err)

	// A running action holds it.
	_, err = e.svc.Update(ctx, e.Project, snap.ID, snapshots.UpdateInput{Name: new("renamed")})
	requireAPIErr(t, err, http.StatusConflict, "locked")

	e.settle(t)
	e.App.Ent.Snapshot.UpdateOneID(snap.ID).SetStatus(snapshot.StatusAvailable).ExecX(ctx)
	_, _, err = e.svc.Create(ctx, e.Project, snapshots.CreateInput{Server: "web1", Name: "b"})
	require.NoError(t, err)
	e.settle(t)
	got, err := e.svc.Update(ctx, e.Project, snap.ID, snapshots.UpdateInput{Name: new("renamed"), Description: new("new"),
		Labels: &map[string]string{"k": "v"}})
	require.NoError(t, err)
	require.Equal(t, "renamed", got.Name)
	require.Equal(t, "new", got.Description)
	require.Equal(t, map[string]string{"k": "v"}, got.Labels)

	_, err = e.svc.Update(ctx, e.Project, snap.ID, snapshots.UpdateInput{Name: new("b")})
	requireAPIErr(t, err, http.StatusConflict, "uniqueness_error")
	_, err = e.svc.Update(ctx, e.Project, snap.ID, snapshots.UpdateInput{Name: new("Bad")})
	requireAPIErr(t, err, http.StatusUnprocessableEntity, "invalid_input")
	_, err = e.svc.Update(ctx, e.Project+99, snap.ID, snapshots.UpdateInput{})
	requireAPIErr(t, err, http.StatusNotFound, "not_found")
}

func TestListSnapshots(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.RunningServer("web1")
	a, _, err := e.svc.Create(ctx, e.Project, snapshots.CreateInput{Server: "web1", Name: "a", Labels: map[string]string{"env": "prod"}})
	require.NoError(t, err)
	e.settle(t)
	_, _, err = e.svc.Create(ctx, e.Project, snapshots.CreateInput{Server: "web1", Name: "b", Labels: map[string]string{"env": "dev"}})
	require.NoError(t, err)

	all, total, err := e.svc.List(ctx, e.Project, snapshots.ListFilter{}, 1, 10)
	require.NoError(t, err)
	require.Equal(t, 2, total)
	require.Len(t, all, 2)

	got, total, err := e.svc.List(ctx, e.Project, snapshots.ListFilter{LabelSelector: "env=prod"}, 1, 10)
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Equal(t, a.ID, got[0].ID)

	got, _, err = e.svc.List(ctx, e.Project, snapshots.ListFilter{Server: "web1", Status: "creating", Location: "eu-1"}, 1, 10)
	require.NoError(t, err)
	require.Len(t, got, 2)
	got, _, err = e.svc.List(ctx, e.Project, snapshots.ListFilter{Server: "ghost"}, 1, 10)
	require.NoError(t, err)
	require.Empty(t, got)
	_, _, err = e.svc.List(ctx, e.Project, snapshots.ListFilter{Status: "bogus"}, 1, 10)
	requireAPIErr(t, err, http.StatusUnprocessableEntity, "invalid_input")
}

func TestDeleteSnapshotLocked(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.RunningServer("web1")
	snap, _, err := e.svc.Create(ctx, e.Project, snapshots.CreateInput{Server: "web1", Name: "a"})
	require.NoError(t, err)

	// Still creating, with a running action.
	_, err = e.svc.Delete(ctx, e.Project, snap.ID)
	requireAPIErr(t, err, http.StatusConflict, "locked")

	e.settle(t)
	e.App.Ent.Snapshot.UpdateOneID(snap.ID).SetStatus(snapshot.StatusAvailable).ExecX(ctx)
	act, err := e.svc.Delete(ctx, e.Project, snap.ID)
	require.NoError(t, err)
	require.Equal(t, "delete_snapshot", act.Command)
	e.requireJob(t, act, "delete_snapshot", snap.ID)
	require.Equal(t, []actions.Resource{{ID: snap.ID, Type: snapshots.ResourceType}}, act.Resources)
	require.Equal(t, snapshot.StatusDeleting, e.App.Ent.Snapshot.GetX(ctx, snap.ID).Status)

	// Deleting again is refused.
	_, err = e.svc.Delete(ctx, e.Project, snap.ID)
	requireAPIErr(t, err, http.StatusConflict, "locked")
}

func TestDeleteSnapshotAllowedStatuses(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	for i, st := range []snapshot.Status{snapshot.StatusMissing, snapshot.StatusError} {
		row := e.App.Ent.Snapshot.Create().SetProjectID(e.Project).SetLocationID(e.Location).SetName("s" + string(rune('a'+i))).
			SetLabels(map[string]string{}).SetSizeGB(10).SetStatus(st).SaveX(ctx)
		_, err := e.svc.Delete(ctx, e.Project, row.ID)
		require.NoError(t, err, st)
	}
}

func TestRestoreKeepsIdentityOnRead(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.RunningServer("web1")
	snap, _, err := e.svc.Create(ctx, e.Project, snapshots.CreateInput{Server: "web1", Name: "a"})
	require.NoError(t, err)
	require.NotNil(t, snap.RestoreKeepsIdentity)
	require.True(t, *snap.RestoreKeepsIdentity)

	// A network change flips it.
	e.App.Ent.Snapshot.UpdateOneID(snap.ID).SetNetworkFingerprint("other").ExecX(ctx)
	got, err := e.svc.Get(ctx, e.Project, snap.ID)
	require.NoError(t, err)
	require.False(t, *got.RestoreKeepsIdentity)

	// A deleted server leaves it unknown.
	e.App.Ent.Snapshot.UpdateOneID(snap.ID).ClearServerID().ExecX(ctx)
	got, err = e.svc.Get(ctx, e.Project, snap.ID)
	require.NoError(t, err)
	require.Nil(t, got.RestoreKeepsIdentity)
	require.Equal(t, "web1", got.ServerName)
}

func TestAvailable(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	ok, err := e.svc.Available(ctx, e.Location)
	require.NoError(t, err)
	require.True(t, ok)

	// Same as server disks: ceph has no images content.
	for _, n := range []string{"pve1", "pve2"} {
		e.App.PVE.AddStorage(n, "ceph", []string{"iso"}, true)
	}
	ok, err = e.svc.Available(ctx, e.Location)
	require.NoError(t, err)
	require.False(t, ok)

	// An explicit snapshot storage with images content makes it available again.
	e.App.Ent.Location.UpdateOneID(e.Location).SetSnapshotStorage("local-lvm").ExecX(ctx)
	ok, err = e.svc.Available(ctx, e.Location)
	require.NoError(t, err)
	require.True(t, ok)
}

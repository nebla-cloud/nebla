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
	"net/http"
	"strconv"
	"testing"

	"github.com/nebla-cloud/nebla/internal/apierr"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/ipallocation"
	"github.com/nebla-cloud/nebla/internal/ent/node"
	"github.com/nebla-cloud/nebla/internal/ent/server"
	"github.com/nebla-cloud/nebla/internal/ent/snapshot"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/stretchr/testify/require"
)

const snapVMID = 9100

func requireAPI(t *testing.T, err error, status int, code string) apierr.E {
	t.Helper()
	var ae apierr.E
	require.True(t, errors.As(err, &ae), "%v", err)
	require.Equal(t, status, ae.Status, ae.Msg)
	require.Equal(t, code, ae.Code, ae.Msg)
	return ae
}

// addSnapshot inserts an available snapshot whose template VM exists on node ("" is shared storage on pve1).
func addSnapshot(t *testing.T, e *testutil.ServerEnv, name, nodeName string, sizeGB int, mod ...func(*ent.SnapshotCreate)) *ent.Snapshot {
	t.Helper()
	pve := nodeName
	if pve == "" {
		pve = "pve1"
	}
	e.App.PVE.AddTemplate(pve, snapVMID, "ceph")
	c := e.App.Ent.Snapshot.Create().SetProjectID(e.Project).SetLocationID(e.Location).SetName(name).SetLabels(map[string]string{}).
		SetSizeGB(sizeGB).SetVmid(snapVMID).SetNode(nodeName).SetStatus(snapshot.StatusAvailable).SetImageID(e.Image).
		SetOsFlavor("ubuntu").SetOsVersion("24.04")
	for _, m := range mod {
		m(c)
	}
	return c.SaveX(context.Background())
}

func snapIn(snap string) servers.CreateInput {
	return servers.CreateInput{Name: "from-snap", ServerType: "small", Snapshot: snap, IPv4: servers.IPChoice{New: true}, StartAfter: true}
}

func TestCreateFromSnapshot(t *testing.T) {
	ctx := context.Background()
	e, cw := directEnv(t)
	snap := addSnapshot(t, e, "gold", "pve2", 10)
	srv, _, err := serverService(e).Create(ctx, e.Project, snapIn("gold"))
	require.NoError(t, err)
	row := e.App.Ent.Server.GetX(ctx, srv.ID)
	require.Equal(t, snap.ID, *row.SnapshotID)
	require.Equal(t, e.Image, *row.ImageID)
	require.Equal(t, "ubuntu", row.OsFlavor)
	require.Equal(t, "24.04", row.OsVersion)
	require.Equal(t, snapVMID, row.TemplateVmid)
	require.Equal(t, "pve2", row.Node)

	a, err := e.App.Ent.Action.Query().Only(ctx)
	require.NoError(t, err)
	require.NoError(t, cw.Work(ctx, job(testutil.CreatedServer{ServerID: srv.ID, ActionID: a.ID}, 1)))
	require.Equal(t, servers.StatusRunning, status(t, e, srv.ID))
	_, ok := e.App.PVE.VM(vmidOf(t, e, srv.ID))
	require.True(t, ok)

	// By id, and with the location named.
	in := snapIn(strconv.FormatInt(snap.ID, 10))
	in.Name, in.Location = "again", "eu-1"
	_, _, err = serverService(e).Create(ctx, e.Project, in)
	require.NoError(t, err)
}

func TestCreateFromSnapshotKeepsNullImage(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	addSnapshot(t, e, "gold", "pve1", 10, func(c *ent.SnapshotCreate) { c.Mutation().ResetImageID() })
	srv, _, err := serverService(e).Create(ctx, e.Project, snapIn("gold"))
	require.NoError(t, err)
	row := e.App.Ent.Server.GetX(ctx, srv.ID)
	require.Nil(t, row.ImageID)
	require.Equal(t, "24.04", row.OsVersion)
}

func TestCreateFromSnapshotSharedStorage(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	addSnapshot(t, e, "gold", "", 10)
	svc := serverService(e)
	// pve1 holds a reserved server, so pve2 has the most free memory.
	_, _, err := svc.Create(ctx, e.Project, servers.CreateInput{Name: "first", ServerType: "small", Image: "ubuntu", Location: "eu-1", IPv4: servers.IPChoice{New: true}})
	require.NoError(t, err)
	srv, _, err := svc.Create(ctx, e.Project, snapIn("gold"))
	require.NoError(t, err)
	require.Equal(t, "pve2", e.App.Ent.Server.GetX(ctx, srv.ID).Node)
}

func TestCreateFromSnapshotTooLarge(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	addSnapshot(t, e, "big", "pve1", 30)
	_, _, err := serverService(e).Create(ctx, e.Project, snapIn("big"))
	ae := requireAPI(t, err, http.StatusUnprocessableEntity, "snapshot_too_large")
	require.Equal(t, "This snapshot needs a server type with at least 30 GB of disk", ae.Msg)
	require.Zero(t, e.App.Ent.Server.Query().CountX(ctx))
}

func TestCreateImageAndSnapshotExclusive(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	addSnapshot(t, e, "gold", "pve1", 10)
	in := snapIn("gold")
	in.Image, in.Location = "ubuntu", "eu-1"
	_, _, err := serverService(e).Create(ctx, e.Project, in)
	requireAPI(t, err, http.StatusUnprocessableEntity, "invalid_input")
	in.Image, in.Snapshot = "", ""
	_, _, err = serverService(e).Create(ctx, e.Project, in)
	requireAPI(t, err, http.StatusUnprocessableEntity, "invalid_input")
	require.Zero(t, e.App.Ent.Server.Query().CountX(ctx))
}

func TestCreateFromSnapshotRefusals(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	svc := serverService(e)
	snap := addSnapshot(t, e, "gold", "pve1", 10)

	_, _, err := svc.Create(ctx, e.Project, snapIn("ghost"))
	requireAPI(t, err, http.StatusNotFound, "not_found")

	// Another project's snapshot is not found.
	other := e.App.Ent.Project.Create().SetName("other").SaveX(ctx)
	theirs := e.App.Ent.Snapshot.Create().SetProjectID(other.ID).SetLocationID(e.Location).SetName("theirs").SetLabels(map[string]string{}).
		SetSizeGB(10).SetVmid(snapVMID + 1).SetNode("pve1").SetStatus(snapshot.StatusAvailable).SaveX(ctx)
	_, _, err = svc.Create(ctx, e.Project, snapIn("theirs"))
	requireAPI(t, err, http.StatusNotFound, "not_found")
	_, _, err = svc.Create(ctx, e.Project, snapIn(strconv.FormatInt(theirs.ID, 10)))
	requireAPI(t, err, http.StatusNotFound, "not_found")

	e.App.Ent.Snapshot.UpdateOneID(snap.ID).SetStatus(snapshot.StatusCreating).ExecX(ctx)
	_, _, err = svc.Create(ctx, e.Project, snapIn("gold"))
	ae := requireAPI(t, err, http.StatusConflict, "snapshot_not_ready")
	require.Equal(t, "This snapshot isn't ready", ae.Msg)
	e.App.Ent.Snapshot.UpdateOneID(snap.ID).SetStatus(snapshot.StatusAvailable).ExecX(ctx)

	// A different location is refused.
	e.Admin.CreateLocation("us-1")
	in := snapIn("gold")
	in.Location = "us-1"
	_, _, err = svc.Create(ctx, e.Project, in)
	ae = requireAPI(t, err, http.StatusUnprocessableEntity, "invalid_input")
	require.Equal(t, "This snapshot is in location eu-1", ae.Msg)
	require.Zero(t, e.App.Ent.Server.Query().CountX(ctx))
}

func TestCreateFromSnapshotHostFull(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	addSnapshot(t, e, "gold", "pve1", 10)
	e.App.Ent.Node.Update().Where(node.Name("pve1")).SetMem(e.App.Ent.Node.Query().Where(node.Name("pve1")).OnlyX(ctx).Maxmem).ExecX(ctx)
	_, _, err := serverService(e).Create(ctx, e.Project, snapIn("gold"))
	ae := requireAPI(t, err, http.StatusUnprocessableEntity, "resource_unavailable")
	require.Equal(t, "The host that holds this snapshot is full", ae.Msg)
}

// rebuildFrom queues a rebuild of s from src and returns its job args.
func rebuildFrom(t *testing.T, e *testutil.ServerEnv, s testutil.CreatedServer, src servers.RebuildSource) (servers.RebuildServerArgs, error) {
	t.Helper()
	a, err := serverService(e).Rebuild(context.Background(), e.Project, s.ServerID, src)
	if err != nil {
		return servers.RebuildServerArgs{}, err
	}
	return queuedArgs[servers.RebuildServerArgs](t, e, a.ID), nil
}

func fingerprintOf(t *testing.T, e *testutil.ServerEnv, id int64) string {
	t.Helper()
	fp, err := servers.NetworkFingerprint(context.Background(), e.App.Ent, e.App.Ent.Server.GetX(context.Background(), id))
	require.NoError(t, err)
	return fp
}

func finishRebuild(t *testing.T, e *testutil.ServerEnv, w *servers.RebuildServerWorker, args servers.RebuildServerArgs) *ent.Server {
	t.Helper()
	require.NoError(t, w.Work(context.Background(), jobOf(args, 1)))
	return e.App.Ent.Server.GetX(context.Background(), args.ServerID)
}

func TestRebuildFromSnapshotOtherServer(t *testing.T) {
	ctx := context.Background()
	e, s, w := rebuildEnv(t)
	snap := addSnapshot(t, e, "gold", "pve1", 10)
	args, err := rebuildFrom(t, e, s, servers.RebuildSource{Snapshot: "gold"})
	require.NoError(t, err)
	require.Zero(t, args.InstanceGeneration)
	require.Equal(t, snapVMID, args.TemplateVMID)
	require.Equal(t, snap.ID, *e.App.Ent.Server.GetX(ctx, s.ServerID).RebuildSnapshotID, "set while queued")

	row := finishRebuild(t, e, w, args)
	require.Equal(t, snap.ID, *row.SnapshotID)
	require.Nil(t, row.RebuildSnapshotID)
	require.Equal(t, snapVMID, row.TemplateVmid)
	require.Equal(t, 2, row.InstanceGeneration)
	require.Contains(t, isoFiles(t, e, *row.IsoVolid)["meta-data"], "instance-id: nb-srv-"+sid(s.ServerID)+"-2\n")
}

func TestRestoreKeepsIdentity(t *testing.T) {
	e, s, w := rebuildEnv(t)
	addSnapshot(t, e, "gold", "pve1", 10, func(c *ent.SnapshotCreate) {
		c.SetServerID(s.ServerID).SetInstanceGeneration(1).SetNetworkFingerprint(fingerprintOf(t, e, s.ServerID))
	})
	args, err := rebuildFrom(t, e, s, servers.RebuildSource{Snapshot: "gold"})
	require.NoError(t, err)
	require.Equal(t, 1, args.InstanceGeneration)
	row := finishRebuild(t, e, w, args)
	require.Equal(t, 1, row.InstanceGeneration)
	require.Equal(t, 2, row.IsoGeneration)
	require.Contains(t, isoFiles(t, e, *row.IsoVolid)["meta-data"], "instance-id: nb-srv-"+sid(s.ServerID)+"-1\n")
}

// assignFloating gives the server a floating IP.
func assignFloating(t *testing.T, e *testutil.ServerEnv, serverID int64, ip string) {
	t.Helper()
	ctx := context.Background()
	a := e.App.Ent.IPAllocation.Create().SetPoolID(e.Pool4).SetIP(ip).SetOwner(ipallocation.OwnerFloating).SaveX(ctx)
	e.App.Ent.IPAddress.Create().SetProjectID(e.Project).SetLocationID(e.Location).SetAllocationID(a.ID).SetFamily("ipv4").SetIP(ip).
		SetName("fip").SetLabels(map[string]string{}).SetServerID(serverID).SaveX(ctx)
}

func TestRestoreNetworkChangedNewIdentity(t *testing.T) {
	e, s, w := rebuildEnv(t)
	addSnapshot(t, e, "gold", "pve1", 10, func(c *ent.SnapshotCreate) {
		c.SetServerID(s.ServerID).SetInstanceGeneration(1).SetNetworkFingerprint(fingerprintOf(t, e, s.ServerID))
	})
	assignFloating(t, e, s.ServerID, "10.0.0.15")
	args, err := rebuildFrom(t, e, s, servers.RebuildSource{Snapshot: "gold"})
	require.NoError(t, err)
	require.Zero(t, args.InstanceGeneration)
	row := finishRebuild(t, e, w, args)
	files := isoFiles(t, e, *row.IsoVolid)
	require.Contains(t, files["meta-data"], "instance-id: nb-srv-"+sid(s.ServerID)+"-2\n")
	require.Contains(t, files["network-config"], "10.0.0.15")
}

func TestRestoreNetworkChangedBeforeRun(t *testing.T) {
	e, s, w := rebuildEnv(t)
	addSnapshot(t, e, "gold", "pve1", 10, func(c *ent.SnapshotCreate) {
		c.SetServerID(s.ServerID).SetInstanceGeneration(1).SetNetworkFingerprint(fingerprintOf(t, e, s.ServerID))
	})
	args, err := rebuildFrom(t, e, s, servers.RebuildSource{Snapshot: "gold"})
	require.NoError(t, err)
	require.Equal(t, 1, args.InstanceGeneration)
	assignFloating(t, e, s.ServerID, "10.0.0.15")
	row := finishRebuild(t, e, w, args)
	require.Equal(t, 2, row.InstanceGeneration, "the job falls back to a new identity")
	files := isoFiles(t, e, *row.IsoVolid)
	require.Contains(t, files["meta-data"], "instance-id: nb-srv-"+sid(s.ServerID)+"-2\n")
	require.Contains(t, files["network-config"], "10.0.0.15")
}

func TestRebuildFromSnapshotNodeMismatch(t *testing.T) {
	ctx := context.Background()
	e, s, _ := rebuildEnv(t)
	addSnapshot(t, e, "gold", "pve2", 10)
	_, err := rebuildFrom(t, e, s, servers.RebuildSource{Snapshot: "gold"})
	ae := requireAPI(t, err, http.StatusUnprocessableEntity, "snapshot_server_mismatch")
	require.Equal(t, "This snapshot is on a different host in this location", ae.Msg)
	row := e.App.Ent.Server.GetX(ctx, s.ServerID)
	require.Equal(t, servers.StatusRunning, string(row.Status))
	require.Nil(t, row.RebuildSnapshotID)
}

func TestRebuildFromSnapshotSharedAnyNode(t *testing.T) {
	e, s, _ := rebuildEnv(t)
	addSnapshot(t, e, "gold", "", 10)
	_, err := rebuildFrom(t, e, s, servers.RebuildSource{Snapshot: "gold"})
	require.NoError(t, err)
}

func TestRebuildFromSnapshotRefusals(t *testing.T) {
	e, s, _ := rebuildEnv(t)
	snap := addSnapshot(t, e, "big", "pve1", 30)
	_, err := rebuildFrom(t, e, s, servers.RebuildSource{Snapshot: "big"})
	requireAPI(t, err, http.StatusUnprocessableEntity, "snapshot_too_large")
	_, err = rebuildFrom(t, e, s, servers.RebuildSource{Snapshot: "big", Image: "ubuntu"})
	requireAPI(t, err, http.StatusUnprocessableEntity, "invalid_input")
	_, err = rebuildFrom(t, e, s, servers.RebuildSource{})
	requireAPI(t, err, http.StatusUnprocessableEntity, "invalid_input")
	_, err = rebuildFrom(t, e, s, servers.RebuildSource{Snapshot: "ghost"})
	requireAPI(t, err, http.StatusNotFound, "not_found")

	e.App.Ent.Snapshot.UpdateOneID(snap.ID).SetSizeGB(10).SetStatus(snapshot.StatusDeleting).ExecX(context.Background())
	_, err = rebuildFrom(t, e, s, servers.RebuildSource{Snapshot: "big"})
	requireAPI(t, err, http.StatusConflict, "snapshot_not_ready")
}

func TestRebuildFromSnapshotFailureClearsMarker(t *testing.T) {
	ctx := context.Background()
	e, s, w := rebuildEnv(t)
	addSnapshot(t, e, "gold", "pve1", 10)
	args, err := rebuildFrom(t, e, s, servers.RebuildSource{Snapshot: "gold"})
	require.NoError(t, err)
	e.App.PVE.FailNext("/resize", http.StatusInternalServerError)
	require.Error(t, w.Work(ctx, jobOf(args, 1)))
	row := e.App.Ent.Server.GetX(ctx, s.ServerID)
	require.Nil(t, row.RebuildSnapshotID)
	require.Nil(t, row.SnapshotID)
	require.Equal(t, servers.StatusRunning, string(row.Status))
}

func TestRebuildFromImageClearsSnapshot(t *testing.T) {
	e, s, w := rebuildEnv(t)
	addSnapshot(t, e, "gold", "pve1", 10)
	args, err := rebuildFrom(t, e, s, servers.RebuildSource{Snapshot: "gold"})
	require.NoError(t, err)
	require.NotNil(t, finishRebuild(t, e, w, args).SnapshotID)

	args, err = rebuildFrom(t, e, s, servers.RebuildSource{Image: "debian"})
	require.NoError(t, err)
	row := finishRebuild(t, e, w, args)
	require.Nil(t, row.SnapshotID)
	require.Nil(t, row.RebuildSnapshotID)
	require.Equal(t, server.StatusRunning, row.Status)
}

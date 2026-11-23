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

package reconcile_test

import (
	"context"
	"testing"
	"time"

	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/backup"
	"github.com/nebla-cloud/nebla/internal/ent/backuptombstone"
	"github.com/nebla-cloud/nebla/internal/pve"
	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/stretchr/testify/require"
)

// backupEnv has PBS storage "pbs" on both nodes and as the location's backup storage.
func backupEnv(t *testing.T) (*testutil.ServerEnv, pve.Client) {
	t.Helper()
	ctx := context.Background()
	e := workerEnv(t)
	for _, n := range []string{"pve1", "pve2"} {
		e.App.PVE.AddStorage(n, "pbs", []string{"backup"}, true)
	}
	require.NoError(t, e.App.Ent.Location.UpdateOneID(e.Location).SetBackupStorage("pbs").SetBackupStorageOk(true).Exec(ctx))
	pc, err := e.App.Catalog.ProxmoxFor(ctx, e.Location)
	require.NoError(t, err)
	return e, pc
}

// archive vzdumps vmid into storage with notes and returns its volid.
func archive(t *testing.T, pc pve.Client, vmid int, storage, notes string) string {
	t.Helper()
	ctx := context.Background()
	up, err := pc.Vzdump(ctx, "pve1", vmid, storage, notes)
	require.NoError(t, err)
	require.NoError(t, pc.WaitTask(ctx, "pve1", up))
	items, err := pc.BackupContent(ctx, "pve1", storage, vmid)
	require.NoError(t, err)
	for _, it := range items {
		if it.Notes == notes {
			return it.Volid
		}
	}
	t.Fatalf("no archive with notes %q", notes)
	return ""
}

func backupRow(t *testing.T, e *testutil.ServerEnv, srv *ent.Server, storage, volid string) *ent.Backup {
	t.Helper()
	return e.App.Ent.Backup.Create().SetProjectID(e.Project).SetServerID(srv.ID).SetLocationID(e.Location).SetStorage(storage).
		SetNode(srv.Node).SetKind(backup.KindManual).SetStatus(backup.StatusAvailable).SetVolid(volid).SetInstanceGeneration(1).
		SetCompletedAt(time.Now().Add(-time.Hour)).SaveX(context.Background())
}

func backupStatus(e *testutil.ServerEnv, id int64) backup.Status {
	return e.App.Ent.Backup.GetX(context.Background(), id).Status
}

func TestReconcileMarksMissing(t *testing.T) {
	ctx := context.Background()
	e, pc := backupEnv(t)
	r := newReconciler(e, quiet())
	srv, vmid := running(t, e, "web")
	gone := backupRow(t, e, srv, "pbs", archive(t, pc, vmid, "pbs", "nebla server 1 backup 1"))
	kept := backupRow(t, e, srv, "pbs", archive(t, pc, vmid, "pbs", "nebla server 1 backup 2"))
	fresh := backupRow(t, e, srv, "pbs", "pbs:backup/vm/1/fresh")
	require.NoError(t, e.App.Ent.Backup.UpdateOneID(fresh.ID).SetCompletedAt(time.Now().Add(time.Hour)).Exec(ctx))

	require.NoError(t, r.Run(ctx, e.Location))
	require.Equal(t, backup.StatusAvailable, backupStatus(e, gone.ID))
	require.NoError(t, pc.DeleteVolume(ctx, "pve1", "pbs", *gone.Volid))
	require.NoError(t, r.Run(ctx, e.Location))
	require.Equal(t, backup.StatusMissing, backupStatus(e, gone.ID))
	require.Equal(t, backup.StatusAvailable, backupStatus(e, kept.ID))
	require.Equal(t, backup.StatusAvailable, backupStatus(e, fresh.ID), "completed after the listing")
}

func TestReconcileRetriesTombstones(t *testing.T) {
	ctx := context.Background()
	e, pc := backupEnv(t)
	r := newReconciler(e, quiet())
	_, vmid := running(t, e, "web")
	tomb := func(volid string) *ent.BackupTombstone {
		return e.App.Ent.BackupTombstone.Create().SetVolid(volid).SetStorage("pbs").SetLocationID(e.Location).SaveX(ctx)
	}
	ok := archive(t, pc, vmid, "pbs", "nebla server 1 backup 1")
	stuck := archive(t, pc, vmid, "pbs", "nebla server 1 backup 2")
	okT, stuckT, goneT := tomb(ok), tomb(stuck), tomb("pbs:backup/vm/1/already-gone")
	e.App.PVE.FailNext(stuck, 500)

	require.NoError(t, r.Run(ctx, e.Location))
	_, err := e.App.Ent.BackupTombstone.Get(ctx, okT.ID)
	require.True(t, ent.IsNotFound(err), "deleted archive")
	_, err = e.App.Ent.BackupTombstone.Get(ctx, goneT.ID)
	require.True(t, ent.IsNotFound(err), "archive already gone")
	require.Equal(t, 1, e.App.Ent.BackupTombstone.GetX(ctx, stuckT.ID).Attempts)
	items, err := pc.BackupContent(ctx, "pve1", "pbs", 0)
	require.NoError(t, err)
	require.Len(t, items, 1)

	require.NoError(t, r.Run(ctx, e.Location))
	require.Zero(t, e.App.Ent.BackupTombstone.Query().Where(backuptombstone.ID(stuckT.ID)).CountX(ctx))
}

func TestReconcileIgnoresForeignBackups(t *testing.T) {
	ctx := context.Background()
	e, pc := backupEnv(t)
	logs := &lockedBuffer{}
	r := newReconciler(e, newLogger(logs))
	_, vmid := running(t, e, "web")
	orphan := archive(t, pc, vmid, "pbs", "nebla server 1 backup 99")
	foreign := archive(t, pc, vmid, "pbs", "my own backup")

	require.NoError(t, r.Run(ctx, e.Location))
	items, err := pc.BackupContent(ctx, "pve1", "pbs", 0)
	require.NoError(t, err)
	require.Len(t, items, 2, "nothing deleted")
	require.Contains(t, logs.String(), orphan)
	require.NotContains(t, logs.String(), foreign)
}

func TestReconcileOldStorage(t *testing.T) {
	ctx := context.Background()
	e, pc := backupEnv(t)
	for _, n := range []string{"pve1", "pve2"} {
		e.App.PVE.AddStorage(n, "pbs2", []string{"backup"}, true)
	}
	r := newReconciler(e, quiet())
	srv, vmid := running(t, e, "web")
	row := backupRow(t, e, srv, "pbs", archive(t, pc, vmid, "pbs", "nebla server 1 backup 1"))
	require.NoError(t, e.App.Ent.Location.UpdateOneID(e.Location).SetBackupStorage("pbs2").Exec(ctx))
	require.NoError(t, pc.DeleteVolume(ctx, "pve1", "pbs", *row.Volid))

	require.NoError(t, r.Run(ctx, e.Location))
	require.Equal(t, backup.StatusMissing, backupStatus(e, row.ID))
}

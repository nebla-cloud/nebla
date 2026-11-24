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
	"net/http"
	"strings"
	"testing"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/backups"
	"github.com/nebla-cloud/nebla/internal/catalog"
	"github.com/nebla-cloud/nebla/internal/ent"
	entbackup "github.com/nebla-cloud/nebla/internal/ent/backup"
	"github.com/nebla-cloud/nebla/internal/ent/volume"
	"github.com/nebla-cloud/nebla/internal/ext"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/riverqueue/river"
	"github.com/stretchr/testify/require"
)

type restoreEnv struct {
	*testutil.ServerEnv
	s   testutil.CreatedServer
	w   *servers.RestoreServerWorker
	svc *backups.Service
	bus *fipBus
}

// newRestoreEnv builds a running server in a location with PBS storage "pbs".
func newRestoreEnv(t *testing.T) *restoreEnv {
	t.Helper()
	e, cw := directEnv(t)
	for _, n := range []string{"pve1", "pve2"} {
		e.App.PVE.AddStorage(n, "pbs", []string{"backup"}, true)
	}
	require.NoError(t, e.App.Ent.Location.UpdateOneID(e.Location).SetBackupStorage("pbs").SetBackupStorageOk(true).Exec(context.Background()))
	s := builtServer(t, e, cw, "web")
	bus := &fipBus{}
	o := opts(e)
	o.Bus = bus
	return &restoreEnv{ServerEnv: e, s: s, w: servers.NewRestoreServerWorker(o), bus: bus,
		svc: backups.New(backups.Options{SQL: e.App.SQL, Ent: e.App.Ent, Catalog: e.App.Catalog, Jobs: e.App.Jobs, Logger: quiet()})}
}

// backUp vzdumps the server's VM now and records an available backup of it; fp is its network fingerprint.
func (e *restoreEnv) backUp(t *testing.T, fp string) *ent.Backup {
	t.Helper()
	ctx := context.Background()
	row := e.App.Ent.Server.GetX(ctx, e.s.ServerID)
	pc, err := e.App.Catalog.ProxmoxFor(ctx, e.Location)
	require.NoError(t, err)
	before, err := pc.BackupContent(ctx, row.Node, "pbs", *row.Vmid)
	require.NoError(t, err)
	up, err := pc.Vzdump(ctx, row.Node, *row.Vmid, "pbs", "nebla")
	require.NoError(t, err)
	require.NoError(t, pc.WaitTask(ctx, row.Node, up))
	items, err := pc.BackupContent(ctx, row.Node, "pbs", *row.Vmid)
	require.NoError(t, err)
	require.Len(t, items, len(before)+1)
	return e.App.Ent.Backup.Create().SetProjectID(e.Project).SetServerID(row.ID).SetLocationID(e.Location).SetStorage("pbs").
		SetNode(row.Node).SetKind(entbackup.KindManual).SetStatus(entbackup.StatusAvailable).SetVolid(items[len(items)-1].Volid).
		SetInstanceGeneration(servers.InstanceGeneration(row)).SetNetworkFingerprint(fp).SaveX(ctx)
}

// sameNet is the server's current network fingerprint.
func (e *restoreEnv) sameNet(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	fp, err := servers.NetworkFingerprint(ctx, e.App.Ent, e.App.Ent.Server.GetX(ctx, e.s.ServerID))
	require.NoError(t, err)
	return fp
}

// request asks for the restore of b and returns the queued job.
func (e *restoreEnv) request(t *testing.T, b *ent.Backup, attempt int) (actions.Action, *river.Job[servers.RestoreServerArgs]) {
	t.Helper()
	a, err := e.svc.Restore(context.Background(), e.Project, e.s.ServerID, b.ID)
	require.NoError(t, err)
	return a, jobOf(queuedArgs[servers.RestoreServerArgs](t, e.ServerEnv, a.ID), attempt)
}

func (e *restoreEnv) backupStatus(t *testing.T, id int64) entbackup.Status {
	t.Helper()
	return e.App.Ent.Backup.GetX(context.Background(), id).Status
}

func TestRestoreSwapsAndKeepsVolumes(t *testing.T) {
	ctx := context.Background()
	e := newRestoreEnv(t)
	old := vmidOf(t, e.ServerEnv, e.s.ServerID)
	vol := attachVol(t, e.ServerEnv, e.s.ServerID, old, "data", "virtio1", 10)
	oldVM, _ := e.App.PVE.VM(old)
	before := e.App.Ent.Server.GetX(ctx, e.s.ServerID)
	b := e.backUp(t, e.sameNet(t))

	a, job := e.request(t, b, 1)
	require.Equal(t, servers.StatusRebuilding, status(t, e.ServerEnv, e.s.ServerID))
	require.NoError(t, e.w.Work(ctx, job))
	require.Equal(t, actions.StatusSuccess, testutil.WaitAction(t, e.App.Ent, a.ID).Status)
	after := e.App.Ent.Server.GetX(ctx, e.s.ServerID)
	require.NotEqual(t, old, *after.Vmid, "a new VMID")
	_, ok := e.App.PVE.VM(old)
	require.False(t, ok, "old VM destroyed")
	vm, ok := e.App.PVE.VM(*after.Vmid)
	require.True(t, ok)
	require.Equal(t, oldVM.Config["virtio1"], vm.Config["virtio1"], "volume re-attached")
	require.NotContains(t, vm.Config["description"], "virtio1:")
	require.Equal(t, oldVM.Config["description"], vm.Config["description"])
	require.Equal(t, oldVM.Config["net0"], vm.Config["net0"], "same MAC and bridge")
	require.True(t, hasTag(vm.Tags, "nebla") && hasTag(vm.Tags, tag(e.s.ServerID)), vm.Tags)
	require.Equal(t, "running", vm.Status)
	require.Equal(t, []string{"10.0.0.10"}, vm.IPSets["ipfilter-net0"])
	require.Equal(t, servers.StatusRunning, string(after.Status))
	require.Equal(t, before.ImageID, after.ImageID, "restore keeps the image")
	require.Equal(t, before.TemplateVmid, after.TemplateVmid)
	require.Equal(t, 2, after.IsoGeneration)
	require.Equal(t, *after.IsoVolid+",media=cdrom", vm.Config["ide2"])
	_, ok = e.App.PVE.Volume(isoVolid(e.s.ServerID))
	require.False(t, ok, "old ISO deleted")
	require.Nil(t, after.RebuildVmid)
	require.Nil(t, after.RebuildCloneUpid)
	require.Nil(t, after.RebuildCloneNonce)
	require.Zero(t, after.RebuildStep)
	require.Len(t, taggedVMs(t, e.ServerEnv, e.s.ServerID), 1)
	got := volRow(t, e.ServerEnv, vol.ID)
	require.Equal(t, volume.StatusAttached, got.Status)
	require.Equal(t, "virtio1", got.Device)
	require.Equal(t, entbackup.StatusAvailable, e.backupStatus(t, b.ID))
	evs := e.bus.of(ext.BackupRestored)
	require.Len(t, evs, 1)
	require.Equal(t, b.ID, evs[0].ResourceID)
	require.Equal(t, "backup", evs[0].ResourceType)
	require.Equal(t, e.s.ServerID, evs[0].ServerID)
	require.Equal(t, e.Project, evs[0].ProjectID)
	require.Equal(t, 1, countCalls(e.ServerEnv, "POST /nodes/pve1/qemu"))

	// An off server stays off.
	setStatus(t, e.ServerEnv, e.s.ServerID, servers.StatusOff)
	e.App.PVE.SetVMStatus(*after.Vmid, "stopped")
	_, job = e.request(t, b, 1)
	require.False(t, job.Args.StartAfter)
	require.NoError(t, e.w.Work(ctx, job))
	require.Equal(t, servers.StatusOff, status(t, e.ServerEnv, e.s.ServerID))
	vm, _ = e.App.PVE.VM(vmidOf(t, e.ServerEnv, e.s.ServerID))
	require.Equal(t, "stopped", vm.Status)
	require.Equal(t, 1, vmCount(t, e.ServerEnv))
}

func TestRestoreFromOldStorage(t *testing.T) {
	ctx := context.Background()
	e := newRestoreEnv(t)
	old := vmidOf(t, e.ServerEnv, e.s.ServerID)
	b := e.backUp(t, e.sameNet(t))
	for _, n := range []string{"pve1", "pve2"} {
		e.App.PVE.AddStorage(n, "pbs2", []string{"backup"}, true)
	}
	require.NoError(t, e.App.Ent.Location.UpdateOneID(e.Location).SetBackupStorage("pbs2").Exec(ctx))

	a, job := e.request(t, b, 1)
	require.NoError(t, e.w.Work(ctx, job))
	require.Equal(t, actions.StatusSuccess, testutil.WaitAction(t, e.App.Ent, a.ID).Status)
	require.NotEqual(t, old, vmidOf(t, e.ServerEnv, e.s.ServerID), "restored from the archive on pbs")
	require.Equal(t, servers.StatusRunning, status(t, e.ServerEnv, e.s.ServerID))
	require.Equal(t, entbackup.StatusAvailable, e.backupStatus(t, b.ID))
}

func TestRestoreKeepsIdentityWhenNetworkSame(t *testing.T) {
	ctx := context.Background()
	e := newRestoreEnv(t)
	b := e.backUp(t, e.sameNet(t))
	require.Equal(t, 1, b.InstanceGeneration)
	_, job := e.request(t, b, 1)
	require.Equal(t, 1, job.Args.InstanceGeneration)
	require.NoError(t, e.w.Work(ctx, job))
	row := e.App.Ent.Server.GetX(ctx, e.s.ServerID)
	require.Equal(t, 2, row.IsoGeneration)
	require.Equal(t, 1, row.InstanceGeneration)
	require.Contains(t, isoFiles(t, e.ServerEnv, *row.IsoVolid)["meta-data"], "instance-id: nb-srv-"+sid(e.s.ServerID)+"-1\n")
}

func TestRestoreNewIdentityWhenNetworkChanged(t *testing.T) {
	ctx := context.Background()
	e := newRestoreEnv(t)
	b := e.backUp(t, "other")
	_, job := e.request(t, b, 1)
	require.Zero(t, job.Args.InstanceGeneration)
	require.NoError(t, e.w.Work(ctx, job))
	row := e.App.Ent.Server.GetX(ctx, e.s.ServerID)
	require.Equal(t, 2, row.IsoGeneration)
	require.Equal(t, 2, row.InstanceGeneration)
	require.Contains(t, isoFiles(t, e.ServerEnv, *row.IsoVolid)["meta-data"], "instance-id: nb-srv-"+sid(e.s.ServerID)+"-2\n")
}

func TestRestoreResumesEveryStep(t *testing.T) {
	ctx := context.Background()
	e := newRestoreEnv(t)
	b := e.backUp(t, e.sameNet(t))
	crashes := append([]int{-1, -2}, servers.RestoreSteps...)
	for _, n := range crashes {
		old := vmidOf(t, e.ServerEnv, e.s.ServerID)
		a, job := e.request(t, b, 1)
		crashCtx, crash := context.WithCancel(ctx)
		switch n {
		case -1:
			// Crash between the restore request and saving its task.
			servers.SetRestoreAfterPost(e.w, crash)
		case -2:
			// Crash once the task is saved, before the restored VM is sanitized.
			servers.SetRestoreAfterUpid(e.w, crash)
		default:
			servers.SetRestoreAfterStep(e.w, func(s int) {
				if s == n {
					crash()
				}
			})
		}
		require.Error(t, e.w.Work(crashCtx, job), "step %d", n)
		crash()
		if n == -2 {
			require.NotNil(t, e.App.Ent.Server.GetX(ctx, e.s.ServerID).RebuildCloneUpid, "task saved before the crash")
		}
		require.Equal(t, servers.StatusRebuilding, status(t, e.ServerEnv, e.s.ServerID), "step %d", n)
		require.Equal(t, entbackup.StatusRestoring, e.backupStatus(t, b.ID), "step %d", n)

		servers.SetRestoreAfterPost(e.w, nil)
		servers.SetRestoreAfterUpid(e.w, nil)
		servers.SetRestoreAfterStep(e.w, nil)
		job.Attempt = 2
		require.NoError(t, e.w.Work(ctx, job), "step %d", n)
		require.Equal(t, actions.StatusSuccess, testutil.WaitAction(t, e.App.Ent, a.ID).Status, "step %d", n)
		require.Equal(t, 1, vmCount(t, e.ServerEnv), "step %d: one VM left", n)
		require.Len(t, taggedVMs(t, e.ServerEnv, e.s.ServerID), 1, "step %d", n)
		row := e.App.Ent.Server.GetX(ctx, e.s.ServerID)
		require.NotEqual(t, old, *row.Vmid, "step %d", n)
		require.Nil(t, row.RebuildVmid, "step %d", n)
		require.Equal(t, servers.StatusRunning, string(row.Status), "step %d", n)
		vm, _ := e.App.PVE.VM(*row.Vmid)
		require.Equal(t, "running", vm.Status, "step %d", n)
		require.Equal(t, entbackup.StatusAvailable, e.backupStatus(t, b.ID), "step %d", n)
	}
	require.Len(t, e.bus.of(ext.BackupRestored), len(crashes))
	require.Equal(t, len(crashes), countCalls(e.ServerEnv, "POST /nodes/pve1/qemu"), "a resume never restores twice")
}

func TestRestoreFailureKeepsOldVM(t *testing.T) {
	for _, tc := range []struct {
		name   string
		fail   func(e *restoreEnv)
		detail string
	}{
		{"restore task", func(e *restoreEnv) { e.App.PVE.FailRestore = true }, "restore task failed"},
		{"resize", func(e *restoreEnv) {
			// The server grew after the backup, so the restored disk is resized.
			row := e.App.Ent.Server.GetX(context.Background(), e.s.ServerID)
			e.App.Ent.Server.UpdateOne(row).SetDiskGB(row.DiskGB + 10).ExecX(context.Background())
			e.App.PVE.FailNext("/resize", http.StatusInternalServerError)
		}, "resize disk"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			e := newRestoreEnv(t)
			before := e.App.Ent.Server.GetX(ctx, e.s.ServerID)
			b := e.backUp(t, e.sameNet(t))
			a, job := e.request(t, b, 1)
			tc.fail(e)
			var cancel *river.JobCancelError
			require.True(t, errors.As(e.w.Work(ctx, job), &cancel))
			e.App.PVE.FailRestore = false
			got := testutil.WaitAction(t, e.App.Ent, a.ID)
			require.Equal(t, actions.StatusError, got.Status)
			require.Contains(t, got.Error.Detail, tc.detail)
			require.NotContains(t, got.Error.Detail, "cleanup incomplete")
			require.Equal(t, 1, vmCount(t, e.ServerEnv), "new VM destroyed")
			vm, ok := e.App.PVE.VM(*before.Vmid)
			require.True(t, ok)
			require.Equal(t, "running", vm.Status)
			after := e.App.Ent.Server.GetX(ctx, e.s.ServerID)
			require.Equal(t, servers.StatusRunning, string(after.Status))
			require.Equal(t, *before.Vmid, *after.Vmid)
			require.Equal(t, 1, after.IsoGeneration)
			require.Nil(t, after.RebuildVmid)
			_, ok = e.App.PVE.Volume("local:iso/nb-srv-" + sid(e.s.ServerID) + "-2.iso")
			require.False(t, ok)
			require.Equal(t, entbackup.StatusAvailable, e.backupStatus(t, b.ID))
			require.Empty(t, e.bus.of(ext.BackupRestored))

			// The server and backup are usable again.
			_, job = e.request(t, b, 1)
			require.NoError(t, e.w.Work(ctx, job))
			require.Equal(t, 1, vmCount(t, e.ServerEnv))
			vm, _ = e.App.PVE.VM(vmidOf(t, e.ServerEnv, e.s.ServerID))
			disk := catalog.BootDisk(vm.Config)
			require.Contains(t, vm.Config[disk], fmt.Sprintf("size=%dG", after.DiskGB), "resized to the server's disk")
		})
	}
}

func TestRestoreDuplicateRunSnoozes(t *testing.T) {
	ctx := context.Background()
	e := newRestoreEnv(t)
	b := e.backUp(t, e.sameNet(t))
	a, job := e.request(t, b, 1)
	reached, release := make(chan struct{}), make(chan struct{})
	servers.SetRestoreAfterStep(e.w, func(n int) {
		if n == servers.StepVMID {
			close(reached)
			<-release
		}
	})
	done := make(chan error, 1)
	go func() { done <- e.w.Work(ctx, job) }()
	select {
	case <-reached:
	case err := <-done:
		t.Fatalf("first run ended before its first step: %v", err)
	}

	// A rescued second run of the same job must not run beside the first.
	dup := jobOf(job.Args, 2)
	var snooze *river.JobSnoozeError
	require.ErrorAs(t, e.w.Work(ctx, dup), &snooze)
	close(release)
	require.NoError(t, <-done)
	require.Equal(t, actions.StatusSuccess, testutil.WaitAction(t, e.App.Ent, a.ID).Status)
	require.NoError(t, e.w.Work(ctx, dup), "the finished action makes the late run a no-op")
	require.Equal(t, 1, vmCount(t, e.ServerEnv))
	require.Equal(t, 1, countCalls(e.ServerEnv, "POST /nodes/pve1/qemu"))
}

func queuedBackup(t *testing.T, e *testutil.ServerEnv, serverID int64, st entbackup.Status) *ent.Backup {
	t.Helper()
	row := e.App.Ent.Server.GetX(context.Background(), serverID)
	c := e.App.Ent.Backup.Create().SetProjectID(e.Project).SetServerID(serverID).SetLocationID(e.Location).SetStorage("pbs").
		SetNode(row.Node).SetKind(entbackup.KindScheduled).SetStatus(st).SetInstanceGeneration(1)
	if st == entbackup.StatusAvailable {
		c.SetVolid("pbs:backup/vm/1/" + strings.ToLower(t.Name()))
	}
	return c.SaveX(context.Background())
}

func TestRebuildCancelsQueuedBackups(t *testing.T) {
	ctx := context.Background()
	e, s, _ := rebuildEnv(t)
	q := queuedBackup(t, e, s.ServerID, entbackup.StatusQueued)
	kept := queuedBackup(t, e, s.ServerID, entbackup.StatusAvailable)
	startAction(t, e, s.ServerID, "rebuild", map[string]any{"image": "debian"})
	_, err := e.App.Ent.Backup.Get(ctx, q.ID)
	require.True(t, ent.IsNotFound(err), "queued backup dropped")
	require.Equal(t, entbackup.StatusAvailable, e.App.Ent.Backup.GetX(ctx, kept.ID).Status)
}

func TestDeleteServerCancelsQueuedBackups(t *testing.T) {
	ctx := context.Background()
	e, s, _ := rebuildEnv(t)
	q := queuedBackup(t, e, s.ServerID, entbackup.StatusQueued)
	kept := queuedBackup(t, e, s.ServerID, entbackup.StatusAvailable)
	expect[actionBody](t, e.C.Send(http.MethodDelete, spath(s.ServerID), nil), http.StatusOK)
	_, err := e.App.Ent.Backup.Get(ctx, q.ID)
	require.True(t, ent.IsNotFound(err), "queued backup dropped")
	require.Equal(t, entbackup.StatusAvailable, e.App.Ent.Backup.GetX(ctx, kept.ID).Status)
}

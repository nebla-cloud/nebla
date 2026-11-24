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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/ent/server"
	"github.com/nebla-cloud/nebla/internal/ent/snapshot"
	"github.com/nebla-cloud/nebla/internal/ext"
	"github.com/nebla-cloud/nebla/internal/pve"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/nebla-cloud/nebla/internal/snapshots"
	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/require"
)

type recorder struct {
	mu  sync.Mutex
	evs []ext.Event
}

func (r *recorder) Publish(_ context.Context, e ext.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.evs = append(r.evs, e)
}

func (r *recorder) all() []ext.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.evs)
}

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func (e *env) opts(rec *recorder) snapshots.WorkerOptions {
	o := snapshots.WorkerOptions{SQL: e.App.SQL, Ent: e.App.Ent, Catalog: e.App.Catalog, Logger: quiet(),
		PollInterval: 5 * time.Millisecond, ShutdownTimeout: 300 * time.Millisecond, ShutdownGrace: 300 * time.Millisecond,
		PowerWait: 5 * time.Second}
	if rec != nil {
		o.Bus = rec
	}
	return o
}

func jobOf[T river.JobArgs](args T, attempt int) *river.Job[T] {
	return &river.Job[T]{JobRow: &rivertype.JobRow{ID: 1, Attempt: attempt, MaxAttempts: servers.MaxAttempts + 2}, Args: args}
}

func (e *env) action(t *testing.T, id int64) actions.Action {
	t.Helper()
	a, err := actions.Get(context.Background(), e.App.Ent, e.Project, id)
	require.NoError(t, err)
	return a
}

// work runs the action's current River job once with o.
func (e *env) work(ctx context.Context, t *testing.T, o snapshots.WorkerOptions, actionID int64, attempt int) error {
	t.Helper()
	a := e.action(t, actionID)
	require.NotNil(t, a.RiverJobID)
	j, err := e.App.Jobs.Client.JobGet(context.Background(), *a.RiverJobID)
	require.NoError(t, err)
	switch a.Command {
	case snapshots.KindCreateSnapshot:
		var args snapshots.CreateSnapshotArgs
		require.NoError(t, json.Unmarshal(j.EncodedArgs, &args))
		return snapshots.NewCreateSnapshotWorker(o).Work(ctx, jobOf(args, attempt))
	case snapshots.KindDeleteSnapshot:
		var args snapshots.DeleteSnapshotArgs
		require.NoError(t, json.Unmarshal(j.EncodedArgs, &args))
		return snapshots.NewDeleteSnapshotWorker(o).Work(ctx, jobOf(args, attempt))
	}
	t.Fatalf("unknown command %s", a.Command)
	return nil
}

// finish runs the action's job once and returns the action, which must be finished.
func (e *env) finish(t *testing.T, o snapshots.WorkerOptions, actionID int64) actions.Action {
	t.Helper()
	_ = e.work(context.Background(), t, o, actionID, 1)
	a := e.action(t, actionID)
	require.NotNil(t, a.Finished, "action still running")
	return a
}

// bootServer is a running server whose VM looks like a built one: a real boot disk, a volume, a seed ISO and a NIC.
func (e *env) bootServer(t *testing.T, name string) (testutil.CreatedServer, int) {
	t.Helper()
	ctx := context.Background()
	s := e.RunningServer(name)
	vmid := 100 + int(s.ServerID)
	pc, err := e.App.Catalog.ProxmoxFor(ctx, e.Location)
	require.NoError(t, err)
	boot, err := pc.AllocVolume(ctx, "pve1", "local-lvm", vmid, fmt.Sprintf("vm-%d-disk-0", vmid), 10)
	require.NoError(t, err)
	for k, v := range map[string]string{
		"scsi0":   boot + ",discard=on,size=10G",
		"virtio1": "ceph:vm-1-disk-0,size=5G",
		"ide2":    fmt.Sprintf("local:iso/nb-srv-%d-1.iso,media=cdrom", s.ServerID),
		"net0":    "virtio=BC:24:11:00:00:01,bridge=vmbr0,firewall=1,tag=10",
		"boot":    "order=scsi0;ide2;net0",
		"scsihw":  "virtio-scsi-single",
		"ostype":  "l26",
		"serial0": "socket",
		"vga":     "serial0",
		"cpu":     "host",
		"agent":   "1",
	} {
		e.App.PVE.SetVMConfig(vmid, k, v)
	}
	return s, vmid
}

func (e *env) take(t *testing.T, name string, force bool) (snapshots.Snapshot, actions.Action) {
	t.Helper()
	snap, act, err := e.svc.Create(context.Background(), e.Project, snapshots.CreateInput{Server: name, Name: name + "-snap", ForceStop: force})
	require.NoError(t, err)
	return snap, act
}

func snapTag(id int64) string { return fmt.Sprintf("nebla-snapshot-%d", id) }

// templates lists the VMs tagged as snapshot id's.
func (e *env) templates(t *testing.T, id int64) []pve.VM {
	t.Helper()
	pc, err := e.App.Catalog.ProxmoxFor(context.Background(), e.Location)
	require.NoError(t, err)
	vms, err := pc.VMs(context.Background())
	require.NoError(t, err)
	return slices.DeleteFunc(vms, func(v pve.VM) bool { return !slices.Contains(servers.SplitTags(v.Tags), snapTag(id)) })
}

func (e *env) countCalls(prefix string) int {
	n := 0
	for _, c := range e.App.PVE.Calls() {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

func (e *env) countExact(call string) int {
	return len(slices.DeleteFunc(e.App.PVE.Calls(), func(c string) bool { return c != call }))
}

func (e *env) requireServer(t *testing.T, id int64, vmid int, want string) {
	t.Helper()
	v, ok := e.App.PVE.VM(vmid)
	require.True(t, ok)
	pveWant := "stopped"
	if want == servers.StatusRunning {
		pveWant = "running"
	}
	require.Equal(t, pveWant, v.Status, "vm state")
	row, err := e.App.Ent.Server.Get(context.Background(), id)
	require.NoError(t, err)
	require.Equal(t, want, string(row.Status), "server row status")
}

func (e *env) usage(t *testing.T) (int, int) {
	t.Helper()
	u, err := snapshots.UsageWith(servers.Usage)(context.Background(), e.App.Ent, e.Project)
	require.NoError(t, err)
	return u.Snapshots, u.SnapshotGB
}

func TestCreateSnapshotRunning(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	s, vmid := e.bootServer(t, "web1")
	snap, act := e.take(t, "web1", false)
	rec := &recorder{}

	a := e.finish(t, e.opts(rec), act.ID)
	require.Equal(t, actions.StatusSuccess, a.Status, "%+v", a.Error)

	row := e.App.Ent.Snapshot.GetX(ctx, snap.ID)
	require.Equal(t, snapshot.StatusAvailable, row.Status)
	require.False(t, row.WasRunning, "cleared once the server runs again, so a delete never restarts it")
	require.Equal(t, "scsi0", row.BootKey)
	require.Equal(t, "ceph", row.Storage)
	require.Equal(t, "", row.Node, "shared storage")
	require.NotNil(t, row.Vmid)
	require.Nil(t, row.Upid)

	tpl, ok := e.App.PVE.VM(*row.Vmid)
	require.True(t, ok)
	require.True(t, tpl.Template)
	require.Equal(t, fmt.Sprintf("nebla-snap-%d", snap.ID), tpl.Name)
	require.Equal(t, []string{"nebla-snapshot", snapTag(snap.ID)}, servers.SplitTags(tpl.Tags))
	require.True(t, strings.HasPrefix(tpl.Config["scsi0"], "ceph:vm-"), tpl.Config["scsi0"])
	require.Contains(t, tpl.Config["scsi0"], "discard=on")
	require.NotContains(t, tpl.Config["scsi0"], "import-from")
	for _, k := range []string{"virtio1", "ide2", "agent"} {
		require.NotContains(t, tpl.Config, k)
	}
	require.Equal(t, "virtio,bridge=vmbr0,tag=10", tpl.Config["net0"], "no MAC and no firewall flag")
	require.Equal(t, "order=scsi0;net0", tpl.Config["boot"])
	for k, v := range map[string]string{"scsihw": "virtio-scsi-single", "ostype": "l26", "serial0": "socket", "vga": "serial0", "cpu": "host"} {
		require.Equal(t, v, tpl.Config[k], k)
	}
	require.NotContains(t, tpl.Config, "bios")
	require.NotContains(t, tpl.Config, "efidisk0")
	require.Contains(t, tpl.Config["description"], *row.Nonce)
	disks := 0
	for k := range tpl.Config {
		if strings.HasPrefix(k, "scsi") && k != "scsihw" || strings.HasPrefix(k, "virtio") || strings.HasPrefix(k, "sata") || strings.HasPrefix(k, "ide") {
			disks++
		}
	}
	require.Equal(t, 1, disks, "only the boot disk: %v", tpl.Config)

	require.Equal(t, 1, e.countCalls(fmt.Sprintf("POST /nodes/pve1/qemu/%d/status/shutdown", vmid)))
	require.Zero(t, e.countCalls(fmt.Sprintf("POST /nodes/pve1/qemu/%d/status/stop", vmid)))
	e.requireServer(t, s.ServerID, vmid, servers.StatusRunning)
	src, _ := e.App.PVE.VM(vmid)
	require.Contains(t, src.Config, "virtio1", "the source keeps its volume")

	evs := rec.all()
	require.Len(t, evs, 1)
	require.Equal(t, ext.SnapshotCreated, evs[0].Kind)
	require.Equal(t, snapshots.ResourceType, evs[0].ResourceType)
	require.Equal(t, snap.ID, evs[0].ResourceID)
	require.Equal(t, e.Project, evs[0].ProjectID)
	require.Equal(t, map[string]any{"size_gb": 10, "location_id": e.Location}, evs[0].Data)
}

func TestCreateSnapshotOff(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	s, vmid := e.bootServer(t, "web1")
	e.App.PVE.SetVMStatus(vmid, "stopped")
	require.NoError(t, e.App.Ent.Server.UpdateOneID(s.ServerID).SetStatus(server.StatusOff).Exec(ctx))
	snap, act := e.take(t, "web1", false)

	a := e.finish(t, e.opts(nil), act.ID)
	require.Equal(t, actions.StatusSuccess, a.Status, "%+v", a.Error)
	require.False(t, e.App.Ent.Snapshot.GetX(ctx, snap.ID).WasRunning)
	require.Zero(t, e.countCalls(fmt.Sprintf("POST /nodes/pve1/qemu/%d/status/", vmid)), "no power calls")
	e.requireServer(t, s.ServerID, vmid, servers.StatusOff)
	require.Len(t, e.templates(t, snap.ID), 1)
}

func TestCreateSnapshotNodeLocalStorage(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	require.NoError(t, e.App.Ent.Location.UpdateOneID(e.Location).SetSnapshotStorage("local-lvm").Exec(ctx))
	e.bootServer(t, "web1")
	snap, act := e.take(t, "web1", false)

	a := e.finish(t, e.opts(nil), act.ID)
	require.Equal(t, actions.StatusSuccess, a.Status, "%+v", a.Error)
	row := e.App.Ent.Snapshot.GetX(ctx, snap.ID)
	require.Equal(t, "local-lvm", row.Storage)
	require.Equal(t, "pve1", row.Node)
	tpl, _ := e.App.PVE.VM(*row.Vmid)
	require.True(t, strings.HasPrefix(tpl.Config["scsi0"], "local-lvm:vm-"), tpl.Config["scsi0"])
}

func TestCreateSnapshotUEFI(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	_, vmid := e.bootServer(t, "web1")
	pc, err := e.App.Catalog.ProxmoxFor(ctx, e.Location)
	require.NoError(t, err)
	efi, err := pc.AllocVolume(ctx, "pve1", "local-lvm", vmid, fmt.Sprintf("vm-%d-disk-1", vmid), 1)
	require.NoError(t, err)
	tpm, err := pc.AllocVolume(ctx, "pve1", "local-lvm", vmid, fmt.Sprintf("vm-%d-disk-2", vmid), 1)
	require.NoError(t, err)
	e.App.PVE.SetVMConfig(vmid, "bios", "ovmf")
	e.App.PVE.SetVMConfig(vmid, "machine", "q35")
	e.App.PVE.SetVMConfig(vmid, "efidisk0", efi+",efitype=4m,pre-enrolled-keys=1,size=4M")
	e.App.PVE.SetVMConfig(vmid, "tpmstate0", tpm+",version=v2.0,size=4M")
	snap, act := e.take(t, "web1", false)

	a := e.finish(t, e.opts(nil), act.ID)
	require.Equal(t, actions.StatusSuccess, a.Status, "%+v", a.Error)
	tpl, _ := e.App.PVE.VM(*e.App.Ent.Snapshot.GetX(ctx, snap.ID).Vmid)
	require.Equal(t, "ovmf", tpl.Config["bios"])
	require.Equal(t, "q35", tpl.Config["machine"])
	require.True(t, strings.HasPrefix(tpl.Config["efidisk0"], "ceph:vm-"), tpl.Config["efidisk0"])
	require.Contains(t, tpl.Config["efidisk0"], "efitype=4m")
	require.Contains(t, tpl.Config["efidisk0"], "pre-enrolled-keys=1")
	require.True(t, strings.HasPrefix(tpl.Config["tpmstate0"], "ceph:vm-"), tpl.Config["tpmstate0"])
	require.Contains(t, tpl.Config["tpmstate0"], "version=v2.0")
}

func TestCreateSnapshotSataBoot(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	_, vmid := e.bootServer(t, "web1")
	src, _ := e.App.PVE.VM(vmid)
	e.App.PVE.SetVMConfig(vmid, "sata0", src.Config["scsi0"])
	e.App.PVE.SetVMConfig(vmid, "scsi0", "local-lvm:vm-1-disk-9,size=1G")
	e.App.PVE.SetVMConfig(vmid, "boot", "order=sata0;net0")
	snap, act := e.take(t, "web1", false)

	a := e.finish(t, e.opts(nil), act.ID)
	require.Equal(t, actions.StatusSuccess, a.Status, "%+v", a.Error)
	row := e.App.Ent.Snapshot.GetX(ctx, snap.ID)
	require.Equal(t, "sata0", row.BootKey)
	tpl, _ := e.App.PVE.VM(*row.Vmid)
	require.True(t, strings.HasPrefix(tpl.Config["sata0"], "ceph:vm-"), tpl.Config["sata0"])
	require.NotContains(t, tpl.Config, "scsi0")
}

// crashAfter runs the create job and cancels its context once step is saved, as if the process died.
func (e *env) crashAfter(t *testing.T, o snapshots.WorkerOptions, act actions.Action, step int) {
	t.Helper()
	crashCtx, crash := context.WithCancel(context.Background())
	defer crash()
	o = snapshots.SetAfterStep(o, func(n int) {
		if n == step {
			crash()
		}
	})
	err := e.work(crashCtx, t, o, act.ID, 1)
	require.Error(t, err)
	var cancel *river.JobCancelError
	require.False(t, errors.As(err, &cancel), "a crash is not a permanent failure: %v", err)
	require.Equal(t, actions.StatusRunning, e.action(t, act.ID).Status)
}

func TestSnapshotJobResumesEveryStep(t *testing.T) {
	for step := 1; step <= 6; step++ {
		t.Run(fmt.Sprint("after step ", step), func(t *testing.T) {
			ctx := context.Background()
			e := newEnv(t)
			s, vmid := e.bootServer(t, "web1")
			snap, act := e.take(t, "web1", false)
			o := e.opts(nil)
			e.crashAfter(t, o, act, step)
			require.Equal(t, step, e.App.Ent.Snapshot.GetX(ctx, snap.ID).Step)

			require.NoError(t, e.work(ctx, t, o, act.ID, 2))
			require.Equal(t, actions.StatusSuccess, e.action(t, act.ID).Status)
			tpls := e.templates(t, snap.ID)
			require.Len(t, tpls, 1)
			require.True(t, tpls[0].Template)
			require.Equal(t, 1, e.countCalls("GET /cluster/nextid"))
			require.Equal(t, 1, e.countExact("POST /nodes/pve1/qemu"))
			e.requireServer(t, s.ServerID, vmid, servers.StatusRunning)
			require.Equal(t, snapshot.StatusAvailable, e.App.Ent.Snapshot.GetX(ctx, snap.ID).Status)
		})
	}
	t.Run("mid import", func(t *testing.T) {
		ctx := context.Background()
		e := newEnv(t)
		s, vmid := e.bootServer(t, "web1")
		snap, act := e.take(t, "web1", false)
		crashCtx, crash := context.WithCancel(ctx)
		defer crash()
		o := snapshots.SetAfterStep(e.opts(nil), func(n int) {
			if n != 3 {
				return
			}
			// Hold the create task so the crash lands while it runs.
			e.App.PVE.SetTaskDelay(time.Hour)
			go func() {
				defer crash()
				for crashCtx.Err() == nil {
					r, err := e.App.Ent.Snapshot.Get(ctx, snap.ID)
					if err == nil && r.Upid != nil {
						return
					}
					time.Sleep(5 * time.Millisecond)
				}
			}()
		})
		err := e.work(crashCtx, t, o, act.ID, 1)
		require.Error(t, err)
		row := e.App.Ent.Snapshot.GetX(ctx, snap.ID)
		require.Equal(t, 3, row.Step)
		require.NotNil(t, row.Upid, "create upid saved before waiting")
		e.App.PVE.SetTaskDelay(0)
		e.App.PVE.FinishTasks()

		require.NoError(t, e.work(ctx, t, e.opts(nil), act.ID, 2))
		require.Equal(t, actions.StatusSuccess, e.action(t, act.ID).Status)
		require.Len(t, e.templates(t, snap.ID), 1)
		e.requireServer(t, s.ServerID, vmid, servers.StatusRunning)
	})
}

// The process dies after the create POST returned but before its upid was saved: the retry POSTs again and claims the VM.
func TestSnapshotCrashBeforeUpidSaved(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	s, vmid := e.bootServer(t, "web1")
	snap, act := e.take(t, "web1", false)
	crashCtx, crash := context.WithCancel(ctx)
	defer crash()
	o := snapshots.SetAfterCreatePost(e.opts(nil), crash)
	err := e.work(crashCtx, t, o, act.ID, 1)
	require.Error(t, err)
	var cancel *river.JobCancelError
	require.False(t, errors.As(err, &cancel), "%v", err)
	row := e.App.Ent.Snapshot.GetX(ctx, snap.ID)
	require.Equal(t, 3, row.Step)
	require.Nil(t, row.Upid, "crashed before the upid was saved")

	require.NoError(t, e.work(ctx, t, e.opts(nil), act.ID, 2))
	require.Equal(t, actions.StatusSuccess, e.action(t, act.ID).Status)
	require.Equal(t, 2, e.countExact("POST /nodes/pve1/qemu"), "the create was posted again")
	require.Equal(t, 1, e.countCalls("GET /cluster/nextid"), "the VM was claimed, not given up")
	tpls := e.templates(t, snap.ID)
	require.Len(t, tpls, 1)
	require.Equal(t, *e.App.Ent.Snapshot.GetX(ctx, snap.ID).Vmid, tpls[0].VMID)
	e.requireServer(t, s.ServerID, vmid, servers.StatusRunning)
}

func TestSnapshotShutdownTimeout(t *testing.T) {
	t.Run("without force the server keeps running", func(t *testing.T) {
		ctx := context.Background()
		e := newEnv(t)
		s, vmid := e.bootServer(t, "web1")
		e.App.PVE.HangShutdown = map[int]bool{vmid: true}
		snap, act := e.take(t, "web1", false)

		a := e.finish(t, e.opts(nil), act.ID)
		require.Equal(t, actions.StatusError, a.Status)
		require.Equal(t, actions.CodeShutdownTimeout, a.Error.Code)
		require.Equal(t, actions.Message(snapshots.KindCreateSnapshot, actions.CodeShutdownTimeout), a.Error.Message)
		require.Zero(t, e.countCalls(fmt.Sprintf("POST /nodes/pve1/qemu/%d/status/stop", vmid)))
		require.Zero(t, e.countCalls(fmt.Sprintf("POST /nodes/pve1/qemu/%d/status/start", vmid)))
		require.Equal(t, "1", e.App.PVE.ShutdownTimeout(vmid), "the shutdown task ends with the job's wait")
		e.requireServer(t, s.ServerID, vmid, servers.StatusRunning)
		require.False(t, e.App.Ent.Snapshot.Query().Where(snapshot.ID(snap.ID)).ExistX(ctx))
		require.Empty(t, e.templates(t, snap.ID))
		n, gb := e.usage(t)
		require.Zero(t, n)
		require.Zero(t, gb)
	})
	t.Run("a guest that powers off late is started again", func(t *testing.T) {
		ctx := context.Background()
		e := newEnv(t)
		s, vmid := e.bootServer(t, "web1")
		e.App.PVE.HangShutdown = map[int]bool{vmid: true}
		snap, act := e.take(t, "web1", false)
		o := e.opts(nil)
		o.ShutdownGrace = 10 * time.Second
		done := make(chan struct{})
		go func() {
			defer close(done)
			for e.App.PVE.ShutdownTimeout(vmid) == "" {
				time.Sleep(5 * time.Millisecond)
			}
			// Well past the job's shutdown wait, the guest acts on the request.
			time.Sleep(3 * o.ShutdownTimeout)
			e.App.PVE.SetVMStatus(vmid, "stopped")
		}()

		a := e.finish(t, o, act.ID)
		<-done
		require.Equal(t, actions.StatusError, a.Status)
		require.Equal(t, actions.CodeShutdownTimeout, a.Error.Code)
		require.Equal(t, 1, e.countCalls(fmt.Sprintf("POST /nodes/pve1/qemu/%d/status/start", vmid)))
		e.requireServer(t, s.ServerID, vmid, servers.StatusRunning)
		require.False(t, e.App.Ent.Snapshot.Query().Where(snapshot.ID(snap.ID)).ExistX(ctx))
	})
	t.Run("with force the server is stopped and started again", func(t *testing.T) {
		ctx := context.Background()
		e := newEnv(t)
		s, vmid := e.bootServer(t, "web1")
		e.App.PVE.HangShutdown = map[int]bool{vmid: true}
		snap, act := e.take(t, "web1", true)

		a := e.finish(t, e.opts(nil), act.ID)
		require.Equal(t, actions.StatusSuccess, a.Status, "%+v", a.Error)
		require.Equal(t, 1, e.countCalls(fmt.Sprintf("POST /nodes/pve1/qemu/%d/status/stop", vmid)))
		require.Equal(t, snapshot.StatusAvailable, e.App.Ent.Snapshot.GetX(ctx, snap.ID).Status)
		require.Len(t, e.templates(t, snap.ID), 1)
		e.requireServer(t, s.ServerID, vmid, servers.StatusRunning)
	})
}

func TestSnapshotCompensationRestartsServer(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	s, vmid := e.bootServer(t, "web1")
	pc, err := e.App.Catalog.ProxmoxFor(ctx, e.Location)
	require.NoError(t, err)
	next, err := pc.NextID(ctx)
	require.NoError(t, err)
	e.App.PVE.FailImport = map[int]error{next: errors.New("storage offline")}
	snap, act := e.take(t, "web1", false)
	n, gb := e.usage(t)
	require.Equal(t, 1, n)
	require.Equal(t, 10, gb)

	a := e.finish(t, e.opts(nil), act.ID)
	require.Equal(t, actions.StatusError, a.Status)
	require.Contains(t, a.Error.Detail, "storage offline")
	require.Equal(t, 1, e.countCalls(fmt.Sprintf("POST /nodes/pve1/qemu/%d/status/shutdown", vmid)))
	e.requireServer(t, s.ServerID, vmid, servers.StatusRunning)
	require.False(t, e.App.Ent.Snapshot.Query().Where(snapshot.ID(snap.ID)).ExistX(ctx))
	require.Empty(t, e.templates(t, snap.ID))
	_, ok := e.App.PVE.VM(next)
	require.False(t, ok)
	n, gb = e.usage(t)
	require.Zero(t, n)
	require.Zero(t, gb)
}

func TestSnapshotCompensationDestroysHalfMadeVM(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	s, vmid := e.bootServer(t, "web1")
	snap, act := e.take(t, "web1", false)
	o := snapshots.SetAfterStep(e.opts(nil), func(n int) {
		if n == 4 {
			// The template conversion fails, after the VM was made.
			e.App.PVE.FailNext("/template", 500)
		}
	})
	a := e.finish(t, o, act.ID)
	require.Equal(t, actions.StatusError, a.Status)
	require.Empty(t, e.templates(t, snap.ID), "our half-made VM is destroyed")
	e.requireServer(t, s.ServerID, vmid, servers.StatusRunning)
	require.False(t, e.App.Ent.Snapshot.Query().Where(snapshot.ID(snap.ID)).ExistX(ctx))
}

func TestSnapshotCompensationUnreachableKeepsRow(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	s, vmid := e.bootServer(t, "web1")
	snap, act := e.take(t, "web1", false)
	o := snapshots.SetAfterStep(e.opts(nil), func(n int) {
		if n == 3 {
			e.App.PVE.SetDown(true)
		}
	})
	// The last attempt: the create fails as unreachable, and so does the cleanup.
	_ = e.work(ctx, t, o, act.ID, servers.MaxAttempts)
	e.App.PVE.SetDown(false)
	a := e.action(t, act.ID)
	require.Equal(t, actions.StatusError, a.Status)
	row := e.App.Ent.Snapshot.GetX(ctx, snap.ID)
	require.Equal(t, snapshot.StatusError, row.Status)
	require.True(t, row.WasRunning, "kept so the cleanup can start the server later")
	n, gb := e.usage(t)
	require.Equal(t, 1, n, "quota kept")
	require.Equal(t, 10, gb)
	v, _ := e.App.PVE.VM(vmid)
	require.Equal(t, "stopped", v.Status)
	srv, err := e.App.Ent.Server.Get(ctx, s.ServerID)
	require.NoError(t, err)
	require.Equal(t, servers.StatusOff, string(srv.Status), "not left stopping or starting when its VM can't be read")
}

func TestSnapshotLostVMIDRace(t *testing.T) {
	t.Run("a VM appears at the vmid before the create", func(t *testing.T) {
		ctx := context.Background()
		e := newEnv(t)
		s, vmid := e.bootServer(t, "web1")
		snap, act := e.take(t, "web1", false)
		stolen := 0
		o := snapshots.SetAfterStep(e.opts(nil), func(n int) {
			if n == 3 && stolen == 0 {
				stolen = *e.App.Ent.Snapshot.GetX(ctx, snap.ID).Vmid
				e.App.PVE.AddVM("pve1", stolen, "other")
			}
		})
		a := e.finish(t, o, act.ID)
		require.Equal(t, actions.StatusSuccess, a.Status, "%+v", a.Error)
		row := e.App.Ent.Snapshot.GetX(ctx, snap.ID)
		require.NotEqual(t, stolen, *row.Vmid)
		other, ok := e.App.PVE.VM(stolen)
		require.True(t, ok)
		require.Equal(t, "other", other.Name)
		require.Len(t, e.templates(t, snap.ID), 1)
		require.Equal(t, 2, e.countCalls("GET /cluster/nextid"))
		e.requireServer(t, s.ServerID, vmid, servers.StatusRunning)
	})
	t.Run("the create task loses the vmid", func(t *testing.T) {
		ctx := context.Background()
		e := newEnv(t)
		s, vmid := e.bootServer(t, "web1")
		snap, act := e.take(t, "web1", false)
		stolen := 0
		done := make(chan struct{})
		o := snapshots.SetAfterStep(e.opts(nil), func(n int) {
			if n != 3 || stolen != 0 {
				return
			}
			stolen = *e.App.Ent.Snapshot.GetX(ctx, snap.ID).Vmid
			e.App.PVE.SetTaskDelay(time.Hour)
			go func() {
				defer close(done)
				for {
					r, err := e.App.Ent.Snapshot.Get(ctx, snap.ID)
					if err == nil && r.Upid != nil {
						break
					}
					time.Sleep(5 * time.Millisecond)
				}
				e.App.PVE.DeleteVM(stolen)
				e.App.PVE.AddVM("pve1", stolen, "other")
				e.App.PVE.SetVMConfig(stolen, "tags", "nebla;nb-srv-999")
				e.App.PVE.SetVMConfig(stolen, "lock", "clone")
				e.App.PVE.SetTaskDelay(0)
				e.App.PVE.FinishTasks()
			}()
		})
		a := e.finish(t, o, act.ID)
		<-done
		require.Equal(t, actions.StatusSuccess, a.Status, "%+v", a.Error)
		row := e.App.Ent.Snapshot.GetX(ctx, snap.ID)
		require.NotEqual(t, stolen, *row.Vmid)
		other, ok := e.App.PVE.VM(stolen)
		require.True(t, ok)
		require.Equal(t, "other", other.Name)
		require.Len(t, e.templates(t, snap.ID), 1)
		e.requireServer(t, s.ServerID, vmid, servers.StatusRunning)
	})
}

// available takes a snapshot of a new running server and finishes the job.
func (e *env) available(t *testing.T, name string) *snapshotRow {
	t.Helper()
	e.bootServer(t, name)
	snap, act := e.take(t, name, false)
	a := e.finish(t, e.opts(nil), act.ID)
	require.Equal(t, actions.StatusSuccess, a.Status, "%+v", a.Error)
	row := e.App.Ent.Snapshot.GetX(context.Background(), snap.ID)
	return &snapshotRow{ID: row.ID, VMID: *row.Vmid}
}

type snapshotRow struct {
	ID   int64
	VMID int
}

func TestDeleteSnapshotJob(t *testing.T) {
	t.Run("ours", func(t *testing.T) {
		ctx := context.Background()
		e := newEnv(t)
		r := e.available(t, "web1")
		act, err := e.svc.Delete(ctx, e.Project, r.ID)
		require.NoError(t, err)
		require.Equal(t, []actions.Resource{{ID: r.ID, Type: snapshots.ResourceType}}, act.Resources, "a snapshot taken cleanly never holds its server")
		rec := &recorder{}
		a := e.finish(t, e.opts(rec), act.ID)
		require.Equal(t, actions.StatusSuccess, a.Status, "%+v", a.Error)
		_, ok := e.App.PVE.VM(r.VMID)
		require.False(t, ok, "template destroyed")
		require.False(t, e.App.Ent.Snapshot.Query().Where(snapshot.ID(r.ID)).ExistX(ctx))
		n, gb := e.usage(t)
		require.Zero(t, n)
		require.Zero(t, gb)
		evs := rec.all()
		require.Len(t, evs, 1)
		require.Equal(t, ext.SnapshotDeleted, evs[0].Kind)
		require.Equal(t, r.ID, evs[0].ResourceID)
		require.Equal(t, map[string]any{"size_gb": 10, "location_id": e.Location}, evs[0].Data)
	})
	t.Run("already gone", func(t *testing.T) {
		ctx := context.Background()
		e := newEnv(t)
		r := e.available(t, "web1")
		e.App.PVE.DeleteVM(r.VMID)
		act, err := e.svc.Delete(ctx, e.Project, r.ID)
		require.NoError(t, err)
		a := e.finish(t, e.opts(nil), act.ID)
		require.Equal(t, actions.StatusSuccess, a.Status, "%+v", a.Error)
		require.False(t, e.App.Ent.Snapshot.Query().Where(snapshot.ID(r.ID)).ExistX(ctx))
	})
	t.Run("a foreign VM at the vmid", func(t *testing.T) {
		ctx := context.Background()
		e := newEnv(t)
		r := e.available(t, "web1")
		e.App.PVE.DeleteVM(r.VMID)
		e.App.PVE.AddVM("pve1", r.VMID, "other")
		act, err := e.svc.Delete(ctx, e.Project, r.ID)
		require.NoError(t, err)
		rec := &recorder{}
		a := e.finish(t, e.opts(rec), act.ID)
		require.Equal(t, actions.StatusError, a.Status)
		require.Contains(t, a.Error.Detail, snapTag(r.ID))
		other, ok := e.App.PVE.VM(r.VMID)
		require.True(t, ok, "never destroyed")
		require.Equal(t, "other", other.Name)
		require.Equal(t, snapshot.StatusMissing, e.App.Ent.Snapshot.GetX(ctx, r.ID).Status)
		require.Empty(t, rec.all())
		require.Zero(t, e.countCalls(fmt.Sprintf("DELETE /nodes/pve1/qemu/%d", r.VMID)))
	})
}

// errorSnapshot is an available snapshot turned into a failed create's leftover: error, with its server left off.
func (e *env) errorSnapshot(t *testing.T, wasRunning bool) (*snapshotRow, int64, int) {
	t.Helper()
	ctx := context.Background()
	s, vmid := e.bootServer(t, "web1")
	snap, act := e.take(t, "web1", false)
	a := e.finish(t, e.opts(nil), act.ID)
	require.Equal(t, actions.StatusSuccess, a.Status, "%+v", a.Error)
	row := e.App.Ent.Snapshot.UpdateOneID(snap.ID).SetStatus(snapshot.StatusError).SetWasRunning(wasRunning).SaveX(ctx)
	e.App.PVE.SetVMStatus(vmid, "stopped")
	require.NoError(t, e.App.Ent.Server.UpdateOneID(s.ServerID).SetStatus(server.StatusOff).Exec(ctx))
	return &snapshotRow{ID: row.ID, VMID: *row.Vmid}, s.ServerID, vmid
}

func TestDeleteErrorSnapshotRestartsServer(t *testing.T) {
	t.Run("the server starts, then the snapshot goes", func(t *testing.T) {
		ctx := context.Background()
		e := newEnv(t)
		r, srvID, vmid := e.errorSnapshot(t, true)
		act, err := e.svc.Delete(ctx, e.Project, r.ID)
		require.NoError(t, err)
		a := e.finish(t, e.opts(nil), act.ID)
		require.Equal(t, actions.StatusSuccess, a.Status, "%+v", a.Error)
		e.requireServer(t, srvID, vmid, servers.StatusRunning)
		require.False(t, e.App.Ent.Snapshot.Query().Where(snapshot.ID(r.ID)).ExistX(ctx))
		_, ok := e.App.PVE.VM(r.VMID)
		require.False(t, ok)
	})
	t.Run("a failed start stops the delete", func(t *testing.T) {
		ctx := context.Background()
		e := newEnv(t)
		r, srvID, vmid := e.errorSnapshot(t, true)
		act, err := e.svc.Delete(ctx, e.Project, r.ID)
		require.NoError(t, err)
		e.App.PVE.FailNext("/status/start", 500)
		a := e.finish(t, e.opts(nil), act.ID)
		require.Equal(t, actions.StatusError, a.Status)
		e.requireServer(t, srvID, vmid, servers.StatusOff)
		row := e.App.Ent.Snapshot.GetX(ctx, r.ID)
		require.Equal(t, snapshot.StatusError, row.Status)
		require.True(t, row.WasRunning, "kept for the next try")
		_, ok := e.App.PVE.VM(r.VMID)
		require.True(t, ok, "the template is kept while the server is off")
	})
	t.Run("a server that was off stays off", func(t *testing.T) {
		ctx := context.Background()
		e := newEnv(t)
		r, srvID, vmid := e.errorSnapshot(t, false)
		start := fmt.Sprintf("POST /nodes/pve1/qemu/%d/status/start", vmid)
		before := e.countCalls(start)
		act, err := e.svc.Delete(ctx, e.Project, r.ID)
		require.NoError(t, err)
		a := e.finish(t, e.opts(nil), act.ID)
		require.Equal(t, actions.StatusSuccess, a.Status, "%+v", a.Error)
		e.requireServer(t, srvID, vmid, servers.StatusOff)
		require.Equal(t, before, e.countCalls(start), "no start")
	})
}

func TestDeleteFailureRestoresStatus(t *testing.T) {
	for _, st := range []snapshot.Status{snapshot.StatusError, snapshot.StatusMissing, snapshot.StatusAvailable} {
		t.Run(string(st), func(t *testing.T) {
			ctx := context.Background()
			e := newEnv(t)
			r := e.available(t, "web1")
			e.App.Ent.Snapshot.UpdateOneID(r.ID).SetStatus(st).ExecX(ctx)
			act, err := e.svc.Delete(ctx, e.Project, r.ID)
			require.NoError(t, err)
			e.App.PVE.SetDown(true)
			_ = e.work(ctx, t, e.opts(nil), act.ID, servers.MaxAttempts)
			e.App.PVE.SetDown(false)
			require.Equal(t, actions.StatusError, e.action(t, act.ID).Status)
			require.Equal(t, st, e.App.Ent.Snapshot.GetX(ctx, r.ID).Status)
		})
	}
}

func TestDeleteErrorSnapshotLocksServer(t *testing.T) {
	t.Run("the action holds the server", func(t *testing.T) {
		ctx := context.Background()
		e := newEnv(t)
		r, srvID, _ := e.errorSnapshot(t, true)
		act, err := e.svc.Delete(ctx, e.Project, r.ID)
		require.NoError(t, err)
		require.Equal(t, []actions.Resource{{ID: srvID, Type: servers.ResourceType}, {ID: r.ID, Type: snapshots.ResourceType}}, act.Resources)
		e.requireJob(t, act, "delete_snapshot", r.ID, `"prev_status":"error"`)
	})
	t.Run("a running server action refuses the delete", func(t *testing.T) {
		ctx := context.Background()
		e := newEnv(t)
		r, srvID, _ := e.errorSnapshot(t, true)
		_, err := actions.Create(ctx, e.App.Ent, e.Project, "start_server", []actions.Resource{{ID: srvID, Type: servers.ResourceType}})
		require.NoError(t, err)
		_, err = e.svc.Delete(ctx, e.Project, r.ID)
		requireAPIErr(t, err, 409, "locked")
		require.Equal(t, snapshot.StatusError, e.App.Ent.Snapshot.GetX(ctx, r.ID).Status)
	})
	t.Run("without a restart only the snapshot is held", func(t *testing.T) {
		ctx := context.Background()
		e := newEnv(t)
		r, _, _ := e.errorSnapshot(t, false)
		act, err := e.svc.Delete(ctx, e.Project, r.ID)
		require.NoError(t, err)
		require.Equal(t, []actions.Resource{{ID: r.ID, Type: snapshots.ResourceType}}, act.Resources)
	})
}

// serverAction records an action on the server; finished ones stand for something the user did.
func (e *env) serverAction(t *testing.T, srvID int64, command string, finished bool) {
	t.Helper()
	ctx := context.Background()
	a, err := actions.Create(ctx, e.App.Ent, e.Project, command, []actions.Resource{{ID: srvID, Type: servers.ResourceType}})
	require.NoError(t, err)
	if finished {
		require.NoError(t, actions.Succeed(ctx, e.App.Ent, a.ID))
	}
}

func (e *env) retryCleanup(t *testing.T, id int64) {
	t.Helper()
	ctx := context.Background()
	pc, err := e.App.Catalog.ProxmoxFor(ctx, e.Location)
	require.NoError(t, err)
	require.NoError(t, snapshots.RetryCleanup(ctx, e.opts(nil), e.App.Ent.Snapshot.GetX(ctx, id), pc))
}

func TestRetryCleanupRestart(t *testing.T) {
	t.Run("the server starts, then the row goes", func(t *testing.T) {
		ctx := context.Background()
		e := newEnv(t)
		r, srvID, vmid := e.errorSnapshot(t, true)
		e.retryCleanup(t, r.ID)
		e.requireServer(t, srvID, vmid, servers.StatusRunning)
		require.False(t, e.App.Ent.Snapshot.Query().Where(snapshot.ID(r.ID)).ExistX(ctx))
	})
	t.Run("the user's own stop since is respected", func(t *testing.T) {
		ctx := context.Background()
		e := newEnv(t)
		r, srvID, vmid := e.errorSnapshot(t, true)
		e.serverAction(t, srvID, "poweroff_server", true)
		start := fmt.Sprintf("POST /nodes/pve1/qemu/%d/status/start", vmid)
		before := e.countCalls(start)
		e.retryCleanup(t, r.ID)
		require.Equal(t, before, e.countCalls(start), "no start")
		e.requireServer(t, srvID, vmid, servers.StatusOff)
		require.False(t, e.App.Ent.Snapshot.Query().Where(snapshot.ID(r.ID)).ExistX(ctx), "cleanup went on")
		_, ok := e.App.PVE.VM(r.VMID)
		require.False(t, ok)
	})
	for _, cmd := range []string{"rebuild_server", "delete_server"} {
		t.Run("a running "+cmd+" blocks the retry", func(t *testing.T) {
			ctx := context.Background()
			e := newEnv(t)
			r, srvID, vmid := e.errorSnapshot(t, true)
			e.serverAction(t, srvID, cmd, false)
			e.retryCleanup(t, r.ID)
			e.requireServer(t, srvID, vmid, servers.StatusOff)
			row := e.App.Ent.Snapshot.GetX(ctx, r.ID)
			require.Equal(t, snapshot.StatusError, row.Status)
			require.True(t, row.WasRunning)
			_, ok := e.App.PVE.VM(r.VMID)
			require.True(t, ok, "nothing touched")
		})
	}
}

func TestDeleteErrorSnapshotRespectsUser(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	r, srvID, vmid := e.errorSnapshot(t, true)
	e.serverAction(t, srvID, "poweroff_server", true)
	start := fmt.Sprintf("POST /nodes/pve1/qemu/%d/status/start", vmid)
	before := e.countCalls(start)
	act, err := e.svc.Delete(ctx, e.Project, r.ID)
	require.NoError(t, err)
	a := e.finish(t, e.opts(nil), act.ID)
	require.Equal(t, actions.StatusSuccess, a.Status, "%+v", a.Error)
	require.Equal(t, before, e.countCalls(start), "no start")
	e.requireServer(t, srvID, vmid, servers.StatusOff)
	require.False(t, e.App.Ent.Snapshot.Query().Where(snapshot.ID(r.ID)).ExistX(ctx))
}

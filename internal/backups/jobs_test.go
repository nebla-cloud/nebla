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

package backups_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/backups"
	"github.com/nebla-cloud/nebla/internal/ent"
	entaction "github.com/nebla-cloud/nebla/internal/ent/action"
	entbackup "github.com/nebla-cloud/nebla/internal/ent/backup"
	"github.com/nebla-cloud/nebla/internal/ent/server"
	"github.com/nebla-cloud/nebla/internal/ext"
	"github.com/nebla-cloud/nebla/internal/jobs"
	"github.com/nebla-cloud/nebla/internal/pve"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/riverqueue/river"
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

func (r *recorder) of(kind ext.EventKind) []ext.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []ext.Event
	for _, e := range r.evs {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

type jenv struct {
	*env
	rec *recorder
	o   backups.WorkerOptions
}

// newJobEnv is newEnv with PBS storage "pbs" on both nodes and backup worker options.
func newJobEnv(t *testing.T) *jenv {
	t.Helper()
	e := newEnv(t)
	for _, n := range []string{"pve1", "pve2"} {
		e.App.PVE.AddStorage(n, "pbs", []string{"backup"}, true)
	}
	rec := &recorder{}
	o := backups.WorkerOptions{SQL: e.App.SQL, Ent: e.App.Ent, Catalog: e.App.Catalog, Bus: rec, Logger: quiet(),
		Jobs: func() *jobs.Jobs { return e.App.Jobs }}
	return &jenv{env: e, rec: rec, o: o}
}

func (e *jenv) queue(t *testing.T, serverID int64) int64 {
	t.Helper()
	b, created, err := e.svc.BackUpNow(context.Background(), e.Project, serverID)
	require.NoError(t, err)
	require.True(t, created)
	return b.ID
}

// jobFor is the newest create_backup job of the backup, as River would hand it to the worker.
func (e *jenv) jobFor(t *testing.T, backupID int64) *river.Job[backups.CreateBackupArgs] {
	t.Helper()
	var id int64
	require.NoError(t, e.App.SQL.QueryRow(`select id from river_job where kind = 'create_backup'
		and (args->>'backup_id')::bigint = $1 order by id desc limit 1`, backupID).Scan(&id))
	row, err := e.App.Jobs.Client.JobGet(context.Background(), id)
	require.NoError(t, err)
	row.Attempt = 1
	return &river.Job[backups.CreateBackupArgs]{JobRow: row, Args: backups.CreateBackupArgs{BackupID: backupID}}
}

func (e *jenv) work(t *testing.T, backupID int64) error {
	t.Helper()
	return backups.NewCreateBackupWorker(e.o).Work(context.Background(), e.jobFor(t, backupID))
}

// backUp queues and runs a backup of the server to completion.
func (e *jenv) backUp(t *testing.T, serverID int64) *ent.Backup {
	t.Helper()
	id := e.queue(t, serverID)
	require.NoError(t, e.work(t, id))
	b := e.App.Ent.Backup.GetX(context.Background(), id)
	require.Equal(t, entbackup.StatusAvailable, b.Status)
	return b
}

func requireSnooze(t *testing.T, err error) {
	t.Helper()
	var s *river.JobSnoozeError
	require.ErrorAs(t, err, &s)
	require.Equal(t, time.Minute, s.Duration)
}

func (e *jenv) pc(t *testing.T) pve.Client {
	t.Helper()
	pc, err := e.App.Catalog.ProxmoxFor(context.Background(), e.Location)
	require.NoError(t, err)
	return pc
}

func (e *jenv) archives(t *testing.T) []pve.BackupItem {
	t.Helper()
	items, err := e.pc(t).BackupContent(context.Background(), "pve1", "pbs", 0)
	require.NoError(t, err)
	return items
}

func notesOf(items []pve.BackupItem) []string {
	out := []string{}
	for _, it := range items {
		out = append(out, it.Notes)
	}
	slices.Sort(out)
	return out
}

func (e *jenv) vzdumps() int {
	n := 0
	for _, c := range e.App.PVE.Calls() {
		if strings.HasPrefix(c, "POST ") && strings.HasSuffix(c, "/vzdump") {
			n++
		}
	}
	return n
}

func (e *jenv) backupAction(t *testing.T, serverID int64) *ent.Action {
	t.Helper()
	a, err := e.App.Ent.Action.Query().Where(servers.ActionsOf(e.Project, serverID), entaction.Command(backups.KindCreateBackup)).
		Order(ent.Desc(entaction.FieldID)).First(context.Background())
	require.NoError(t, err)
	return a
}

func (e *jenv) noBackupAction(t *testing.T, serverID int64) {
	t.Helper()
	n, err := e.App.Ent.Action.Query().Where(servers.ActionsOf(e.Project, serverID), entaction.Command(backups.KindCreateBackup)).
		Count(context.Background())
	require.NoError(t, err)
	require.Zero(t, n)
}

func (e *jenv) slotFree(t *testing.T, node string) {
	t.Helper()
	release, ok, err := backups.NodeSlot(context.Background(), e.App.SQL, e.Location, node)
	require.NoError(t, err)
	require.True(t, ok, "slot on %s is still held", node)
	release()
}

// onNode moves the server's VM to node.
func (e *jenv) onNode(t *testing.T, serverID int64, node string) {
	t.Helper()
	row := e.App.Ent.Server.GetX(context.Background(), serverID)
	e.App.PVE.MoveVM(*row.Vmid, node)
	require.NoError(t, e.App.Ent.Server.UpdateOneID(serverID).SetNode(node).Exec(context.Background()))
}

func TestNodeSlotOneAtATime(t *testing.T) {
	ctx := context.Background()
	e := newJobEnv(t)

	r1, ok, err := backups.NodeSlot(ctx, e.App.SQL, e.Location, "pve1")
	require.NoError(t, err)
	require.True(t, ok)
	_, ok, err = backups.NodeSlot(ctx, e.App.SQL, e.Location, "pve1")
	require.NoError(t, err)
	require.False(t, ok)
	e.slotFree(t, "pve2")

	a := e.running(t, "a")
	c := e.running(t, "c")
	e.onNode(t, c, "pve2")
	ba, bc := e.queue(t, a), e.queue(t, c)
	// A backup on pve1 waits for the running one; pve2 runs.
	requireSnooze(t, e.work(t, ba))
	require.Equal(t, entbackup.StatusQueued, e.App.Ent.Backup.GetX(ctx, ba).Status)
	e.noBackupAction(t, a)
	require.NoError(t, e.work(t, bc))
	require.Equal(t, entbackup.StatusAvailable, e.App.Ent.Backup.GetX(ctx, bc).Status)

	r1()
	r1()
	require.NoError(t, e.work(t, ba))
	require.Equal(t, entbackup.StatusAvailable, e.App.Ent.Backup.GetX(ctx, ba).Status)
	e.slotFree(t, "pve1")
}

func TestNodeSlotReleasedOnCancel(t *testing.T) {
	e := newJobEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	release, ok, err := backups.NodeSlot(ctx, e.App.SQL, e.Location, "pve1")
	require.NoError(t, err)
	require.True(t, ok)
	cancel()
	release()
	e.slotFree(t, "pve1")
}

func TestFairOrderOldestFirst(t *testing.T) {
	ctx := context.Background()
	e := newJobEnv(t)
	a, b := e.running(t, "a"), e.running(t, "b")
	ba := e.queue(t, a)
	bb := e.queue(t, b)
	requireSnooze(t, e.work(t, bb))
	e.noBackupAction(t, b)
	e.slotFree(t, "pve1")
	require.NoError(t, e.work(t, ba))
	require.NoError(t, e.work(t, bb))
	require.Equal(t, entbackup.StatusAvailable, e.App.Ent.Backup.GetX(ctx, bb).Status)
}

func TestQueuedBackupDoesNotLockServer(t *testing.T) {
	ctx := context.Background()
	e := newJobEnv(t)
	id := e.running(t, "a")
	bid := e.queue(t, id)

	act, err := e.srv.Power(ctx, e.Project, id, pve.PowerReboot)
	require.NoError(t, err, "a queued backup leaves the server free")
	require.NoError(t, actions.Succeed(ctx, e.App.Ent, act.ID))
	require.NoError(t, e.App.Ent.Server.UpdateOneID(id).SetStatus(server.StatusRunning).Exec(ctx))

	e.App.PVE.SetTaskDelay(time.Hour)
	done := make(chan error, 1)
	go func() { done <- backups.NewCreateBackupWorker(e.o).Work(ctx, e.jobFor(t, bid)) }()
	require.Eventually(t, func() bool {
		return e.App.Ent.Backup.GetX(ctx, bid).Status == entbackup.StatusCreating && e.vzdumps() == 1
	}, 10*time.Second, 10*time.Millisecond)
	_, err = e.srv.Power(ctx, e.Project, id, pve.PowerReboot)
	requireAPIErr(t, err, http.StatusConflict, "backup_in_progress")

	e.App.PVE.SetTaskDelay(0)
	e.App.PVE.FinishTasks()
	require.NoError(t, <-done)
	require.Equal(t, entbackup.StatusAvailable, e.App.Ent.Backup.GetX(ctx, bid).Status)
	require.Equal(t, entaction.StatusSuccess, e.backupAction(t, id).Status)
	_, err = e.srv.Power(ctx, e.Project, id, pve.PowerReboot)
	require.NoError(t, err)
}

func TestCreateBackupSnoozesWhenServerBusy(t *testing.T) {
	ctx := context.Background()
	e := newJobEnv(t)
	id := e.running(t, "a")
	bid := e.queue(t, id)
	act, err := e.srv.Power(ctx, e.Project, id, pve.PowerReboot)
	require.NoError(t, err)

	requireSnooze(t, e.work(t, bid))
	require.Equal(t, entbackup.StatusQueued, e.App.Ent.Backup.GetX(ctx, bid).Status)
	e.noBackupAction(t, id)
	e.slotFree(t, "pve1")
	require.Zero(t, e.vzdumps())

	require.NoError(t, actions.Succeed(ctx, e.App.Ent, act.ID))
	require.NoError(t, e.work(t, bid))
	require.Equal(t, entbackup.StatusAvailable, e.App.Ent.Backup.GetX(ctx, bid).Status)
}

func TestCreateBackupRecordsIdentity(t *testing.T) {
	ctx := context.Background()
	e := newJobEnv(t)
	id := e.running(t, "a")
	b := e.backUp(t, id)
	row := e.App.Ent.Server.GetX(ctx, id)
	fp, err := servers.NetworkFingerprint(ctx, e.App.Ent, row)
	require.NoError(t, err)
	require.Equal(t, fp, b.NetworkFingerprint)
	require.Equal(t, servers.InstanceGeneration(row), b.InstanceGeneration)
	require.Equal(t, "pve1", b.Node)
	require.Equal(t, "pbs", b.Storage)
	require.NotNil(t, b.Volid)
	require.NotNil(t, b.CompletedAt)
	require.EqualValues(t, 1<<30, b.SizeBytes)
	require.Equal(t, entaction.StatusSuccess, e.backupAction(t, id).Status)

	items := e.archives(t)
	require.Len(t, items, 1)
	require.Equal(t, *b.Volid, items[0].Volid)
	require.Equal(t, fmt.Sprintf("nebla server %d backup %d", id, b.ID), items[0].Notes)
	require.Equal(t, backups.Notes(id, b.ID), items[0].Notes)

	evs := e.rec.of(ext.BackupCreated)
	require.Len(t, evs, 1)
	require.Equal(t, b.ID, evs[0].ResourceID)
	require.Equal(t, backups.ResourceType, evs[0].ResourceType)
	require.Zero(t, evs[0].ServerID)
	require.Equal(t, map[string]any{"server_id": id, "size_bytes": int64(1 << 30), "kind": "manual", "location_id": e.Location},
		evs[0].Data)
}

func TestCreateBackupGoneIsNoop(t *testing.T) {
	ctx := context.Background()
	e := newJobEnv(t)
	id := e.running(t, "a")
	bid := e.queue(t, id)
	job := e.jobFor(t, bid)
	require.NoError(t, e.App.Ent.Backup.DeleteOneID(bid).Exec(ctx))
	require.NoError(t, backups.NewCreateBackupWorker(e.o).Work(ctx, job))
	require.Zero(t, e.vzdumps())

	// A server that is being deleted or rebuilt drops its queued backup.
	for _, st := range []server.Status{server.StatusDeleting, server.StatusRebuilding} {
		require.NoError(t, e.App.Ent.Server.UpdateOneID(id).SetStatus(server.StatusRunning).Exec(ctx))
		bid := e.queue(t, id)
		require.NoError(t, e.App.Ent.Server.UpdateOneID(id).SetStatus(st).Exec(ctx))
		require.NoError(t, e.work(t, bid))
		_, err := e.App.Ent.Backup.Get(ctx, bid)
		require.True(t, ent.IsNotFound(err), "%s: %v", st, err)
		e.noBackupAction(t, id)
	}
	require.Zero(t, e.vzdumps())
	e.slotFree(t, "pve1")
}

// crashAfterVzdump fails the first WaitTask after the task finished, as if the worker died there.
type crashAfterVzdump struct {
	pve.Client
	crashed atomic.Bool
}

func (c *crashAfterVzdump) WaitTask(ctx context.Context, node string, upid pve.UPID) error {
	if err := c.Client.WaitTask(ctx, node, upid); err != nil {
		return err
	}
	if c.crashed.CompareAndSwap(false, true) {
		return &pve.Error{Status: 503, Message: "connection lost"}
	}
	return nil
}

func TestCreateBackupResumesAfterVzdump(t *testing.T) {
	ctx := context.Background()
	e := newJobEnv(t)
	id := e.running(t, "a")
	bid := e.queue(t, id)
	pc := &crashAfterVzdump{Client: e.pc(t)}
	o := backups.SetPVE(e.o, func(context.Context, int64) (pve.Client, error) { return pc, nil })
	w := backups.NewCreateBackupWorker(o)

	job := e.jobFor(t, bid)
	err := w.Work(ctx, job)
	require.Error(t, err)
	var cancel *river.JobCancelError
	require.NotErrorAs(t, err, &cancel)
	require.Equal(t, entbackup.StatusCreating, e.App.Ent.Backup.GetX(ctx, bid).Status)
	require.Equal(t, 1, e.vzdumps())
	e.slotFree(t, "pve1")

	job = e.jobFor(t, bid)
	job.Attempt = 2
	require.NoError(t, w.Work(ctx, job))
	b := e.App.Ent.Backup.GetX(ctx, bid)
	require.Equal(t, entbackup.StatusAvailable, b.Status)
	require.Equal(t, 1, e.vzdumps(), "the retry found the archive by its notes")
	items := e.archives(t)
	require.Len(t, items, 1)
	require.Equal(t, *b.Volid, items[0].Volid)
	require.Equal(t, entaction.StatusSuccess, e.backupAction(t, id).Status)
	n, err := e.App.Ent.Action.Query().Where(entaction.Command(backups.KindCreateBackup)).Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n, "the retry reuses the action")
	require.Len(t, e.rec.of(ext.BackupCreated), 1)
}

// failResolve fails the content listing that follows the vzdump with a permanent error.
type failResolve struct {
	pve.Client
	calls atomic.Int32
}

func (c *failResolve) BackupContent(ctx context.Context, node, storage string, vmid int) ([]pve.BackupItem, error) {
	if c.calls.Add(1) == 2 {
		return nil, &pve.Error{Status: 400, Message: "bad request"}
	}
	return c.Client.BackupContent(ctx, node, storage, vmid)
}

func TestCreateBackupFailureRemovesRow(t *testing.T) {
	ctx := context.Background()
	e := newJobEnv(t)
	id := e.running(t, "a")
	bid := e.queue(t, id)
	e.App.PVE.FailNext("/vzdump", 500)
	err := e.work(t, bid)
	var cancel *river.JobCancelError
	require.ErrorAs(t, err, &cancel)
	_, err = e.App.Ent.Backup.Get(ctx, bid)
	require.True(t, ent.IsNotFound(err))
	act := e.backupAction(t, id)
	require.Equal(t, entaction.StatusError, act.Status)
	require.Empty(t, e.archives(t))
	e.slotFree(t, "pve1")

	// An archive written before the failure is deleted with the row.
	bid = e.queue(t, id)
	pc := &failResolve{Client: e.pc(t)}
	w := backups.NewCreateBackupWorker(backups.SetPVE(e.o, func(context.Context, int64) (pve.Client, error) { return pc, nil }))
	err = w.Work(ctx, e.jobFor(t, bid))
	require.ErrorAs(t, err, &cancel)
	_, err = e.App.Ent.Backup.Get(ctx, bid)
	require.True(t, ent.IsNotFound(err))
	require.Equal(t, 2, e.vzdumps())
	require.Empty(t, e.archives(t))
	require.Empty(t, e.rec.of(ext.BackupCreated))
}

func (e *jenv) keep(t *testing.T, serverID int64, keep int) {
	t.Helper()
	_, err := e.srv.Update(context.Background(), e.Project, serverID, servers.UpdateInput{Backups: &servers.BackupSettings{Enabled: true, Keep: keep}})
	require.NoError(t, err)
}

func TestPruneOnlyOwnRows(t *testing.T) {
	ctx := context.Background()
	e := newJobEnv(t)
	id := e.running(t, "a")
	e.keep(t, id, 1)
	vmid := *e.App.Ent.Server.GetX(ctx, id).Vmid
	pc := e.pc(t)
	// The admin's own backup in the same VMID group, and a Nebla-notes archive no row records.
	foreign := []string{"admin nightly", fmt.Sprintf("nebla server %d backup 999999", id)}
	for _, n := range foreign {
		upid, err := pc.Vzdump(ctx, "pve1", vmid, "pbs", n)
		require.NoError(t, err)
		require.NoError(t, pc.WaitTask(ctx, "pve1", upid))
	}
	first := e.backUp(t, id)
	second := e.backUp(t, id)

	_, err := e.App.Ent.Backup.Get(ctx, first.ID)
	require.True(t, ent.IsNotFound(err), "the older own backup was pruned")
	want := append(slices.Clone(foreign), backups.Notes(id, second.ID))
	slices.Sort(want)
	require.Equal(t, want, notesOf(e.archives(t)))
	require.Empty(t, slices.DeleteFunc(e.App.PVE.Calls(), func(c string) bool { return !strings.Contains(c, "prunebackups") }))
}

func TestPruneKeepsN(t *testing.T) {
	ctx := context.Background()
	e := newJobEnv(t)
	id := e.running(t, "a")
	e.keep(t, id, 2)
	var made []*ent.Backup
	for range 3 {
		made = append(made, e.backUp(t, id))
	}
	rows, err := e.App.Ent.Backup.Query().Where(entbackup.ServerID(id)).Order(ent.Asc(entbackup.FieldID)).All(ctx)
	require.NoError(t, err)
	require.Equal(t, []int64{made[1].ID, made[2].ID}, []int64{rows[0].ID, rows[1].ID})
	require.Len(t, rows, 2)
	require.Len(t, e.archives(t), 2)
	del := e.rec.of(ext.BackupDeleted)
	require.Len(t, del, 1)
	require.Equal(t, made[0].ID, del[0].ResourceID)
	require.Equal(t, map[string]any{"server_id": id, "size_bytes": int64(1 << 30), "kind": "manual", "location_id": e.Location}, del[0].Data)

	// A row whose archive is already gone still goes.
	require.NoError(t, e.App.Ent.Backup.UpdateOneID(made[1].ID).SetVolid("pbs:backup/vm/1/gone").Exec(ctx))
	e.backUp(t, id)
	_, err = e.App.Ent.Backup.Get(ctx, made[1].ID)
	require.True(t, ent.IsNotFound(err))
}

func deleteJob(t *testing.T, e *jenv, act actions.Action) *river.Job[backups.DeleteBackupArgs] {
	t.Helper()
	row, err := e.App.Jobs.Client.JobGet(context.Background(), *act.RiverJobID)
	require.NoError(t, err)
	var args backups.DeleteBackupArgs
	require.NoError(t, json.Unmarshal(row.EncodedArgs, &args))
	row.Attempt = 1
	return &river.Job[backups.DeleteBackupArgs]{JobRow: row, Args: args}
}

func TestDeleteBackupJob(t *testing.T) {
	ctx := context.Background()
	e := newJobEnv(t)
	id := e.running(t, "a")
	b := e.backUp(t, id)
	w := backups.NewDeleteBackupWorker(e.o)

	act, err := e.svc.Delete(ctx, e.Project, id, b.ID)
	require.NoError(t, err)
	require.NoError(t, w.Work(ctx, deleteJob(t, e, act)))
	_, err = e.App.Ent.Backup.Get(ctx, b.ID)
	require.True(t, ent.IsNotFound(err))
	require.Empty(t, e.archives(t))
	require.Equal(t, entaction.StatusSuccess, e.App.Ent.Action.GetX(ctx, act.ID).Status)
	require.Len(t, e.rec.of(ext.BackupDeleted), 1)

	// An archive that is already gone counts as deleted.
	gone := e.put(t, id, entbackup.StatusAvailable)
	act, err = e.svc.Delete(ctx, e.Project, id, gone.ID)
	require.NoError(t, err)
	require.NoError(t, w.Work(ctx, deleteJob(t, e, act)))
	_, err = e.App.Ent.Backup.Get(ctx, gone.ID)
	require.True(t, ent.IsNotFound(err))
	require.Equal(t, entaction.StatusSuccess, e.App.Ent.Action.GetX(ctx, act.ID).Status)

	// A failed delete puts the backup back.
	kept := e.backUp(t, id)
	act, err = e.svc.Delete(ctx, e.Project, id, kept.ID)
	require.NoError(t, err)
	e.App.PVE.FailNext("/content/"+*kept.Volid, 400)
	err = w.Work(ctx, deleteJob(t, e, act))
	var cancel *river.JobCancelError
	require.ErrorAs(t, err, &cancel)
	require.Equal(t, entbackup.StatusAvailable, e.App.Ent.Backup.GetX(ctx, kept.ID).Status)
	require.Equal(t, entaction.StatusError, e.App.Ent.Action.GetX(ctx, act.ID).Status)
	require.Len(t, e.archives(t), 1)

	// A missing backup whose delete fails stays missing.
	missing := e.put(t, id, entbackup.StatusMissing)
	act, err = e.svc.Delete(ctx, e.Project, id, missing.ID)
	require.NoError(t, err)
	e.App.PVE.FailNext("/content/"+*missing.Volid, 400)
	require.ErrorAs(t, w.Work(ctx, deleteJob(t, e, act)), &cancel)
	require.Equal(t, entbackup.StatusMissing, e.App.Ent.Backup.GetX(ctx, missing.ID).Status)
}

func TestDeleteBackupOnOldStorage(t *testing.T) {
	ctx := context.Background()
	e := newJobEnv(t)
	id := e.running(t, "a")
	b := e.backUp(t, id)
	for _, n := range []string{"pve1", "pve2"} {
		e.App.PVE.AddStorage(n, "pbs2", []string{"backup"}, true)
	}
	require.NoError(t, e.App.Ent.Location.UpdateOneID(e.Location).SetBackupStorage("pbs2").Exec(ctx))

	act, err := e.svc.Delete(ctx, e.Project, id, b.ID)
	require.NoError(t, err)
	require.NoError(t, backups.NewDeleteBackupWorker(e.o).Work(ctx, deleteJob(t, e, act)))
	require.False(t, e.App.Ent.Backup.Query().Where(entbackup.ID(b.ID)).ExistX(ctx))
	require.Empty(t, e.archives(t), "deleted from the storage it was written to")
	require.Equal(t, entaction.StatusSuccess, e.App.Ent.Action.GetX(ctx, act.ID).Status)
}

func TestDeadJobDoesNotBlockNode(t *testing.T) {
	ctx := context.Background()
	e := newJobEnv(t)
	a, b := e.running(t, "a"), e.running(t, "b")
	ba := e.queue(t, a)
	_, err := e.App.SQL.Exec(`update river_job set state = 'discarded', finalized_at = now() where kind = 'create_backup'
		and (args->>'backup_id')::bigint = $1`, ba)
	require.NoError(t, err)
	bb := e.queue(t, b)
	require.NoError(t, e.work(t, bb), "an older row whose job died does not hold the line")
	require.Equal(t, entbackup.StatusAvailable, e.App.Ent.Backup.GetX(ctx, bb).Status)

	// The scheduler pass gives the row a new job.
	e.schedule(t, at(23, 0, 0))
	require.Equal(t, 2, e.createJobs(t, ba))
	require.NoError(t, e.work(t, ba))
	require.Equal(t, entbackup.StatusAvailable, e.App.Ent.Backup.GetX(ctx, ba).Status)
}

func TestCreateBackupFinalAttemptDrops(t *testing.T) {
	ctx := context.Background()
	e := newJobEnv(t)
	id := e.running(t, "a")
	w := backups.NewCreateBackupWorker(backups.SetBeforeStart(e.o, func() error { return errors.New("boom") }))

	bid := e.queue(t, id)
	job := e.jobFor(t, bid)
	err := w.Work(ctx, job)
	require.EqualError(t, err, "boom")
	require.Equal(t, entbackup.StatusQueued, e.App.Ent.Backup.GetX(ctx, bid).Status)
	job.Attempt = servers.MaxAttempts
	err = w.Work(ctx, job)
	var cancel *river.JobCancelError
	require.ErrorAs(t, err, &cancel)
	_, err = e.App.Ent.Backup.Get(ctx, bid)
	require.True(t, ent.IsNotFound(err))
	e.noBackupAction(t, id)

	// A started backup also fails its action.
	bid = e.queue(t, id)
	job = e.jobFor(t, bid)
	act, err := actions.Create(ctx, e.App.Ent, e.Project, backups.KindCreateBackup, []actions.Resource{{ID: id, Type: servers.ResourceType}})
	require.NoError(t, err)
	require.NoError(t, actions.AttachJob(ctx, e.App.Ent, act.ID, job.ID))
	require.NoError(t, e.App.Ent.Backup.UpdateOneID(bid).SetStatus(entbackup.StatusCreating).Exec(ctx))
	job.Attempt = servers.MaxAttempts
	require.ErrorAs(t, w.Work(ctx, job), &cancel)
	_, err = e.App.Ent.Backup.Get(ctx, bid)
	require.True(t, ent.IsNotFound(err))
	require.Equal(t, entaction.StatusError, e.App.Ent.Action.GetX(ctx, act.ID).Status)
	e.slotFree(t, "pve1")
}

func TestCreateBackupDuplicateRun(t *testing.T) {
	ctx := context.Background()
	e := newJobEnv(t)
	id := e.running(t, "a")
	bid := e.queue(t, id)
	w := backups.NewCreateBackupWorker(e.o)

	e.App.PVE.SetTaskDelay(time.Hour)
	done := make(chan error, 1)
	go func() { done <- w.Work(ctx, e.jobFor(t, bid)) }()
	require.Eventually(t, func() bool { return e.vzdumps() == 1 }, 10*time.Second, 10*time.Millisecond)

	// River rescued the job while the first run still backs up.
	dup := e.jobFor(t, bid)
	dup.Attempt = 2
	requireSnooze(t, w.Work(ctx, dup))
	n, err := e.App.Ent.Action.Query().Where(entaction.Command(backups.KindCreateBackup)).Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.Equal(t, entbackup.StatusCreating, e.App.Ent.Backup.GetX(ctx, bid).Status)

	e.App.PVE.SetTaskDelay(0)
	e.App.PVE.FinishTasks()
	require.NoError(t, <-done)
	require.NoError(t, w.Work(ctx, dup))
	require.Equal(t, 1, e.vzdumps())
	require.Equal(t, entbackup.StatusAvailable, e.App.Ent.Backup.GetX(ctx, bid).Status)
	require.Equal(t, entaction.StatusSuccess, e.backupAction(t, id).Status)
	require.Len(t, e.rec.of(ext.BackupCreated), 1)
}

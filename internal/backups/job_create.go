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

package backups

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/apierr"
	"github.com/nebla-cloud/nebla/internal/db"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/action"
	"github.com/nebla-cloud/nebla/internal/ent/backup"
	"github.com/nebla-cloud/nebla/internal/ent/server"
	"github.com/nebla-cloud/nebla/internal/ext"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/riverqueue/river"
)

// errWaitServer is a server that is busy, or moved node, when the backup's turn comes.
var errWaitServer = errors.New("server busy")

// CreateBackupWorker waits for its node's slot, then backs the server up under its lock.
type CreateBackupWorker struct {
	river.WorkerDefaults[CreateBackupArgs]
	o WorkerOptions
}

func NewCreateBackupWorker(o WorkerOptions) *CreateBackupWorker {
	o.defaults()
	return &CreateBackupWorker{o: o}
}

func (*CreateBackupWorker) Timeout(*river.Job[CreateBackupArgs]) time.Duration { return backupTimeout }

func (w *CreateBackupWorker) Work(ctx context.Context, job *river.Job[CreateBackupArgs]) error {
	act, err := w.o.Ent.Action.Query().Where(action.RiverJobID(job.ID), action.Command(KindCreateBackup)).
		Order(ent.Desc(action.FieldID)).First(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return err
	}
	running := act != nil && act.Finished == nil
	row, err := w.o.Ent.Backup.Get(ctx, job.Args.BackupID)
	if ent.IsNotFound(err) {
		// Restore, rebuild and delete drop queued rows without cancelling their jobs.
		if running {
			return failAction(ctx, w.o.Ent, act.ID, servers.Failure{Code: actions.CodeFailed, Detail: "backup no longer exists"})
		}
		return nil
	}
	if err != nil {
		return err
	}
	switch row.Status {
	case backup.StatusQueued, backup.StatusCreating:
	case backup.StatusAvailable:
		// The last attempt recorded the archive but stopped before finishing the action.
		if running {
			w.o.prune(ctx, row.ServerID)
			return actions.Succeed(ctx, w.o.Ent, act.ID)
		}
		return nil
	default:
		return nil
	}
	srv, err := w.o.Ent.Server.Get(ctx, row.ServerID)
	if ent.IsNotFound(err) {
		return w.drop(ctx, row.ID)
	}
	if err != nil {
		return err
	}
	node := row.Node
	if !running {
		if srv.Status == server.StatusDeleting || srv.Status == server.StatusRebuilding {
			return w.drop(ctx, row.ID)
		}
		node = srv.Node
		if row.Status == backup.StatusQueued {
			if node != row.Node {
				if err := w.o.Ent.Backup.UpdateOneID(row.ID).SetNode(node).Exec(ctx); err != nil {
					return err
				}
			}
			first, err := w.firstInLine(ctx, row, node)
			if err != nil {
				return err
			}
			if !first {
				return river.JobSnooze(waitTurn)
			}
		}
	}
	release, ok, err := nodeSlot(ctx, w.o.SQL, row.LocationID, node)
	if err != nil {
		return err
	}
	if !ok {
		return river.JobSnooze(waitTurn)
	}
	defer release()
	if !running {
		act, row, srv, err = w.lock(ctx, job.ID, row.ID, node)
		if errors.Is(err, errWaitServer) {
			return river.JobSnooze(waitTurn)
		}
		if err != nil || act == nil {
			return err
		}
	}
	return w.steps(ctx, job, act, row, srv)
}

// drop deletes a pending backup that can no longer run.
func (w *CreateBackupWorker) drop(ctx context.Context, id int64) error {
	_, err := w.o.Ent.Backup.Delete().Where(backup.ID(id), backup.StatusIn(backup.StatusQueued, backup.StatusCreating)).Exec(ctx)
	return err
}

// firstInLine is whether no older pending backup waits for the same node.
func (w *CreateBackupWorker) firstInLine(ctx context.Context, row *ent.Backup, node string) (bool, error) {
	ahead, err := w.o.Ent.Backup.Query().Where(backup.LocationID(row.LocationID), backup.Node(node),
		backup.StatusIn(backup.StatusQueued, backup.StatusCreating), backup.IDNEQ(row.ID),
		backup.Or(backup.CreatedAtLT(row.CreatedAt), backup.And(backup.CreatedAt(row.CreatedAt), backup.IDLT(row.ID)))).Exist(ctx)
	return !ahead, err
}

// lock takes the server and starts the action on this job; a nil action means the backup was dropped.
func (w *CreateBackupWorker) lock(ctx context.Context, jobID, id int64, node string) (act *ent.Action, row *ent.Backup, srv *ent.Server, err error) {
	err = db.WithTx(ctx, w.o.SQL, func(_ *sql.Tx, etx *ent.Client) error {
		b, err := etx.Backup.Get(ctx, id)
		if ent.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
		srv, err = servers.LockServer(ctx, etx, b.ProjectID, b.ServerID)
		var ae apierr.E
		switch {
		case errors.As(err, &ae) && ae.Status == http.StatusConflict:
			return errWaitServer
		case errors.As(err, &ae) && ae.Status == http.StatusNotFound:
			return nil
		case err != nil:
			return err
		}
		b, err = etx.Backup.Query().Where(backup.ID(id)).ForUpdate().Only(ctx)
		if ent.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if b.Status != backup.StatusQueued && b.Status != backup.StatusCreating {
			return nil
		}
		if srv.Status == server.StatusDeleting || srv.Status == server.StatusRebuilding || srv.Vmid == nil {
			return etx.Backup.DeleteOneID(id).Exec(ctx)
		}
		if srv.Node != node {
			return errWaitServer
		}
		u := etx.Backup.UpdateOneID(id).SetStatus(backup.StatusCreating).SetNode(node)
		// A creating row resumes on the storage it may already have written to.
		if b.Status == backup.StatusQueued {
			loc, err := w.o.Catalog.GetLocation(ctx, b.LocationID)
			if err != nil {
				return err
			}
			if !w.o.Catalog.BackupsAvailable(loc) {
				return etx.Backup.DeleteOneID(id).Exec(ctx)
			}
			fp, err := servers.NetworkFingerprint(ctx, etx, srv)
			if err != nil {
				return err
			}
			u.SetStorage(loc.BackupStorage).SetInstanceGeneration(servers.InstanceGeneration(srv)).SetNetworkFingerprint(fp)
		}
		a, err := actions.Create(ctx, etx, b.ProjectID, KindCreateBackup, []actions.Resource{{ID: srv.ID, Type: servers.ResourceType}})
		if err != nil {
			return err
		}
		if err := actions.AttachJob(ctx, etx, a.ID, jobID); err != nil {
			return err
		}
		if row, err = u.Save(ctx); err != nil {
			return err
		}
		act, err = etx.Action.Get(ctx, a.ID)
		return err
	})
	if err != nil {
		return nil, nil, nil, err
	}
	return act, row, srv, nil
}

// steps runs vzdump, or finds the archive an earlier attempt made, then records it and prunes.
func (w *CreateBackupWorker) steps(ctx context.Context, job *river.Job[CreateBackupArgs], act *ent.Action, row *ent.Backup, srv *ent.Server) error {
	log := w.o.Logger.With("backup_id", row.ID, "server_id", row.ServerID, "action_id", act.ID, "command", act.Command)
	run := func(ctx context.Context) error {
		if srv.Vmid == nil {
			return servers.Permanent("server %d has no vm", srv.ID)
		}
		vmid := *srv.Vmid
		pc, err := w.o.pveFor(ctx, row.LocationID)
		if err != nil {
			return servers.StepError("connect to proxmox", err)
		}
		item, ok, err := findArchive(ctx, pc, row.Node, row.Storage, vmid, row)
		if err != nil {
			return servers.StepError("list backups", err)
		}
		if !ok {
			upid, err := pc.Vzdump(ctx, row.Node, vmid, row.Storage, notes(row.ServerID, row.ID))
			if err != nil {
				return servers.StepError("start backup", err)
			}
			if err := pc.WaitTask(ctx, row.Node, upid); err != nil {
				return servers.StepError("back up", err)
			}
			if item, ok, err = findArchive(ctx, pc, row.Node, row.Storage, vmid, row); err != nil {
				return servers.StepError("find archive", err)
			}
			if !ok {
				return servers.Permanent("vzdump finished but no archive on %s has notes %q", row.Storage, notes(row.ServerID, row.ID))
			}
		}
		done, err := w.o.Ent.Backup.UpdateOneID(row.ID).Where(backup.StatusEQ(backup.StatusCreating)).SetStatus(backup.StatusAvailable).
			SetVolid(item.Volid).SetSizeBytes(item.Size).SetCompletedAt(time.Now()).Save(ctx)
		if err != nil {
			return err
		}
		w.o.publish(ctx, ext.BackupCreated, done, nil)
		w.o.prune(ctx, row.ServerID)
		return actions.Succeed(ctx, w.o.Ent, act.ID)
	}
	fail := func(ctx context.Context, f servers.Failure) error {
		w.removeArchive(ctx, log, row, srv)
		if err := w.drop(ctx, row.ID); err != nil {
			return err
		}
		return failAction(ctx, w.o.Ent, act.ID, f)
	}
	return servers.Contract{Job: job.JobRow, Name: "back up", Finished: act.Finished != nil, Log: log, Run: run, Fail: fail}.Work(ctx)
}

// removeArchive deletes the failed backup's archive if vzdump wrote one, best effort.
func (w *CreateBackupWorker) removeArchive(ctx context.Context, log *slog.Logger, row *ent.Backup, srv *ent.Server) {
	if srv.Vmid == nil {
		return
	}
	pc, err := w.o.pveFor(ctx, row.LocationID)
	if err != nil {
		log.Warn("failed backup: archive not checked", "err", err)
		return
	}
	item, ok, err := findArchive(ctx, pc, row.Node, row.Storage, *srv.Vmid, row)
	if err != nil {
		log.Warn("failed backup: archive not checked", "err", err)
		return
	}
	if !ok {
		return
	}
	if err := removeArchive(ctx, pc, row.Node, row.Storage, item.Volid); err != nil {
		log.Warn("failed backup: archive left behind", "volid", item.Volid, "err", err)
	}
}

// prune deletes the server's oldest available backups beyond its keep; errors are logged, and the next run retries.
func (o *WorkerOptions) prune(ctx context.Context, serverID int64) {
	log := o.Logger.With("server_id", serverID)
	srv, err := o.Ent.Server.Get(ctx, serverID)
	if err != nil {
		log.Warn("prune backups", "err", err)
		return
	}
	rows, err := o.Ent.Backup.Query().Where(backup.ServerID(serverID), backup.StatusEQ(backup.StatusAvailable)).
		Order(backup.ByCreatedAt(entsql.OrderDesc()), backup.ByID(entsql.OrderDesc())).All(ctx)
	if err != nil {
		log.Warn("prune backups", "err", err)
		return
	}
	if len(rows) <= srv.BackupKeep {
		return
	}
	for _, b := range rows[srv.BackupKeep:] {
		if err := o.pruneOne(ctx, b); err != nil {
			log.Warn("prune backup", "backup_id", b.ID, "err", err)
		}
	}
}

func (o *WorkerOptions) pruneOne(ctx context.Context, b *ent.Backup) error {
	n, err := o.Ent.Backup.Update().Where(backup.ID(b.ID), backup.StatusEQ(backup.StatusAvailable)).SetStatus(backup.StatusDeleting).Save(ctx)
	if err != nil || n == 0 {
		return err
	}
	if b.Volid != nil {
		if err := o.deleteArchive(ctx, b); err != nil {
			back := o.Ent.Backup.Update().Where(backup.ID(b.ID), backup.StatusEQ(backup.StatusDeleting)).SetStatus(backup.StatusAvailable)
			return errors.Join(err, back.Exec(context.WithoutCancel(ctx)))
		}
	}
	if err := o.Ent.Backup.DeleteOneID(b.ID).Exec(ctx); err != nil {
		return err
	}
	if b.Volid != nil {
		o.publish(ctx, ext.BackupDeleted, b, nil)
	}
	return nil
}

// deleteArchive deletes b's recorded archive through any online node; it uses b's storage, not the location's current one.
func (o *WorkerOptions) deleteArchive(ctx context.Context, b *ent.Backup) error {
	pc, err := o.pveFor(ctx, b.LocationID)
	if err != nil {
		return servers.StepError("connect to proxmox", err)
	}
	node, err := o.storageNode(ctx, b.LocationID, b.Node)
	if err != nil {
		return err
	}
	if err := removeArchive(ctx, pc, node, b.Storage, *b.Volid); err != nil {
		return servers.StepError("delete backup", err)
	}
	return nil
}

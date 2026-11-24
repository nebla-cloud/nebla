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

// Package backups owns server backups: requests, quota and the backup actions.
package backups

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/catalog"
	"github.com/nebla-cloud/nebla/internal/db"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/backup"
	"github.com/nebla-cloud/nebla/internal/ent/server"
	"github.com/nebla-cloud/nebla/internal/jobs"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/riverqueue/river"
)

type Options struct {
	SQL     *sql.DB
	Ent     *ent.Client
	Catalog *catalog.Service
	// Jobs nil makes every request that enqueues fail with 503 service_unavailable.
	Jobs   *jobs.Jobs
	Logger *slog.Logger
}

type Service struct{ o Options }

func New(o Options) *Service {
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	return &Service{o: o}
}

// Backup is a backup as the project sees it; it carries no storage, node or volume id.
type Backup struct {
	ID, ServerID int64
	Kind, Status string
	SizeBytes    int64
	Created      time.Time
	Completed    *time.Time
	// RestoreResetsIdentity is whether restoring now would give the server a new cloud-init instance id.
	RestoreResetsIdentity bool
}

type ListFilter struct{ Status, Kind string }

func toBackup(b *ent.Backup, resets bool) Backup {
	return Backup{ID: b.ID, ServerID: b.ServerID, Kind: string(b.Kind), Status: string(b.Status), SizeBytes: b.SizeBytes,
		Created: b.CreatedAt, Completed: b.CompletedAt, RestoreResetsIdentity: resets}
}

// resetsIdentity is whether restoring b gives the server a new instance id: its network config changed since.
func resetsIdentity(b *ent.Backup, current string) bool {
	return b.NetworkFingerprint == "" || b.NetworkFingerprint != current
}

func (s *Service) serverRow(ctx context.Context, projectID, serverID int64) (*ent.Server, error) {
	row, err := s.o.Ent.Server.Query().Where(server.ID(serverID), server.ProjectID(projectID)).Only(ctx)
	return row, notFound(err)
}

// List returns the server's backups, newest first.
func (s *Service) List(ctx context.Context, projectID, serverID int64, f ListFilter) ([]Backup, error) {
	row, err := s.serverRow(ctx, projectID, serverID)
	if err != nil {
		return nil, err
	}
	q := s.o.Ent.Backup.Query().Where(backup.ServerID(serverID), backup.ProjectID(projectID))
	if f.Status != "" {
		if backup.StatusValidator(backup.Status(f.Status)) != nil {
			return nil, invalid("unknown status %q", f.Status)
		}
		q = q.Where(backup.StatusEQ(backup.Status(f.Status)))
	}
	if f.Kind != "" {
		if backup.KindValidator(backup.Kind(f.Kind)) != nil {
			return nil, invalid("unknown kind %q", f.Kind)
		}
		q = q.Where(backup.KindEQ(backup.Kind(f.Kind)))
	}
	rows, err := q.Order(backup.ByCreatedAt(entsql.OrderDesc()), backup.ByID(entsql.OrderDesc())).All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Backup, len(rows))
	if len(rows) == 0 {
		return out, nil
	}
	fp, err := servers.NetworkFingerprint(ctx, s.o.Ent, row)
	if err != nil {
		return nil, err
	}
	for i, r := range rows {
		out[i] = toBackup(r, resetsIdentity(r, fp))
	}
	return out, nil
}

// Get returns one of the server's backups.
func (s *Service) Get(ctx context.Context, projectID, serverID, id int64) (Backup, error) {
	row, err := s.serverRow(ctx, projectID, serverID)
	if err != nil {
		return Backup{}, err
	}
	b, err := s.o.Ent.Backup.Query().Where(backup.ID(id), backup.ServerID(serverID), backup.ProjectID(projectID)).Only(ctx)
	if err != nil {
		return Backup{}, notFound(err)
	}
	fp, err := servers.NetworkFingerprint(ctx, s.o.Ent, row)
	if err != nil {
		return Backup{}, err
	}
	return toBackup(b, resetsIdentity(b, fp)), nil
}

// BackUpNow queues a manual backup; created is false when one is already queued or running, which is returned instead.
func (s *Service) BackUpNow(ctx context.Context, projectID, serverID int64) (b Backup, created bool, err error) {
	if s.o.Jobs == nil {
		return Backup{}, false, errNoJobs
	}
	for range 2 {
		var row *ent.Backup
		row, created, err = s.backUpNow(ctx, projectID, serverID)
		if err == nil {
			return toBackup(row, false), created, nil
		}
		// A concurrent request won the one-pending slot; the retry returns its row.
		if !pendingExists(err) {
			return Backup{}, false, err
		}
	}
	return Backup{}, false, err
}

func (s *Service) backUpNow(ctx context.Context, projectID, serverID int64) (*ent.Backup, bool, error) {
	var out *ent.Backup
	var created bool
	err := db.WithTx(ctx, s.o.SQL, func(tx *sql.Tx, etx *ent.Client) error {
		srv, err := etx.Server.Query().Where(server.ID(serverID), server.ProjectID(projectID)).Only(ctx)
		if err != nil {
			return notFound(err)
		}
		if !srv.BackupsEnabled {
			return errDisabled
		}
		pending, err := etx.Backup.Query().Where(backup.ServerID(serverID), backup.StatusIn(backup.StatusQueued, backup.StatusCreating)).Only(ctx)
		if err == nil {
			out = pending
			return nil
		}
		if !ent.IsNotFound(err) {
			return err
		}
		loc, err := s.o.Catalog.GetLocation(ctx, srv.LocationID)
		if err != nil {
			return err
		}
		if !s.o.Catalog.BackupsAvailable(loc) {
			return errUnavailable
		}
		out, err = etx.Backup.Create().SetProjectID(projectID).SetServerID(serverID).SetLocationID(srv.LocationID).
			SetStorage(loc.BackupStorage).SetNode(srv.Node).SetKind(backup.KindManual).
			SetInstanceGeneration(servers.InstanceGeneration(srv)).Save(ctx)
		if err != nil {
			return err
		}
		created = true
		_, err = s.o.Jobs.InsertTx(ctx, tx, CreateBackupArgs{BackupID: out.ID}, nil)
		return err
	})
	return out, created, err
}

// lockBackup locks the server, then the backup row, in tx.
func (s *Service) lockBackup(ctx context.Context, etx *ent.Client, projectID, serverID, id int64) (*ent.Server, *ent.Backup, error) {
	row, err := servers.LockServer(ctx, etx, projectID, serverID)
	if err != nil {
		return nil, nil, err
	}
	b, err := etx.Backup.Query().Where(backup.ID(id), backup.ServerID(serverID)).ForUpdate().Only(ctx)
	if err != nil {
		return nil, nil, notFound(err)
	}
	return row, b, nil
}

// Delete marks an idle backup deleting and queues delete_backup; one that is queued, running or being restored gives 409 backup_busy.
func (s *Service) Delete(ctx context.Context, projectID, serverID, id int64) (actions.Action, error) {
	if s.o.Jobs == nil {
		return actions.Action{}, errNoJobs
	}
	var act actions.Action
	err := db.WithTx(ctx, s.o.SQL, func(tx *sql.Tx, etx *ent.Client) error {
		_, b, err := s.lockBackup(ctx, etx, projectID, serverID, id)
		if err != nil {
			return err
		}
		switch b.Status {
		case backup.StatusAvailable, backup.StatusMissing, backup.StatusError:
		default:
			return errBusy
		}
		if err := etx.Backup.UpdateOneID(id).SetStatus(backup.StatusDeleting).Exec(ctx); err != nil {
			return err
		}
		act, err = s.o.Jobs.StartAction(ctx, tx, etx, projectID, KindDeleteBackup,
			[]actions.Resource{{ID: serverID, Type: servers.ResourceType}},
			func(actionID int64) river.JobArgs {
				return DeleteBackupArgs{BackupID: id, ServerID: serverID, ActionID: actionID, From: b.Status}
			}, nil)
		return err
	})
	return act, err
}

// Restore queues restore_backup for an available backup; the server is locked, its queued backups are dropped and it shows rebuilding.
func (s *Service) Restore(ctx context.Context, projectID, serverID, id int64) (actions.Action, error) {
	if s.o.Jobs == nil {
		return actions.Action{}, errNoJobs
	}
	var act actions.Action
	err := db.WithTx(ctx, s.o.SQL, func(tx *sql.Tx, etx *ent.Client) error {
		row, b, err := s.lockBackup(ctx, etx, projectID, serverID, id)
		if err != nil {
			return err
		}
		if b.Status != backup.StatusAvailable || b.Volid == nil {
			return errBusy
		}
		switch {
		case row.Status == servers.StatusMissing:
			return servers.ErrMissing
		case row.Vmid == nil || (row.Status != servers.StatusRunning && row.Status != servers.StatusOff):
			return errServerNotReady
		}
		fp, err := servers.NetworkFingerprint(ctx, etx, row)
		if err != nil {
			return err
		}
		args := servers.RestoreServerArgs{ServerID: serverID, BackupID: id, Archive: *b.Volid, StartAfter: row.Status == servers.StatusRunning}
		if !resetsIdentity(b, fp) {
			args.InstanceGeneration = b.InstanceGeneration
		}
		if _, err := etx.Backup.Delete().Where(backup.ServerID(serverID), backup.StatusEQ(backup.StatusQueued)).Exec(ctx); err != nil {
			return err
		}
		if err := etx.Backup.UpdateOneID(id).SetStatus(backup.StatusRestoring).Exec(ctx); err != nil {
			return err
		}
		if err := etx.Server.UpdateOneID(serverID).SetStatus(server.StatusRebuilding).Exec(ctx); err != nil {
			return err
		}
		act, err = s.o.Jobs.StartAction(ctx, tx, etx, projectID, servers.KindRestoreBackup,
			[]actions.Resource{{ID: serverID, Type: servers.ResourceType}},
			func(actionID int64) river.JobArgs {
				args.ActionID = actionID
				return args
			}, nil)
		return err
	})
	return act, err
}

// BackupInfo is servers.Options.BackupInfo: each server's stored count, last backup time and next due time.
func (s *Service) BackupInfo(ctx context.Context, ids []int64) (map[int64]servers.ServerBackups, error) {
	out := make(map[int64]servers.ServerBackups, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.o.Ent.Backup.Query().Where(backup.ServerIDIn(ids...), servers.StoredBackups).
		Select(backup.FieldServerID, backup.FieldCompletedAt).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		info := out[r.ServerID]
		info.Count++
		if r.CompletedAt != nil && (info.LastBackupAt == nil || r.CompletedAt.After(*info.LastBackupAt)) {
			info.LastBackupAt = r.CompletedAt
		}
		out[r.ServerID] = info
	}
	srvs, err := s.o.Ent.Server.Query().Where(server.IDIn(ids...), server.BackupsEnabled(true)).
		Select(server.FieldID, server.FieldLocationID, server.FieldBackupSlot).All(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	for _, r := range srvs {
		loc, err := s.o.Catalog.GetLocation(ctx, r.LocationID)
		if err != nil {
			return nil, err
		}
		if !s.o.Catalog.BackupsAvailable(loc) {
			continue
		}
		info := out[r.ID]
		next := NextDue(now, loc, r.BackupSlot)
		info.NextBackupAt = &next
		out[r.ID] = info
	}
	return out, nil
}

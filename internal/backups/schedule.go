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
	"time"

	"github.com/nebla-cloud/nebla/internal/catalog"
	"github.com/nebla-cloud/nebla/internal/db"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/action"
	"github.com/nebla-cloud/nebla/internal/ent/backup"
	"github.com/nebla-cloud/nebla/internal/ent/location"
	"github.com/nebla-cloud/nebla/internal/ent/server"
	"github.com/nebla-cloud/nebla/internal/jobs"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/riverqueue/river"
)

const (
	minutesPerDay    = 1440
	KindSchedule     = "schedule_backups"
	scheduleInterval = 5 * time.Minute
)

// dueMinute is the UTC minute of day the server's backup is due: the window start plus slot's share of the window.
func dueMinute(loc catalog.Location, slot int) int {
	length := ((loc.BackupWindowEnd-loc.BackupWindowStart)%minutesPerDay + minutesPerDay) % minutesPerDay
	if length == 0 {
		length = minutesPerDay
	}
	return (loc.BackupWindowStart + slot*length/minutesPerDay) % minutesPerDay
}

// LastDue is the latest due time at or before now.
func LastDue(now time.Time, loc catalog.Location, slot int) time.Time {
	now = now.UTC()
	due := now.Truncate(24 * time.Hour).Add(time.Duration(dueMinute(loc, slot)) * time.Minute)
	if due.After(now) {
		due = due.AddDate(0, 0, -1)
	}
	return due
}

// NextDue is the earliest due time after now.
func NextDue(now time.Time, loc catalog.Location, slot int) time.Time {
	return LastDue(now, loc, slot).AddDate(0, 0, 1)
}

// ScheduleArgs is the periodic job that queues each server's daily backup once its slot has passed.
type ScheduleArgs struct{}

func (ScheduleArgs) Kind() string { return KindSchedule }

// InsertOpts never retry and never double a slow run; the next tick is the retry.
func (ScheduleArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: jobs.QueueMaintenance, MaxAttempts: 1, UniqueOpts: river.UniqueOpts{ByState: jobs.LiveStates}}
}

// SchedulePeriodicJob runs the scheduler every five minutes.
func SchedulePeriodicJob() *river.PeriodicJob {
	return river.NewPeriodicJob(river.PeriodicInterval(scheduleInterval),
		func() (river.JobArgs, *river.InsertOpts) { return ScheduleArgs{}, nil }, &river.PeriodicJobOpts{ID: KindSchedule, RunOnStart: true})
}

type ScheduleWorker struct {
	river.WorkerDefaults[ScheduleArgs]
	o WorkerOptions
}

func NewScheduleWorker(o WorkerOptions) *ScheduleWorker {
	o.defaults()
	return &ScheduleWorker{o: o}
}

func (*ScheduleWorker) Timeout(*river.Job[ScheduleArgs]) time.Duration { return jobTimeout }

func (w *ScheduleWorker) Work(ctx context.Context, _ *river.Job[ScheduleArgs]) error {
	var j *jobs.Jobs
	if w.o.Jobs != nil {
		j = w.o.Jobs()
	}
	if j == nil {
		return errNoJobs
	}
	// A pending backup whose job died would otherwise hold its server's pending slot until the next restart.
	if err := db.WithTx(ctx, w.o.SQL, func(tx *sql.Tx, _ *ent.Client) error { return requeueLost(ctx, tx, j) }); err != nil {
		return err
	}
	now := w.o.now().UTC()
	ids, err := w.o.Ent.Location.Query().Where(location.BackupStorageNEQ(""), location.BackupStorageOk(true)).IDs(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, id := range ids {
		loc, err := w.o.Catalog.GetLocation(ctx, id)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if !w.o.Catalog.BackupsAvailable(loc) {
			continue
		}
		srvs, err := w.o.Ent.Server.Query().Where(server.LocationID(id), server.BackupsEnabled(true), server.BackupsEnabledAtNotNil(),
			server.StatusIn(server.StatusRunning, server.StatusOff), server.VmidNotNil()).All(ctx)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, s := range srvs {
			if err := w.scheduleOne(ctx, j, loc, s, now); err != nil {
				w.o.Logger.Error("schedule backup", "server_id", s.ID, "err", err)
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

// scheduleOne queues the server's backup when its latest slot has passed with no backup run or queued since.
func (w *ScheduleWorker) scheduleOne(ctx context.Context, j *jobs.Jobs, loc catalog.Location, s *ent.Server, now time.Time) error {
	due := LastDue(now, loc, s.BackupSlot)
	if !s.BackupsEnabledAt.Before(due) {
		return nil
	}
	busy, err := w.o.Ent.Backup.Query().Where(backup.ServerID(s.ID), backup.Or(
		backup.And(backup.KindEQ(backup.KindScheduled), backup.CreatedAtGTE(due)),
		backup.StatusIn(backup.StatusQueued, backup.StatusCreating))).Exist(ctx)
	if err != nil || busy {
		return err
	}
	// Any backup run since the slot counts: a failed one leaves no row, and prune may already have removed a good one.
	ran, err := w.o.Ent.Action.Query().Where(servers.ActionsOf(s.ProjectID, s.ID), action.Command(KindCreateBackup),
		action.StartedGTE(due)).Exist(ctx)
	if err != nil || ran {
		return err
	}
	return db.WithTx(ctx, w.o.SQL, func(tx *sql.Tx, _ *ent.Client) error {
		var id int64
		err := tx.QueryRowContext(ctx, `
			insert into backups (project_id, server_id, location_id, storage, node, kind, status, instance_generation,
				network_fingerprint, size_bytes, created_at)
			values ($1, $2, $3, $4, $5, 'scheduled', 'queued', $6, '', 0, $7)
			on conflict (server_id) where status in ('queued', 'creating') do nothing
			returning id`,
			s.ProjectID, s.ID, s.LocationID, loc.BackupStorage, s.Node, servers.InstanceGeneration(s), now).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		_, err = j.InsertTx(ctx, tx, CreateBackupArgs{BackupID: id}, nil)
		return err
	})
}

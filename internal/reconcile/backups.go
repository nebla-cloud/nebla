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

package reconcile

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/nebla-cloud/nebla/internal/db"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/backup"
	"github.com/nebla-cloud/nebla/internal/ent/backuptombstone"
	"github.com/nebla-cloud/nebla/internal/pve"
	"github.com/nebla-cloud/nebla/internal/servers"
)

const backupNotesPrefix = "nebla server "

// syncBackups marks available backups whose archive is gone as missing, and missing ones listed again available, and retries tombstones;
// it never deletes an archive it has no record of.
func (r *Reconciler) syncBackups(ctx context.Context, log *slog.Logger, locationID int64, pc pve.Client, vms view) error {
	rows, err := r.o.Ent.Backup.Query().Where(backup.LocationID(locationID)).All(ctx)
	if err != nil {
		return err
	}
	tombs, err := r.o.Ent.BackupTombstone.Query().Where(backuptombstone.LocationID(locationID)).All(ctx)
	if err != nil {
		return err
	}
	loc, err := r.o.Ent.Location.Get(ctx, locationID)
	if err != nil {
		return err
	}
	onlineNodes := slices.Sorted(maps.Keys(vms.online))
	if len(onlineNodes) == 0 {
		return nil
	}
	node := onlineNodes[0]
	storages := map[string]bool{}
	if loc.BackupStorage != "" {
		storages[loc.BackupStorage] = true
	}
	volids := map[string]bool{}
	ids := map[int64]bool{}
	for _, b := range rows {
		storages[b.Storage] = true
		ids[b.ID] = true
		if b.Volid != nil {
			volids[*b.Volid] = true
		}
	}
	for _, t := range tombs {
		storages[t.Storage] = true
		volids[t.Volid] = true
	}
	var errs []error
	for _, storage := range slices.Sorted(maps.Keys(storages)) {
		listedAt := time.Now()
		tctx, cancel := context.WithTimeout(ctx, pveTimeout)
		items, err := pc.BackupContent(tctx, node, storage, 0)
		cancel()
		if err != nil {
			log.Warn("reconcile: list backups", "node", node, "storage", storage, "err", err)
			continue
		}
		listed := map[string]bool{}
		for _, it := range items {
			listed[it.Volid] = true
			if !strings.HasPrefix(strings.TrimSpace(it.Notes), backupNotesPrefix) || volids[it.Volid] || notedRow(it.Notes, ids) {
				continue
			}
			log.Warn("reconcile: backup archive without a row left alone", "volid", it.Volid, "notes", it.Notes)
		}
		for _, b := range rows {
			if b.Storage != storage || b.Volid == nil {
				continue
			}
			var err error
			switch {
			case b.Status == backup.StatusAvailable && !listed[*b.Volid]:
				err = r.markBackupMissing(ctx, log, b.ID, *b.Volid, listedAt)
			case b.Status == backup.StatusMissing && listed[*b.Volid]:
				err = r.markBackupFound(ctx, log, b.ID, *b.Volid)
			}
			if err != nil {
				errs = append(errs, fmt.Errorf("backup %d: %w", b.ID, err))
			}
		}
	}
	for _, t := range tombs {
		tctx, cancel := context.WithTimeout(ctx, pveTimeout)
		err := servers.RemoveArchive(tctx, pc, node, t.Storage, t.Volid)
		cancel()
		if err == nil {
			err = r.o.Ent.BackupTombstone.DeleteOneID(t.ID).Exec(ctx)
			if err != nil && !ent.IsNotFound(err) {
				errs = append(errs, fmt.Errorf("tombstone %d: %w", t.ID, err))
			}
			continue
		}
		log.Warn("reconcile: delete tombstoned backup", "volid", t.Volid, "attempts", t.Attempts+1, "err", err)
		if err := r.o.Ent.BackupTombstone.UpdateOneID(t.ID).AddAttempts(1).Exec(ctx); err != nil && !ent.IsNotFound(err) {
			errs = append(errs, fmt.Errorf("tombstone %d: %w", t.ID, err))
		}
	}
	return errors.Join(errs...)
}

// notedRow reports whether the notes name a backup row that exists, such as one still being taken.
func notedRow(notes string, ids map[int64]bool) bool {
	var sid, bid int64
	_, err := fmt.Sscanf(strings.TrimSpace(notes), backupNotesPrefix+"%d backup %d", &sid, &bid)
	return err == nil && ids[bid]
}

// markBackupMissing re-reads the row under its lock; one that finished after the listing is left alone.
func (r *Reconciler) markBackupMissing(ctx context.Context, log *slog.Logger, id int64, volid string, listedAt time.Time) error {
	return db.WithTx(ctx, r.o.SQL, func(_ *sql.Tx, etx *ent.Client) error {
		row, err := etx.Backup.Query().Where(backup.ID(id)).ForUpdate().Only(ctx)
		if ent.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
		done := row.CreatedAt
		if row.CompletedAt != nil {
			done = *row.CompletedAt
		}
		if row.Status != backup.StatusAvailable || row.Volid == nil || *row.Volid != volid || !done.Before(listedAt) {
			return nil
		}
		log.Warn("reconcile: backup archive gone", "backup_id", id, "server_id", row.ServerID)
		return etx.Backup.UpdateOneID(id).SetStatus(backup.StatusMissing).Exec(ctx)
	})
}

// markBackupFound makes a backup available again if it is still missing with the listed archive.
func (r *Reconciler) markBackupFound(ctx context.Context, log *slog.Logger, id int64, volid string) error {
	n, err := r.o.Ent.Backup.Update().Where(backup.ID(id), backup.StatusEQ(backup.StatusMissing), backup.Volid(volid)).
		SetStatus(backup.StatusAvailable).Save(ctx)
	if n > 0 {
		log.Info("reconcile: backup archive back", "backup_id", id)
	}
	return err
}

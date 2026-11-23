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

package servers

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/nebla-cloud/nebla/internal/catalog"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/backup"
	"github.com/nebla-cloud/nebla/internal/ent/backuptombstone"
	"github.com/nebla-cloud/nebla/internal/ext"
	"github.com/nebla-cloud/nebla/internal/pve"
)

// RemoveArchive deletes a recorded archive; one that is already gone counts as deleted.
func RemoveArchive(ctx context.Context, pc pve.Client, node, storage, volid string) error {
	err := pc.DeleteVolume(ctx, node, storage, volid)
	var te *pve.TaskError
	switch {
	case err == nil, errors.Is(err, pve.ErrNotFound):
		return nil
	case errors.As(err, &te) && strings.Contains(te.ExitStatus, "does not exist"):
		return nil
	}
	return err
}

// deleteBackups removes the server's archives and rows; a failed archive delete leaves a tombstone. It never fails the delete.
func (w *DeleteServerWorker) deleteBackups(ctx context.Context, pc pve.Client, row *ent.Server) {
	log := w.o.Logger.With("server_id", row.ID)
	rows, err := w.o.Ent.Backup.Query().Where(backup.ServerID(row.ID)).Order(ent.Asc(backup.FieldID)).All(ctx)
	if err != nil {
		log.Warn("delete backups: list rows", "err", err)
		return
	}
	loc, err := w.o.Catalog.GetLocation(ctx, row.LocationID)
	if err != nil {
		log.Warn("delete backups: load location", "err", err)
		return
	}
	for _, b := range rows {
		if b.Volid != nil && !w.dropArchive(ctx, log, pc, loc, b) {
			continue
		}
		if err := w.o.Ent.Backup.DeleteOneID(b.ID).Exec(ctx); err != nil && !ent.IsNotFound(err) {
			log.Warn("delete backups: delete row", "backup_id", b.ID, "err", err)
			continue
		}
		if b.Volid != nil {
			w.publishBackupDeleted(ctx, b)
		}
	}
}

// dropArchive deletes b's archive or tombstones it; false means neither worked and the row must stay.
func (w *DeleteServerWorker) dropArchive(ctx context.Context, log *slog.Logger, pc pve.Client, loc catalog.Location, b *ent.Backup) bool {
	node := onlineNode(loc, b.Node)
	err := errors.New("no online node")
	if node != "" {
		err = RemoveArchive(ctx, pc, node, b.Storage, *b.Volid)
	}
	if err == nil {
		return true
	}
	log.Warn("delete backups: delete archive, leaving a tombstone", "backup_id", b.ID, "err", err)
	err = w.o.Ent.BackupTombstone.Create().SetVolid(*b.Volid).SetStorage(b.Storage).SetLocationID(b.LocationID).
		OnConflictColumns(backuptombstone.FieldVolid).Ignore().Exec(ctx)
	if err != nil {
		log.Warn("delete backups: record tombstone", "backup_id", b.ID, "err", err)
		return false
	}
	return true
}

// onlineNode is prefer when it is online, else the first online node by name.
func onlineNode(loc catalog.Location, prefer string) string {
	nodes := slices.Clone(loc.Nodes)
	slices.SortFunc(nodes, func(a, b catalog.Node) int { return strings.Compare(a.Name, b.Name) })
	for _, n := range nodes {
		if n.Name == prefer && n.Status == "online" {
			return prefer
		}
	}
	for _, n := range nodes {
		if n.Status == "online" {
			return n.Name
		}
	}
	return ""
}

func (w *DeleteServerWorker) publishBackupDeleted(ctx context.Context, b *ent.Backup) {
	if w.o.Bus == nil {
		return
	}
	data := map[string]any{"server_id": b.ServerID, "size_bytes": b.SizeBytes, "kind": string(b.Kind), "location_id": b.LocationID}
	w.o.Bus.Publish(ctx, ext.Event{Kind: ext.BackupDeleted, ProjectID: b.ProjectID, ResourceType: "backup", ResourceID: b.ID, At: time.Now(), Data: data})
}

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
	"fmt"
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
		blog := log.With("backup_id", b.ID)
		node := onlineNode(loc, b.Node)
		if b.Volid != nil {
			if err := DropArchive(ctx, blog, w.o.Ent, pc, node, b, *b.Volid); err != nil {
				blog.Warn("delete backups: record tombstone", "err", err)
				continue
			}
		} else if b.Status == backup.StatusCreating {
			if node == "" {
				blog.Warn("delete backups: no online node to look for an unrecorded archive")
			} else {
				DropUnrecordedArchive(ctx, blog, w.o.Ent, pc, node, b)
			}
		}
		if err := w.o.Ent.Backup.DeleteOneID(b.ID).Exec(ctx); err != nil && !ent.IsNotFound(err) {
			blog.Warn("delete backups: delete row", "err", err)
			continue
		}
		if b.Volid != nil {
			w.publishBackupDeleted(ctx, b)
		}
	}
}

// BackupNotes are the vzdump notes that name a backup.
func BackupNotes(serverID, backupID int64) string {
	return fmt.Sprintf("nebla server %d backup %d", serverID, backupID)
}

// FindArchive is the archive on storage whose notes name b; vmid 0 searches all of it.
func FindArchive(ctx context.Context, pc pve.Client, node, storage string, vmid int, b *ent.Backup) (pve.BackupItem, bool, error) {
	items, err := pc.BackupContent(ctx, node, storage, vmid)
	if err != nil {
		return pve.BackupItem{}, false, err
	}
	want := BackupNotes(b.ServerID, b.ID)
	i := slices.IndexFunc(items, func(it pve.BackupItem) bool { return strings.TrimSpace(it.Notes) == want })
	if i < 0 {
		return pve.BackupItem{}, false, nil
	}
	return items[i], true, nil
}

// DropArchive deletes volid, an archive of b, through node, or tombstones it; an error means neither worked.
func DropArchive(ctx context.Context, log *slog.Logger, c *ent.Client, pc pve.Client, node string, b *ent.Backup, volid string) error {
	err := errors.New("no online node")
	if node != "" {
		err = RemoveArchive(ctx, pc, node, b.Storage, volid)
	}
	if err == nil {
		return nil
	}
	log.Warn("delete backup archive, leaving a tombstone", "volid", volid, "err", err)
	return c.BackupTombstone.Create().SetVolid(volid).SetStorage(b.Storage).SetLocationID(b.LocationID).
		OnConflictColumns(backuptombstone.FieldVolid).Ignore().Exec(ctx)
}

// DropUnrecordedArchive deletes or tombstones the archive an interrupted backup wrote before its volid was recorded; best effort.
func DropUnrecordedArchive(ctx context.Context, log *slog.Logger, c *ent.Client, pc pve.Client, node string, b *ent.Backup) {
	item, ok, err := FindArchive(ctx, pc, node, b.Storage, 0, b)
	if err != nil {
		log.Warn("look for an unrecorded backup archive", "err", err)
		return
	}
	if !ok {
		return
	}
	if err := DropArchive(ctx, log, c, pc, node, b, item.Volid); err != nil {
		log.Warn("unrecorded backup archive left behind", "volid", item.Volid, "err", err)
	}
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

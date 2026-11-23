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
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/catalog"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ext"
	"github.com/nebla-cloud/nebla/internal/jobs"
	"github.com/nebla-cloud/nebla/internal/pve"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/riverqueue/river"
)

const (
	KindCreateBackup = "create_backup"
	KindDeleteBackup = "delete_backup"
	// ResourceType is the events resource type of a backup.
	ResourceType = "backup"

	backupTimeout = 2 * time.Hour
	jobTimeout    = 5 * time.Minute
	// waitTurn is how long a backup snoozes while its node or server is busy.
	waitTurn = time.Minute
)

// CreateBackupArgs is the create_backup job for a queued row; its action starts once the job holds the node slot.
type CreateBackupArgs struct {
	BackupID int64 `json:"backup_id"`
}

func (CreateBackupArgs) Kind() string                 { return KindCreateBackup }
func (CreateBackupArgs) InsertOpts() river.InsertOpts { return servers.InsertOpts() }

// DeleteBackupArgs is the delete_backup job.
type DeleteBackupArgs struct {
	BackupID int64 `json:"backup_id"`
	ServerID int64 `json:"server_id"`
	ActionID int64 `json:"action_id"`
}

func (DeleteBackupArgs) Kind() string                 { return KindDeleteBackup }
func (DeleteBackupArgs) InsertOpts() river.InsertOpts { return servers.InsertOpts() }

type WorkerOptions struct {
	SQL     *sql.DB
	Ent     *ent.Client
	Catalog *catalog.Service
	// Bus nil drops events.
	Bus servers.Publisher
	// Jobs is the runtime the scheduler enqueues on; it may return nil until the runtime exists.
	Jobs   func() *jobs.Jobs
	Logger *slog.Logger
	// pveFor is Catalog.ProxmoxFor unless a test replaces it.
	pveFor func(ctx context.Context, locationID int64) (pve.Client, error)
	now    func() time.Time
	// beforeStart lets a test fail create_backup before its contract runs.
	beforeStart func() error
}

// RegisterWorkers adds the backup job workers.
func RegisterWorkers(w *river.Workers, o WorkerOptions) {
	river.AddWorker(w, NewCreateBackupWorker(o))
	river.AddWorker(w, NewDeleteBackupWorker(o))
	river.AddWorker(w, NewScheduleWorker(o))
	river.AddWorker(w, NewUsageWorker(o))
}

func (o *WorkerOptions) defaults() {
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	if o.pveFor == nil && o.Catalog != nil {
		o.pveFor = o.Catalog.ProxmoxFor
	}
	if o.now == nil {
		o.now = time.Now
	}
}

// notes is the vzdump notes that tie an archive to its backup row.
func notes(serverID, backupID int64) string {
	return fmt.Sprintf("nebla server %d backup %d", serverID, backupID)
}

func (o *WorkerOptions) publish(ctx context.Context, kind ext.EventKind, b *ent.Backup, extra map[string]any) {
	if o.Bus == nil {
		return
	}
	data := map[string]any{"server_id": b.ServerID, "size_bytes": b.SizeBytes, "kind": string(b.Kind), "location_id": b.LocationID}
	for k, v := range extra {
		data[k] = v
	}
	o.Bus.Publish(ctx, ext.Event{Kind: kind, ProjectID: b.ProjectID, ResourceType: ResourceType, ResourceID: b.ID, At: time.Now(), Data: data})
}

func failAction(ctx context.Context, c *ent.Client, id int64, f servers.Failure) error {
	return actions.Fail(context.WithoutCancel(ctx), c, id, f.Code, f.Detail)
}

// storageNode is a node for calls on shared backup storage: prefer when it is online, else the first online node.
func (o *WorkerOptions) storageNode(ctx context.Context, locationID int64, prefer string) (string, error) {
	loc, err := o.Catalog.GetLocation(ctx, locationID)
	if err != nil {
		return "", err
	}
	nodes := slices.Clone(loc.Nodes)
	slices.SortFunc(nodes, func(a, b catalog.Node) int { return strings.Compare(a.Name, b.Name) })
	if slices.ContainsFunc(nodes, func(n catalog.Node) bool { return n.Name == prefer && n.Status == "online" }) {
		return prefer, nil
	}
	for _, n := range nodes {
		if n.Status == "online" {
			return n.Name, nil
		}
	}
	return "", servers.Transient("no online node in location %d", locationID)
}

// findArchive is the archive on storage whose notes name the backup.
func findArchive(ctx context.Context, pc pve.Client, node, storage string, vmid int, b *ent.Backup) (pve.BackupItem, bool, error) {
	items, err := pc.BackupContent(ctx, node, storage, vmid)
	if err != nil {
		return pve.BackupItem{}, false, err
	}
	want := notes(b.ServerID, b.ID)
	i := slices.IndexFunc(items, func(it pve.BackupItem) bool { return strings.TrimSpace(it.Notes) == want })
	if i < 0 {
		return pve.BackupItem{}, false, nil
	}
	return items[i], true, nil
}

// removeArchive deletes a recorded archive; one that is already gone counts as deleted.
func removeArchive(ctx context.Context, pc pve.Client, node, storage, volid string) error {
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

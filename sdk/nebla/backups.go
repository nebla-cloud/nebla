// Copyright (C) 2026 The Nebla contributors
// SPDX-License-Identifier: Apache-2.0

package nebla

import (
	"context"
	"net/url"
	"strconv"
	"time"

	"github.com/nebla-cloud/nebla/sdk/nebla/internal/wire"
)

// BackupStatus is the state of a backup.
type BackupStatus string

// Backup statuses.
const (
	BackupStatusQueued    BackupStatus = "queued"
	BackupStatusCreating  BackupStatus = "creating"
	BackupStatusAvailable BackupStatus = "available"
	BackupStatusRestoring BackupStatus = "restoring"
	BackupStatusDeleting  BackupStatus = "deleting"
	BackupStatusError     BackupStatus = "error"
	BackupStatusMissing   BackupStatus = "missing"
)

// BackupKind says whether a backup was scheduled or taken on demand.
type BackupKind string

// Backup kinds.
const (
	BackupKindScheduled BackupKind = "scheduled"
	BackupKindManual    BackupKind = "manual"
)

// Backup is a stored copy of a server.
type Backup struct {
	ID        int64
	Server    int64
	Kind      BackupKind
	Status    BackupStatus
	Size      int64
	Created   time.Time
	Completed *time.Time
	// RestoreResetsIdentity is true when restoring gives the server a new cloud-init instance id.
	RestoreResetsIdentity bool
}

func backupFromWire(w wire.Backup) Backup {
	return Backup{ID: w.ID, Server: w.Server, Kind: BackupKind(w.Kind), Status: BackupStatus(w.Status), Size: w.Size, Created: w.Created, Completed: w.Completed, RestoreResetsIdentity: w.RestoreResetsIdentity}
}

// BackupListOpts filters a server's backups.
type BackupListOpts struct {
	Status BackupStatus
	Kind   BackupKind
}

func (o BackupListOpts) values() url.Values {
	q := url.Values{}
	setIf(q, "status", string(o.Status))
	setIf(q, "kind", string(o.Kind))
	return q
}

// BackupsService works with a server's backups.
type BackupsService struct{ c *Client }

func backupPath(serverID, backupID int64) string {
	return serverPath(serverID, "/backups/"+strconv.FormatInt(backupID, 10))
}

// List returns a server's backups, newest first; the API does not paginate them.
func (s *BackupsService) List(ctx context.Context, serverID int64, opts BackupListOpts) ([]Backup, error) {
	var w struct {
		Backups []wire.Backup `json:"backups"`
	}
	if err := s.c.do(ctx, "GET", serverPath(serverID, "/backups"), opts.values(), nil, &w); err != nil {
		return nil, err
	}
	out := make([]Backup, len(w.Backups))
	for i, b := range w.Backups {
		out[i] = backupFromWire(b)
	}
	return out, nil
}

// Get returns one backup.
func (s *BackupsService) Get(ctx context.Context, serverID, backupID int64) (*Backup, error) {
	var w wire.Backup
	if err := s.c.do(ctx, "GET", backupPath(serverID, backupID), nil, nil, &w); err != nil {
		return nil, err
	}
	b := backupFromWire(w)
	return &b, nil
}

// Create queues a backup now; if one is already queued or running, that one is returned.
func (s *BackupsService) Create(ctx context.Context, serverID int64) (*Backup, error) {
	var w wire.Backup
	if err := s.c.do(ctx, "POST", serverPath(serverID, "/backups"), nil, nil, &w); err != nil {
		return nil, err
	}
	b := backupFromWire(w)
	return &b, nil
}

// Delete deletes a backup and returns the delete action.
func (s *BackupsService) Delete(ctx context.Context, serverID, backupID int64) (*Action, error) {
	return actionReply(ctx, s.c, "DELETE", backupPath(serverID, backupID), nil)
}

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
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/riverqueue/river"
)

const (
	KindCreateBackup = "create_backup"
	KindDeleteBackup = "delete_backup"
)

// CreateBackupArgs is the create_backup job for a queued row; its action starts once the job holds the node slot.
type CreateBackupArgs struct {
	BackupID int64 `json:"backup_id"`
}

func (CreateBackupArgs) Kind() string { return KindCreateBackup }

// DeleteBackupArgs is the delete_backup job.
type DeleteBackupArgs struct {
	BackupID int64 `json:"backup_id"`
	ServerID int64 `json:"server_id"`
	ActionID int64 `json:"action_id"`
}

func (DeleteBackupArgs) Kind() string                 { return KindDeleteBackup }
func (DeleteBackupArgs) InsertOpts() river.InsertOpts { return servers.InsertOpts() }

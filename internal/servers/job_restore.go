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

import "github.com/riverqueue/river"

const KindRestoreBackup = "restore_backup"

// RestoreServerArgs is the restore_backup job; the archive was chosen at request time.
type RestoreServerArgs struct {
	ServerID int64 `json:"server_id"`
	ActionID int64 `json:"action_id"`
	BackupID int64 `json:"backup_id"`
	// Archive is the backup's volume id in Proxmox.
	Archive string `json:"archive"`
	// StartAfter is whether the server was running when the restore was requested.
	StartAfter bool `json:"start_after"`
	// InstanceGeneration pins the cloud-init instance-id generation; 0 uses the new ISO generation.
	InstanceGeneration int `json:"instance_generation,omitempty"`
}

func (RestoreServerArgs) Kind() string                 { return KindRestoreBackup }
func (RestoreServerArgs) InsertOpts() river.InsertOpts { return insertOpts() }

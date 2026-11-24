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

import { formatDate } from '@/lib/format'
import type { Snapshot } from '@/api/types'

export const restoreText = (serverName: string, s: Snapshot) =>
  `Restore ${serverName} to ${s.name}? Changes since ${formatDate(s.created)} are lost. Volumes stay attached.`

export const identityWarning = (serverName: string) =>
  `${serverName}'s network changed since this snapshot, so it will come back with new SSH host keys and run its user data again.`

export const takeWarning = (serverName: string) =>
  `${serverName} is running. Nebla will shut it down, save its disk and start it again. Expect a few minutes offline.`

export const deletedSource = (name: string) => `${name} (deleted)`

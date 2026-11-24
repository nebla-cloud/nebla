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

import type { components } from '@/api/schema'

export type Backup = components['schemas']['Backup']

export function makeBackup(over: Partial<Backup> = {}): Backup {
  return {
    id: 1,
    server: 5,
    kind: 'scheduled',
    status: 'available',
    size: 2 * 1024 ** 3,
    created: '2026-11-22T02:10:00Z',
    completed: '2026-11-22T02:14:00Z',
    restore_resets_identity: false,
    ...over,
  }
}

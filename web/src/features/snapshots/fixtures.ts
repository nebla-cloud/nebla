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

import type { Snapshot } from '@/api/types'

export { makeLocation } from '@/features/volumes/fixtures'

export function makeSnapshot(over: Partial<Snapshot> = {}): Snapshot {
  return {
    id: 1,
    name: 'before-upgrade',
    description: '',
    labels: {},
    size: 20,
    location: { id: 1, name: 'home', description: '' },
    server: 5,
    server_name: 'web1',
    image: { id: 1, name: 'Debian 13' },
    os_flavor: 'debian',
    os_version: '13',
    status: 'available',
    restore_keeps_identity: true,
    created: '2026-11-18T10:00:00Z',
    ...over,
  }
}

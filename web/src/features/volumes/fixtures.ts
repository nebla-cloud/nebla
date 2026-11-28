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

import type { Location, Volume } from '@/api/types'

export function makeVolume(over: Partial<Volume> = {}): Volume {
  return {
    id: 1,
    name: 'data',
    labels: {},
    size: 20,
    location: { id: 1, name: 'home', description: '' },
    server: null,
    status: 'available',
    linux_device: '/dev/disk/by-id/virtio-nb-vol-1',
    created: '2026-11-18T10:00:00Z',
    protection: { delete: false },
    ...over,
  }
}

export function makeLocation(over: Partial<Location> = {}): Location {
  return {
    id: 1,
    name: 'home',
    description: '',
    networks: { ipv4: true, ipv6: false },
    volumes_available: true,
    backups_available: false,
    private_networks_available: false,
    snapshots_available: true,
    ...over,
  }
}

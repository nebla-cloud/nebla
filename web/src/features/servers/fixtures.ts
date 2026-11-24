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

import type { Server } from '@/api/types'

export function makeServer(over: Partial<Server> = {}): Server {
  return {
    id: 1,
    name: 'web1',
    status: 'running',
    location: { id: 1, name: 'home', description: '' },
    server_type: {
      id: 1,
      name: 'small-2',
      description: '',
      cores: 2,
      memory_mb: 4096,
      disk_gb: 40,
      locations: [1],
      deprecated: false,
    },
    image: {
      id: 1,
      name: 'Debian 13',
      description: '',
      os_flavor: 'debian',
      os_version: '13',
      locations: [1],
    },
    public_net: { ipv4: { id: 1, ip: '203.0.113.10' }, ipv6: null, floating_ips: [] },
    labels: {},
    disk_gb: 40,
    firewalls: [],
    volumes: [],
    backups: { enabled: false, keep: 7, count: 0, last_backup_at: null, next_backup_at: null },
    private_net: [],
    created: '2026-11-18T10:00:00Z',
    ip_change_reruns_cloud_init: false,
    ...over,
  }
}

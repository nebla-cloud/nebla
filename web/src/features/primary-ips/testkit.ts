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

import { http } from 'msw'
import type { Location, PrimaryIp, Server } from '@/api/types'
import { json, server } from '@/test/server'

export { button, fill, settle } from '@/features/volumes/testkit'

const meta = (n: number) => ({ pagination: { page: 1, per_page: 50, total_entries: n } })

export function serveLists(d: {
  locations?: Location[]
  servers?: Server[]
  primaryIps?: PrimaryIp[]
}) {
  server.use(
    http.get('*/v1/projects/:id', () => json(200, { id: 7, name: 'Home', role: 'owner' })),
    http.get('*/v1/locations', () =>
      json(200, { locations: d.locations ?? [], meta: meta(d.locations?.length ?? 0) }),
    ),
    http.get('*/v1/servers', () =>
      json(200, { servers: d.servers ?? [], meta: meta(d.servers?.length ?? 0) }),
    ),
    http.get('*/v1/primary_ips', () =>
      json(200, { primary_ips: d.primaryIps ?? [], meta: meta(d.primaryIps?.length ?? 0) }),
    ),
  )
}

export function action(command: string, status = 'running', id = 50) {
  return {
    id,
    command,
    status,
    progress: status === 'running' ? 0 : 100,
    resources: [
      { id: 1, type: 'primary_ip' },
      { id: 5, type: 'server' },
    ],
    error: null,
    started: '2026-11-23T10:00:00Z',
    finished: status === 'running' ? null : '2026-11-23T10:00:20Z',
  }
}

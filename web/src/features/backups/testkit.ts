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
import type { Action } from '@/api/types'
import { makeServer } from '@/features/servers/fixtures'
import { json, server } from '@/test/server'
import type { Backup } from './fixtures'

export { button, settle } from '../volumes/testkit'

export function backupAction(command: string, id = 60): Action {
  return {
    id,
    command,
    status: 'running',
    progress: 0,
    resources: [{ id: 5, type: 'server' }],
    error: null,
    started: '2026-11-24T10:00:00Z',
    finished: null,
  }
}

export interface Calls {
  list: number
  posts: string[]
  deletes: string[]
  restores: unknown[]
  puts: unknown[]
}

// Serves one server's backups and records the writes.
export function serveBackups(getBackups: () => Backup[], calls: Calls) {
  server.use(
    http.get('*/v1/projects/:id', () => json(200, { id: 7, name: 'Home', role: 'owner' })),
    http.get('*/v1/servers/:id/backups', () => {
      calls.list++
      return json(200, { backups: getBackups() })
    }),
    http.post('*/v1/servers/:id/backups', ({ params }) => {
      calls.posts.push(String(params.id))
      return json(202, getBackups()[0])
    }),
    http.delete('*/v1/servers/:id/backups/:backup', ({ params }) => {
      calls.deletes.push(String(params.backup))
      return json(200, { action: backupAction('delete_backup') })
    }),
    http.post('*/v1/servers/:id/actions/restore_backup', async ({ request }) => {
      calls.restores.push(await request.json())
      return json(201, { action: backupAction('restore_backup') })
    }),
    http.put('*/v1/servers/:id', async ({ request }) => {
      const body = (await request.json()) as { backups: { enabled: boolean; keep: number } }
      calls.puts.push(body)
      const base = makeServer({ id: 5 })
      return json(200, { ...base, backups: { ...base.backups, ...body.backups } })
    }),
    http.get('*/v1/actions/:id', () => json(200, backupAction('restore_backup'))),
  )
}

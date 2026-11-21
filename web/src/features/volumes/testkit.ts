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

import { flushPromises } from '@vue/test-utils'
import { http } from 'msw'
import type { Location, Server, Volume } from '@/api/types'
import { json, server } from '@/test/server'

export async function settle() {
  for (let i = 0; i < 5; i++) {
    await new Promise((r) => setTimeout(r, 10))
    await flushPromises()
  }
}

export function button(text: string): HTMLButtonElement {
  const b = Array.from(document.body.querySelectorAll('button')).find(
    (x) => x.textContent?.trim() === text,
  )
  if (!b) throw new Error(`button ${text} not found`)
  return b
}

export async function fill(selector: string, value: string) {
  const el = document.body.querySelector(selector) as HTMLInputElement | HTMLSelectElement
  if (!el) throw new Error(`${selector} not found`)
  el.value = value
  el.dispatchEvent(new Event(el instanceof HTMLSelectElement ? 'change' : 'input'))
  await flushPromises()
}

const meta = (n: number) => ({ pagination: { page: 1, per_page: 50, total_entries: n } })

export function serveLists(d: { locations?: Location[]; servers?: Server[]; volumes?: Volume[] }) {
  server.use(
    http.get('*/v1/projects/:id', () => json(200, { id: 7, name: 'Home', role: 'owner' })),
    http.get('*/v1/locations', () =>
      json(200, { locations: d.locations ?? [], meta: meta(d.locations?.length ?? 0) }),
    ),
    http.get('*/v1/servers', () =>
      json(200, { servers: d.servers ?? [], meta: meta(d.servers?.length ?? 0) }),
    ),
    http.get('*/v1/volumes', () =>
      json(200, { volumes: d.volumes ?? [], meta: meta(d.volumes?.length ?? 0) }),
    ),
  )
}

export function runningAction(command: string, id = 50) {
  return {
    id,
    command,
    status: 'running',
    progress: 0,
    resources: [{ id: 1, type: 'volume' }],
    error: null,
    started: '2026-11-20T10:00:00Z',
    finished: null,
  }
}

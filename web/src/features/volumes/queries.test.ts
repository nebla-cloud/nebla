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

import { describe, expect, it } from 'vitest'
import { http } from 'msw'
import { setProjectSource } from '@/api/client'
import { json, server } from '@/test/server'
import { makeVolume } from './fixtures'
import { fetchVolumes } from './queries'

describe('fetchVolumes', () => {
  it('reads every page from the same project if the route changes', async () => {
    const all = Array.from({ length: 120 }, (_, i) => makeVolume({ id: i + 1, name: `v${i + 1}` }))
    const seen: (string | null)[] = []
    let currentProject = 1
    setProjectSource(() => currentProject)
    server.use(
      http.get('*/v1/volumes', ({ request }) => {
        const u = new URL(request.url)
        seen.push(request.headers.get('Project-ID'))
        currentProject = 2
        const page = Number(u.searchParams.get('page'))
        const per = Number(u.searchParams.get('per_page'))
        return json(200, {
          volumes: all.slice((page - 1) * per, page * per),
          meta: { pagination: { page, per_page: per, total_entries: all.length } },
        })
      }),
    )
    try {
      const got = await fetchVolumes(1)
      expect(got.map((v) => v.id)).toEqual(all.map((v) => v.id))
      expect(seen).toEqual(['1', '1', '1'])
    } finally {
      setProjectSource(null)
    }
  })

  it('pages the volumes of one server', async () => {
    const servers: (string | null)[] = []
    server.use(
      http.get('*/v1/volumes', ({ request }) => {
        servers.push(new URL(request.url).searchParams.get('server'))
        return json(200, {
          volumes: [makeVolume({ id: 1 })],
          meta: { pagination: { page: 1, per_page: 50, total_entries: 1 } },
        })
      }),
    )
    expect(await fetchVolumes(1, 5)).toHaveLength(1)
    expect(servers).toEqual(['5'])
  })
})

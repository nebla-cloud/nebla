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
import { defineComponent, h } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import { navItems } from '@/router/nav'
import { testQueryClient } from '@/test/mount'
import { json, server } from '@/test/server'
import { setProjectSource } from '@/api/client'
import { anySnapshots, snapshotsNavHidden, trackAvailability } from './available'
import { makeLocation } from './fixtures'
import { settle } from './testkit'

const Stub = defineComponent({ render: () => h('div') })

describe('snapshots nav entry', () => {
  it('anySnapshots needs one location with snapshots', () => {
    expect(anySnapshots(undefined)).toBeUndefined()
    expect(anySnapshots([makeLocation({ snapshots_available: false })])).toBe(false)
    expect(
      anySnapshots([makeLocation({ snapshots_available: false }), makeLocation({ id: 2 })]),
    ).toBe(true)
  })

  async function setup(pid: number, available: boolean) {
    const qc = testQueryClient()
    trackAvailability(qc)
    server.use(
      http.get('*/v1/locations', () =>
        json(200, { locations: [makeLocation({ snapshots_available: available })], meta: {} }),
      ),
    )
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        {
          path: '/p/:projectId/snapshots',
          name: 'snapshots',
          component: Stub,
          meta: {
            side: 'project',
            nav: { label: 'Snapshots', order: 13, hidden: (r) => snapshotsNavHidden(r, qc) },
          },
        },
      ],
    })
    setProjectSource(() => pid)
    await router.push(`/p/${pid}/snapshots`)
    return router
  }

  it('shows once a location offers snapshots', async () => {
    const router = await setup(61, true)
    expect(navItems(router, 'project').map((i) => i.label)).not.toContain('Snapshots')
    await settle()
    expect(navItems(router, 'project').map((i) => i.label)).toContain('Snapshots')
  })

  it('stays hidden when no location offers snapshots', async () => {
    const router = await setup(62, false)
    await settle()
    expect(navItems(router, 'project').map((i) => i.label)).not.toContain('Snapshots')
  })
})

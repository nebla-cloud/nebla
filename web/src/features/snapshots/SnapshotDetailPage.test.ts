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

import { afterEach, describe, expect, it } from 'vitest'
import { http } from 'msw'
import { json, server } from '@/test/server'
import { mountApp, testRouter } from '@/test/mount'
import SnapshotDetailPage from './SnapshotDetailPage.vue'
import { makeLocation, makeSnapshot } from './fixtures'
import type { Snapshot } from '@/api/types'
import { serveLists, settle } from './testkit'
import UTooltip from '@nuxt/ui/components/Tooltip.vue'

let unmount: () => void
afterEach(() => {
  unmount()
  document.body.innerHTML = ''
})

async function mountPage(snap: Snapshot) {
  serveLists({ locations: [makeLocation()], snapshots: [snap] })
  server.use(
    http.get('*/v1/snapshots/:id', () => json(200, snap)),
    http.get('*/v1/snapshots/:id/actions', () =>
      json(200, {
        actions: [],
        meta: { pagination: { page: 1, per_page: 25, total_entries: 0 } },
      }),
    ),
  )
  const router = testRouter([
    { path: '/p/:projectId/snapshots', name: 'snapshots', component: SnapshotDetailPage },
    {
      path: '/p/:projectId/snapshots/:snapshotId',
      name: 'snapshot',
      component: SnapshotDetailPage,
    },
    { path: '/p/:projectId/servers/:serverId', name: 'server', component: SnapshotDetailPage },
    { path: '/p/:projectId/servers/new', name: 'server-new', component: SnapshotDetailPage },
  ])
  await router.push('/p/7/snapshots/1')
  const m = mountApp(SnapshotDetailPage, { router })
  unmount = () => m.wrapper.unmount()
  await settle()
  return m
}

const buttonTexts = () =>
  Array.from(document.body.querySelectorAll('button')).map((b) => b.textContent?.trim())

const deleteButton = () =>
  Array.from(document.body.querySelectorAll('button')).find(
    (b) => b.textContent?.trim() === 'Delete' && !b.closest('[role="dialog"]'),
  ) as HTMLButtonElement

describe('SnapshotDetailPage', () => {
  it('shows the details and the actions', async () => {
    await mountPage(makeSnapshot({ name: 'before-upgrade', description: 'pre 2.0' }))
    const text = document.body.textContent ?? ''
    expect(text).toContain('before-upgrade')
    expect(text).toContain('pre 2.0')
    expect(text).toContain('20 GB')
    expect(buttonTexts()).toContain('Restore')
    expect(buttonTexts()).toContain('Delete')
  })

  it('labels a deleted source and hides restore', async () => {
    await mountPage(makeSnapshot({ server: null, server_name: 'web1' }))
    expect(document.body.textContent).toContain('web1 (deleted)')
    expect(buttonTexts()).not.toContain('Restore')
  })

  it('disables Delete with a lock and tooltip while delete protection is on', async () => {
    const snap = makeSnapshot({ protection: { delete: true } })
    const { wrapper } = await mountPage(snap)
    const del = deleteButton()
    expect(del.disabled).toBe(true)
    expect(del.innerHTML).toContain('M7 11V7a5 5 0 0 1 10 0v4')
    expect(wrapper.findAllComponents(UTooltip).map((t) => t.props('text'))).toContain(
      'Turn off delete protection first.',
    )
  })

  it('leaves Delete alone while delete protection is off', async () => {
    const snap = makeSnapshot({ protection: { delete: false } })
    const { wrapper } = await mountPage(snap)
    expect(deleteButton().disabled).toBe(false)
    expect(wrapper.findAllComponents(UTooltip).map((t) => t.props('text'))).not.toContain(
      'Turn off delete protection first.',
    )
  })
})

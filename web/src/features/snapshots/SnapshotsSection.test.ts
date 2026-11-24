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

import { afterEach, describe, expect, it, vi } from 'vitest'
import { http } from 'msw'
import { makeServer } from '@/features/servers/fixtures'
import { json, server } from '@/test/server'
import { mountApp, testRouter } from '@/test/mount'
import SnapshotsSection from './SnapshotsSection.vue'
import { makeLocation, makeSnapshot } from './fixtures'
import { button, runningAction, serveLists, settle } from './testkit'

let unmount: () => void
afterEach(() => {
  unmount()
  document.body.innerHTML = ''
})

const onStarted = vi.fn()

async function mountSection(snaps = [makeSnapshot({ id: 1, name: 'before-upgrade', server: 1 })]) {
  serveLists({ locations: [makeLocation()], snapshots: snaps })
  const seen: URL[] = []
  server.use(
    http.get('*/v1/snapshots', ({ request }) => {
      seen.push(new URL(request.url))
      return json(200, {
        snapshots: snaps,
        meta: { pagination: { page: 1, per_page: 50, total_entries: snaps.length } },
      })
    }),
  )
  const router = testRouter([
    { path: '/p/:projectId/snapshots/:snapshotId', name: 'snapshot', component: SnapshotsSection },
  ])
  await router.push('/p/7/snapshots/1')
  const m = mountApp(SnapshotsSection, {
    router,
    props: { server: makeServer({ id: 1 }), onStarted },
  })
  unmount = () => m.wrapper.unmount()
  await settle()
  return { ...m, seen }
}

describe('SnapshotsSection', () => {
  it("lists this server's snapshots with restore", async () => {
    const { seen } = await mountSection()
    expect(document.body.textContent).toContain('before-upgrade')
    expect(document.body.querySelector('button[aria-label="Restore before-upgrade"]')).toBeTruthy()
    expect(seen.some((u) => u.searchParams.get('server') === '1')).toBe(true)
  })

  it('opens the take dialog', async () => {
    await mountSection([])
    expect(document.body.textContent).toContain('No snapshots of this server')
    button('Take snapshot').click()
    await settle()
    expect(document.body.querySelector('input[name=name]')).toBeTruthy()
  })

  it('hands a restore to the server page to track', async () => {
    const act = runningAction('rebuild_server', 77)
    server.use(http.post('*/v1/servers/1/actions/rebuild', () => json(201, { action: act })))
    await mountSection()
    ;(
      document.body.querySelector(
        'button[aria-label="Restore before-upgrade"]',
      ) as HTMLButtonElement
    ).click()
    await settle()
    Array.from(document.body.querySelectorAll('button'))
      .filter((b) => b.textContent?.trim() === 'Restore')
      .at(-1)!
      .click()
    await settle()
    expect(onStarted).toHaveBeenCalledWith(expect.objectContaining({ id: 77 }))
  })
})

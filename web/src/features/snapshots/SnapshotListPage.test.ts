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
import { makeServer } from '@/features/servers/fixtures'
import { mountApp, testRouter } from '@/test/mount'
import SnapshotListPage from './SnapshotListPage.vue'
import { makeLocation, makeSnapshot } from './fixtures'
import { serveLists, settle } from './testkit'

let unmount: () => void
afterEach(() => {
  unmount()
  document.body.innerHTML = ''
})

async function mountPage() {
  const router = testRouter([
    { path: '/p/:projectId/snapshots', name: 'snapshots', component: SnapshotListPage },
    { path: '/p/:projectId/snapshots/:snapshotId', name: 'snapshot', component: SnapshotListPage },
    { path: '/p/:projectId/servers/:serverId', name: 'server', component: SnapshotListPage },
    { path: '/p/:projectId/servers/new', name: 'server-new', component: SnapshotListPage },
  ])
  await router.push('/p/7/snapshots')
  const m = mountApp(SnapshotListPage, { router })
  unmount = () => m.wrapper.unmount()
  await settle()
  return m
}

describe('SnapshotListPage', () => {
  it('shows an empty state', async () => {
    serveLists({ locations: [makeLocation()], snapshots: [] })
    await mountPage()
    expect(document.body.textContent).toContain('No snapshots yet')
  })

  it('explains when no location offers snapshots', async () => {
    serveLists({ locations: [makeLocation({ snapshots_available: false })], snapshots: [] })
    await mountPage()
    expect(document.body.textContent).toContain("Snapshots aren't available here yet")
  })

  it('lists rows with size, source server, location and status', async () => {
    serveLists({
      locations: [makeLocation()],
      servers: [makeServer({ id: 5, name: 'web1' })],
      snapshots: [
        makeSnapshot({ id: 1, name: 'live', size: 20, server: 5 }),
        makeSnapshot({ id: 2, name: 'orphan', size: 40, server: null, server_name: 'old1' }),
      ],
    })
    const { wrapper } = await mountPage()
    const rows = wrapper.findAll('li')
    expect(rows).toHaveLength(2)
    expect(rows[0]!.text()).toContain('live')
    expect(rows[0]!.text()).toContain('20 GB')
    expect(rows[0]!.text()).toContain('web1')
    expect(rows[0]!.text()).toContain('home')
    expect(rows[0]!.find('[role="img"]').attributes('aria-label')).toBe('Available')
    expect(rows[0]!.find('a').attributes('href')).toBe('/p/7/snapshots/1')
    expect(rows[1]!.text()).toContain('old1 (deleted)')
  })

  it('offers restore only while the source server exists', async () => {
    serveLists({
      locations: [makeLocation()],
      servers: [makeServer({ id: 5, name: 'web1' })],
      snapshots: [
        makeSnapshot({ id: 1, name: 'live', server: 5 }),
        makeSnapshot({ id: 2, name: 'orphan', server: null }),
      ],
    })
    const { wrapper } = await mountPage()
    const rows = wrapper.findAll('li')
    expect(rows[0]!.find('button[aria-label="Restore live"]').exists()).toBe(true)
    expect(rows[1]!.find('button[aria-label="Restore orphan"]').exists()).toBe(false)
  })

  it('marks protected rows with a lock', async () => {
    serveLists({
      locations: [makeLocation()],
      snapshots: [
        makeSnapshot({ id: 1, name: 'safe', protection: { delete: true } }),
        makeSnapshot({ id: 2, name: 'open' }),
      ],
    })
    const { wrapper } = await mountPage()
    const rows = wrapper.findAll('li')
    expect(rows[0]!.find('[aria-label="Protected"]').exists()).toBe(true)
    expect(rows[1]!.find('[aria-label="Protected"]').exists()).toBe(false)
  })

  it('keeps Restore enabled for a rebuild-protected server', async () => {
    serveLists({
      locations: [makeLocation()],
      servers: [makeServer({ id: 5, name: 'web1', protection: { delete: false, rebuild: true } })],
      snapshots: [makeSnapshot({ id: 1, name: 'live', server: 5 })],
    })
    await mountPage()
    const restore = document.body.querySelector(
      'button[aria-label="Restore live"]',
    ) as HTMLButtonElement
    expect(restore.disabled).toBe(false)
  })

  it('disables Delete in the compact menu with a hint while protected', async () => {
    serveLists({
      locations: [makeLocation()],
      snapshots: [makeSnapshot({ id: 1, name: 'safe', protection: { delete: true } })],
    })
    await mountPage()
    ;(
      document.body.querySelector('button[aria-label="More actions for safe"]') as HTMLElement
    ).dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
    await settle()
    const item = Array.from(document.body.querySelectorAll('[role="menuitem"]')).find((i) =>
      i.textContent?.includes('Delete'),
    ) as HTMLElement
    expect(item.getAttribute('aria-disabled') ?? item.getAttribute('data-disabled')).not.toBeNull()
    expect(item.textContent).toContain('Turn off delete protection first.')
  })
})

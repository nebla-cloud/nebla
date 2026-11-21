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

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { http } from 'msw'
import type { Volume } from '@/api/types'
import { makeServer } from '@/features/servers/fixtures'
import { json, server } from '@/test/server'
import { mountApp, testRouter } from '@/test/mount'
import VolumesSection from './VolumesSection.vue'
import { makeLocation, makeVolume } from './fixtures'
import { button, runningAction, settle } from './testkit'

let onServer: Volume[]
let all: Volume[]
let listUrls: URL[]
let calls: { path: string; body?: unknown }[]
let unmount: () => void

const meta = { pagination: { page: 1, per_page: 50, total_entries: 0 } }

beforeEach(() => {
  onServer = [makeVolume({ id: 1, name: 'data', status: 'attached', server: 5 })]
  all = [...onServer, makeVolume({ id: 2, name: 'spare' })]
  listUrls = []
  calls = []
  server.use(
    http.get('*/v1/projects/:id', () => json(200, { id: 7, name: 'Home', role: 'owner' })),
    http.get('*/v1/locations', () => json(200, { locations: [makeLocation()], meta })),
    http.get('*/v1/servers', () => json(200, { servers: [], meta })),
    http.get('*/v1/volumes', ({ request }) => {
      const u = new URL(request.url)
      listUrls.push(u)
      return json(200, { volumes: u.searchParams.get('server') ? onServer : all, meta })
    }),
    http.post('*/v1/volumes/:id/actions/:verb', async ({ request, params }) => {
      const text = await request.text()
      calls.push({
        path: `${String(params.id)}/${String(params.verb)}`,
        body: text ? JSON.parse(text) : undefined,
      })
      return json(201, { action: runningAction(`${String(params.verb)}_volume`) })
    }),
    http.get('*/v1/actions/:id', () => json(200, runningAction('detach_volume'))),
  )
})

afterEach(() => {
  unmount()
  document.body.innerHTML = ''
})

async function mountTab() {
  const router = testRouter([
    {
      path: '/p/:projectId/servers/:serverId/volumes',
      name: 'server-volumes',
      component: VolumesSection,
    },
    { path: '/p/:projectId/volumes/:volumeId', name: 'volume', component: VolumesSection },
  ])
  await router.push('/p/7/servers/5/volumes')
  const m = mountApp(VolumesSection, {
    router,
    props: { server: makeServer({ id: 5, volumes: [1] }) },
  })
  unmount = () => m.wrapper.unmount()
  await settle()
  return m
}

describe('VolumesSection', () => {
  it('lists the attached volumes by server', async () => {
    await mountTab()
    expect(listUrls.some((u) => u.searchParams.get('server') === '5')).toBe(true)
    const rows = document.body.querySelectorAll('[data-testid="server-volume"]')
    expect(rows).toHaveLength(1)
    expect(rows[0]!.textContent).toContain('data')
    expect(rows[0]!.textContent).toContain('20 GB')
    expect(rows[0]!.querySelector('[data-testid="mount-hint"]')!.textContent).toContain(
      'sudo mount /dev/disk/by-id/virtio-nb-vol-1 /mnt/data',
    )
  })

  it('copies the device path', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })
    await mountTab()
    button('Copy').click()
    await settle()
    expect(writeText).toHaveBeenCalledWith('/dev/disk/by-id/virtio-nb-vol-1')
    expect(button('Copied')).toBeTruthy()
  })

  it('detaches only after confirming', async () => {
    await mountTab()
    button('Detach').click()
    await settle()
    expect(calls).toEqual([])
    expect(document.body.textContent).toContain('Detach data?')
    const confirm = Array.from(document.body.querySelectorAll('[role="dialog"] button')).find(
      (b) => b.textContent?.trim() === 'Detach',
    ) as HTMLButtonElement
    confirm.click()
    await settle()
    expect(calls).toEqual([{ path: '1/detach', body: undefined }])
    expect(document.body.textContent).toContain('Detaching volume')
  })

  it('attaches a spare volume from the same location', async () => {
    await mountTab()
    button('Attach volume').click()
    await settle()
    document.body.querySelector<HTMLFormElement>('#attach-to-server')!.requestSubmit()
    await settle()
    expect(calls).toEqual([{ path: '2/attach', body: { server: '5' } }])
  })

  it('hides create while the location is loading or offers no volumes', async () => {
    onServer = []
    let release: () => void = () => {}
    const gate = new Promise<void>((r) => (release = r))
    server.use(
      http.get('*/v1/locations', async () => {
        await gate
        return json(200, { locations: [makeLocation({ volumes_available: false })], meta })
      }),
    )
    await mountTab()
    expect(document.body.textContent).toContain('No volumes attached')
    expect(() => button('Create volume')).toThrow()
    release()
    await settle()
    expect(document.body.textContent).toContain("Volumes aren't available here yet")
    expect(() => button('Create volume')).toThrow()
  })

  it('shows an empty state with create', async () => {
    onServer = []
    await mountTab()
    expect(document.body.textContent).toContain('No volumes attached')
    expect(button('Create volume')).toBeTruthy()
  })
})

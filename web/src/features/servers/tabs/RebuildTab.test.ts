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

import { computed, defineComponent, h } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { http } from 'msw'
import { RouterView } from 'vue-router'
import { json, server } from '@/test/server'
import { mountApp, testRouter } from '@/test/mount'
import type { Action, Server } from '@/api/types'
import { makeServer } from '../fixtures'
import { serverBusyKey } from '../shell'
import RebuildTab from './RebuildTab.vue'

const img = (id: number, name: string, locations: number[]) => ({
  id,
  name,
  description: '',
  os_flavor: 'debian',
  os_version: '13',
  locations,
})
const images = [img(1, 'Debian 13', [1]), img(3, 'Ubuntu 24.04', [1]), img(5, 'Far image', [2])]
const act: Action = {
  id: 9,
  command: 'rebuild_server',
  status: 'running',
  progress: 0,
  resources: [{ id: 1, type: 'server' }],
  error: null,
  started: '2026-11-20T10:00:00Z',
  finished: null,
}

const Busy = defineComponent({
  setup: () => () => h(RebuildTab, { server: makeServer() as Server }),
  provide: { [serverBusyKey as symbol]: computed(() => true) },
})

const snap = (id: number, name: string, over: Record<string, unknown> = {}) => ({
  id,
  name,
  description: '',
  labels: {},
  size: 20,
  location: { id: 1, name: 'home', description: '' },
  server: 1,
  server_name: 'web1',
  image: null,
  os_flavor: 'debian',
  os_version: '13',
  status: 'available',
  restore_keeps_identity: true,
  created: '2026-11-18T10:00:00Z',
  ...over,
})
let snapshots: ReturnType<typeof snap>[] = []

async function mountTab(busy = false) {
  const router = testRouter([{ path: '/p/:projectId', component: RouterView }])
  await router.push('/p/1')
  const onStarted = vi.fn()
  server.use(
    http.get('*/v1/locations', () =>
      json(200, {
        locations: [
          {
            id: 1,
            name: 'home',
            description: '',
            networks: { ipv4: true, ipv6: true },
            snapshots_available: true,
          },
        ],
        meta: { pagination: { page: 1, per_page: 50, total_entries: 1 } },
      }),
    ),
    http.get('*/v1/snapshots', () =>
      json(200, {
        snapshots,
        meta: { pagination: { page: 1, per_page: 50, total_entries: snapshots.length } },
      }),
    ),
    http.get('*/v1/images', () =>
      json(200, { images, meta: { pagination: { page: 1, per_page: 50, total_entries: 3 } } }),
    ),
  )
  const res = mountApp(busy ? Busy : RebuildTab, {
    router,
    props: busy ? {} : { server: makeServer(), onStarted },
  })
  await vi.waitFor(() => expect(res.wrapper.text()).toContain('Ubuntu 24.04'))
  return { ...res, onStarted }
}

const buttons = () => Array.from(document.body.querySelectorAll('button'))
const byText = (t: string) => buttons().filter((b) => b.textContent?.trim() === t)

beforeEach(() => {
  snapshots = []
  document.body.innerHTML = ''
})
afterEach(() => {
  document.body.innerHTML = ''
})

describe('RebuildTab', () => {
  it('busy server cannot start a rebuild', async () => {
    await mountTab(true)
    expect(byText('Rebuild')[0]!.disabled).toBe(true)
  })

  it("lists images for the server's location", async () => {
    const { wrapper } = await mountTab()
    const radios = Array.from(
      (wrapper.element as Element).querySelectorAll<HTMLInputElement>('input[type=radio]'),
    )
    expect(radios).toHaveLength(3)
    expect(radios.map((r) => r.disabled)).toEqual([false, false, true])
    expect(wrapper.text()).toContain('Not available in home')
  })

  it('images are radio choice cards named by the image', async () => {
    const { wrapper } = await mountTab()
    const radios = Array.from(
      (wrapper.element as Element).querySelectorAll<HTMLInputElement>('input[type=radio]'),
    )
    const cards = radios.map((r) => r.closest('label.nb-choice')!)
    expect(radios.map((r) => r.name)).toEqual(['image', 'image', 'image'])
    expect(cards.map((c) => c.getAttribute('data-variant'))).toEqual([
      'default',
      'default',
      'default',
    ])
    expect(cards[1]!.textContent).toContain('Ubuntu 24.04')
    expect(cards[1]!.querySelector('[aria-hidden=true]')!.textContent).toBe('D')
    expect(cards[2]!.textContent).toContain('Not available in home')
  })

  it('warning copy', async () => {
    const { wrapper } = await mountTab()
    expect(wrapper.text()).toContain(
      'Rebuilding erases the disk and reinstalls from the image you pick. The IP addresses stay the same.',
    )
  })

  it('requires typing the name', async () => {
    await mountTab()
    byText('Rebuild')[0]!.click()
    await vi.waitFor(() =>
      expect(document.body.querySelector('input[name=confirm_name]')).toBeTruthy(),
    )
    const confirm = byText('Rebuild').at(-1)!
    expect(confirm.disabled).toBe(true)
    const input = document.body.querySelector('input[name=confirm_name]') as HTMLInputElement
    input.value = 'web1'
    input.dispatchEvent(new Event('input'))
    await vi.waitFor(() => expect(byText('Rebuild').at(-1)!.disabled).toBe(false))
  })

  it('posts rebuild and emits started', async () => {
    let body: unknown
    server.use(
      http.post('*/v1/servers/1/actions/rebuild', async ({ request }) => {
        body = await request.json()
        return json(201, { action: act })
      }),
    )
    const { wrapper, onStarted } = await mountTab()
    const radios = (wrapper.element as Element).querySelectorAll<HTMLInputElement>(
      'input[type=radio]',
    )
    radios[1]!.click()
    await flushPromises()
    byText('Rebuild')[0]!.click()
    await vi.waitFor(() =>
      expect(document.body.querySelector('input[name=confirm_name]')).toBeTruthy(),
    )
    const input = document.body.querySelector('input[name=confirm_name]') as HTMLInputElement
    input.value = 'web1'
    input.dispatchEvent(new Event('input'))
    await vi.waitFor(() => expect(byText('Rebuild').at(-1)!.disabled).toBe(false))
    byText('Rebuild').at(-1)!.click()
    await vi.waitFor(() => expect(onStarted).toHaveBeenCalledWith(act))
    expect(body).toEqual({ image: '3' })
  })

  it("lists only this location's available snapshots that fit the disk", async () => {
    snapshots = [
      snap(1, 'fits'),
      snap(2, 'too-big', { size: 80 }),
      snap(3, 'elsewhere', { location: { id: 2, name: 'rack-2', description: '' } }),
      snap(4, 'busy', { status: 'creating' }),
    ]
    const { wrapper } = await mountTab()
    await vi.waitFor(() => expect(wrapper.text()).toContain('fits'))
    const names = Array.from(
      (wrapper.element as Element).querySelectorAll<HTMLInputElement>('input[name=snapshot]'),
    ).map((r) => r.closest('label')!.textContent)
    expect(names).toHaveLength(1)
    expect(names[0]).toContain('fits')
  })

  it('restoring the own snapshot shows the restore copy and identity warning', async () => {
    snapshots = [
      snap(1, 'mine', { restore_keeps_identity: false }),
      snap(2, 'other', { server: 9 }),
    ]
    let body: unknown
    server.use(
      http.post('*/v1/servers/1/actions/rebuild', async ({ request }) => {
        body = await request.json()
        return json(201, { action: act })
      }),
    )
    const { wrapper, onStarted } = await mountTab()
    await vi.waitFor(() => expect(wrapper.text()).toContain('mine'))
    const radio = (wrapper.element as Element).querySelector<HTMLInputElement>(
      'input[name=snapshot][value="1"]',
    )!
    radio.click()
    await flushPromises()
    expect(wrapper.text()).toContain('Restore web1 to mine?')
    expect(wrapper.text()).toContain('new SSH host keys and run its user data again')
    byText('Rebuild')[0]!.click()
    await vi.waitFor(() =>
      expect(document.body.querySelector('input[name=confirm_name]')).toBeTruthy(),
    )
    const input = document.body.querySelector('input[name=confirm_name]') as HTMLInputElement
    input.value = 'web1'
    input.dispatchEvent(new Event('input'))
    await vi.waitFor(() => expect(byText('Rebuild').at(-1)!.disabled).toBe(false))
    byText('Rebuild').at(-1)!.click()
    await vi.waitFor(() => expect(onStarted).toHaveBeenCalledWith(act))
    expect(body).toEqual({ snapshot: '1' })
  })

  it('shows the API error when the snapshot cannot be used on this host', async () => {
    snapshots = [snap(2, 'other', { server: 9 })]
    server.use(
      http.post('*/v1/servers/1/actions/rebuild', () =>
        json(422, {
          error: {
            code: 'snapshot_server_mismatch',
            message: "This snapshot can't be used on this server's host.",
          },
        }),
      ),
    )
    const { wrapper } = await mountTab()
    await vi.waitFor(() => expect(wrapper.text()).toContain('other'))
    ;(wrapper.element as Element)
      .querySelector<HTMLInputElement>('input[name=snapshot][value="2"]')!
      .click()
    await flushPromises()
    expect(wrapper.text()).not.toContain('Restore web1')
    byText('Rebuild')[0]!.click()
    await vi.waitFor(() =>
      expect(document.body.querySelector('input[name=confirm_name]')).toBeTruthy(),
    )
    const input = document.body.querySelector('input[name=confirm_name]') as HTMLInputElement
    input.value = 'web1'
    input.dispatchEvent(new Event('input'))
    await vi.waitFor(() => expect(byText('Rebuild').at(-1)!.disabled).toBe(false))
    byText('Rebuild').at(-1)!.click()
    await vi.waitFor(() => expect(wrapper.text()).toContain("can't be used on this server's host"))
  })
})

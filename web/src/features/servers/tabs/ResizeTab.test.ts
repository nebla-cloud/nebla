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
import { describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { http } from 'msw'
import { RouterView } from 'vue-router'
import { json, server } from '@/test/server'
import { mountApp, testRouter } from '@/test/mount'
import type { Action, Server, ServerType } from '@/api/types'
import { makeServer } from '../fixtures'
import { serverBusyKey } from '../shell'
import { checkboxClass } from '@/lib/ui'
import ResizeTab from './ResizeTab.vue'

const t = (id: number, cores: number, mem: number, disk: number): ServerType => ({
  id,
  name: `t${id}`,
  description: '',
  cores,
  memory_mb: mem,
  disk_gb: disk,
  locations: [1],
  deprecated: false,
})
const types = [t(1, 2, 4096, 40), t(4, 4, 8192, 80)]
const act: Action = {
  id: 9,
  command: 'change_server_type',
  status: 'running',
  progress: 0,
  resources: [{ id: 1, type: 'server' }],
  error: null,
  started: '2026-11-20T10:00:00Z',
  finished: null,
}

const Busy = defineComponent({
  props: { server: { type: Object, required: true } },
  emits: ['started'],
  provide: { [serverBusyKey as symbol]: computed(() => true) },
  setup:
    (p, { emit }) =>
    () =>
      h(ResizeTab, { server: p.server as Server, onStarted: (a: Action) => emit('started', a) }),
})

async function mountTab(s: Server, busy = false) {
  const router = testRouter([
    {
      path: '/p/:projectId',
      component: RouterView,
      children: [{ path: 'servers/:serverId', name: 'server', component: RouterView }],
    },
  ])
  await router.push('/p/1')
  const onStarted = vi.fn()
  server.use(
    http.get('*/v1/server_types', () =>
      json(200, {
        server_types: types,
        meta: { pagination: { page: 1, per_page: 50, total_entries: 2 } },
      }),
    ),
  )
  const res = mountApp(busy ? Busy : ResizeTab, { router, props: { server: s, onStarted } })
  await vi.waitFor(() => expect(res.wrapper.text()).toContain('t4'))
  root = res.wrapper.element
  return { ...res, onStarted }
}

let root: Element
const button = (t: string) =>
  Array.from(root.querySelectorAll('button')).find((b) => b.textContent?.trim() === t)

describe('ResizeTab', () => {
  it('busy server cannot submit or shut down', async () => {
    const { wrapper } = await mountTab(makeServer({ status: 'off' }), true)
    ;(wrapper.element as Element)
      .querySelectorAll<HTMLInputElement>('input[type=radio]')[1]!
      .click()
    await flushPromises()
    expect(button('Resize')!.disabled).toBe(true)
  })

  it('busy running server disables shut down', async () => {
    await mountTab(makeServer({ status: 'running' }), true)
    expect(button('Shut down')!.disabled).toBe(true)
  })

  it('error and stopping states explain and link to Overview', async () => {
    const a = await mountTab(makeServer({ status: 'error' }))
    expect(a.wrapper.text()).toContain("Fix the server's state from Overview")
    expect(a.wrapper.find('a').attributes('href')).toBe('/p/1/servers/1')
    a.wrapper.unmount()
    const b = await mountTab(makeServer({ status: 'stopping' }))
    expect(b.wrapper.text()).toContain("The server is stopping. Resizing unlocks once it's off.")
  })

  it('running server cannot submit', async () => {
    let posted = false
    server.use(
      http.post('*/v1/servers/1/actions/change_type', () => {
        posted = true
        return json(201, { action: act })
      }),
    )
    const { wrapper } = await mountTab(makeServer({ status: 'running' }))
    expect(wrapper.text()).toContain('Shut the server down to resize it')
    expect(button('Shut down')).toBeTruthy()
    ;(wrapper.element as Element)
      .querySelectorAll<HTMLInputElement>('input[type=radio]')[1]!
      .click()
    await flushPromises()
    expect(button('Resize')!.disabled).toBe(true)
    button('Resize')!.click()
    await flushPromises()
    expect(posted).toBe(false)
  })

  it('types are arch choice cards and the note sits beside shut down', async () => {
    const { wrapper } = await mountTab(makeServer({ status: 'running' }))
    const el = wrapper.element as Element
    const radios = Array.from(el.querySelectorAll<HTMLInputElement>('input[type=radio]'))
    const cards = radios.map((r) => r.closest('label.nb-choice')!)
    expect(radios.map((r) => r.name)).toEqual(['type', 'type'])
    expect(cards.map((c) => c.getAttribute('data-variant'))).toEqual(['arch', 'arch'])
    expect(cards[1]!.textContent).toContain('t4')
    expect(cards[1]!.textContent).toContain('4 vCPU')
    const note = Array.from(el.querySelectorAll('*')).find(
      (n) => n.children.length === 0 && n.textContent === 'Shut the server down to resize it.',
    )!
    expect(note.parentElement!.contains(button('Shut down')!)).toBe(true)
    expect(note.parentElement!.querySelector('button')).toBe(button('Shut down'))
  })

  it('grow disk is a native checkbox with the shared style', async () => {
    const { wrapper } = await mountTab(makeServer({ status: 'off' }))
    const box = (wrapper.element as Element).querySelector<HTMLInputElement>(
      'input[type=checkbox][name=upgrade_disk]',
    )!
    expect(box.className).toBe(checkboxClass)
  })

  it('shut down posts and emits started', async () => {
    server.use(
      http.post('*/v1/servers/1/actions/shutdown', () =>
        json(201, { action: { ...act, command: 'shutdown_server' } }),
      ),
    )
    const { onStarted } = await mountTab(makeServer({ status: 'running' }))
    button('Shut down')!.click()
    await vi.waitFor(() => expect(onStarted).toHaveBeenCalled())
  })

  it('off server submits', async () => {
    let body: unknown
    server.use(
      http.post('*/v1/servers/1/actions/change_type', async ({ request }) => {
        body = await request.json()
        return json(201, { action: act })
      }),
    )
    const { wrapper, onStarted } = await mountTab(makeServer({ status: 'off' }))
    ;(wrapper.element as Element)
      .querySelectorAll<HTMLInputElement>('input[type=radio]')[1]!
      .click()
    await flushPromises()
    button('Resize')!.click()
    await vi.waitFor(() => expect(onStarted).toHaveBeenCalledWith(act))
    expect(body).toEqual({ server_type: '4', upgrade_disk: false })
  })

  it('grow disk sends true and copy shows', async () => {
    let body: unknown
    server.use(
      http.post('*/v1/servers/1/actions/change_type', async ({ request }) => {
        body = await request.json()
        return json(201, { action: act })
      }),
    )
    const { wrapper, onStarted } = await mountTab(makeServer({ status: 'off' }))
    expect(wrapper.text()).toContain(
      "Disks never shrink. If you grow it, you can't move back to a type with a smaller disk.",
    )
    ;(wrapper.element as Element).querySelector<HTMLInputElement>('input[type=checkbox]')!.click()
    await flushPromises()
    ;(wrapper.element as Element)
      .querySelectorAll<HTMLInputElement>('input[type=radio]')[1]!
      .click()
    await flushPromises()
    button('Resize')!.click()
    await vi.waitFor(() => expect(onStarted).toHaveBeenCalled())
    expect(body).toEqual({ server_type: '4', upgrade_disk: true })
  })

  it('api error shown', async () => {
    server.use(
      http.post('*/v1/servers/1/actions/change_type', () =>
        json(422, { error: { code: 'disk_shrink_not_allowed', message: 'Disks cannot shrink' } }),
      ),
    )
    const { wrapper } = await mountTab(makeServer({ status: 'off' }))
    ;(wrapper.element as Element)
      .querySelectorAll<HTMLInputElement>('input[type=radio]')[1]!
      .click()
    await flushPromises()
    button('Resize')!.click()
    await vi.waitFor(() => expect(wrapper.text()).toContain('Disks cannot shrink'))
  })
})

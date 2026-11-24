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
import { flushPromises, type VueWrapper } from '@vue/test-utils'
import { http } from 'msw'
import { json, server } from '@/test/server'
import { defineComponent, h, type Component } from 'vue'
import { RouterView, type HistoryState } from 'vue-router'
import { useToast } from '@nuxt/ui/composables/useToast'
import { mountApp, testRouter } from '@/test/mount'
import TopBar from '@/layouts/TopBar.vue'
import type { Action, Server } from '@/api/types'
import { makeServer } from './fixtures'
import routes, { builtInTabs, withServerTabs } from './routes'

function action(over: Partial<Action> = {}): Action {
  return {
    id: 7,
    command: 'reboot_server',
    status: 'running',
    progress: 40,
    resources: [{ id: 1, type: 'server' }],
    error: null,
    started: '2026-11-20T10:00:00Z',
    finished: null,
    ...over,
  }
}

const emptyList = { servers: [], meta: { pagination: { page: 1, per_page: 50, total_entries: 0 } } }

const locationList = (backups: boolean) => ({
  locations: [
    {
      id: 1,
      name: 'home',
      description: '',
      networks: { ipv4: true, ipv6: false },
      volumes_available: true,
      backups_available: backups,
    },
  ],
  meta: { pagination: { page: 1, per_page: 50, total_entries: 1 } },
})

function serve(s: Server, backups = false) {
  server.use(
    http.get('*/v1/locations', () => json(200, locationList(backups))),
    http.get('*/v1/projects/1', () =>
      json(200, { id: 1, name: 'Home lab', description: '', created: '2026-11-01T00:00:00Z' }),
    ),
    http.get(`*/v1/servers/${s.id}`, () => json(200, s)),
  )
}

let mounted: VueWrapper | undefined

const WithTopBar = defineComponent({ render: () => h('div', [h(TopBar), h(RouterView)]) })

async function mountDetail(
  path = '/p/1/servers/1',
  state?: HistoryState,
  root: Component = RouterView,
) {
  const router = testRouter([
    {
      path: '/p/:projectId',
      component: RouterView,
      children: [
        { path: 'servers/new', name: 'server-new', component: RouterView },
        { path: 'ssh-keys', name: 'ssh-keys', component: RouterView },
        ...routes,
      ],
    },
  ])
  await router.push({ path, state })
  const res = mountApp(root, { router })
  mounted = res.wrapper
  await vi.waitFor(() => expect(res.wrapper.text()).not.toContain('Loading server'))
  await flushPromises()
  return { ...res, router }
}

const buttons = () => Array.from(document.body.querySelectorAll('button'))
const byText = (t: string) => buttons().find((b) => b.textContent?.trim() === t)
const toastText = () => document.body.textContent ?? ''

async function openMore() {
  ;(
    document.body.querySelector('button[aria-label="More power actions"]') as HTMLElement
  ).dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
  await vi.waitFor(() => expect(document.body.querySelector('[role="menuitem"]')).toBeTruthy())
}

beforeEach(() => {
  document.body.innerHTML = ''
})
afterEach(() => {
  // Shared toasts and live polls must not outlive the test.
  mounted?.vm.$.appContext.app.runWithContext(() => useToast().clear())
  mounted?.unmount()
  mounted = undefined
  vi.useRealTimers()
  document.body.innerHTML = ''
})

describe('ServerDetailShell', () => {
  it('starting a rebuild returns to Overview with progress', async () => {
    serve(makeServer({ id: 5 }))
    server.use(
      http.get('*/v1/images', () =>
        json(200, {
          images: [makeServer().image],
          meta: { pagination: { page: 1, per_page: 50, total_entries: 1 } },
        }),
      ),
      http.post('*/v1/servers/5/actions/rebuild', () =>
        json(201, { action: action({ id: 8, command: 'rebuild_server' }) }),
      ),
      http.get('*/v1/actions/8', () =>
        json(200, action({ id: 8, command: 'rebuild_server', status: 'success', progress: 100 })),
      ),
    )
    const { wrapper, router } = await mountDetail('/p/1/servers/5/rebuild')
    await vi.waitFor(() => expect(wrapper.text()).toContain('Debian 13'))
    const rebuild = () =>
      Array.from(document.body.querySelectorAll('button')).filter(
        (b) => b.textContent?.trim() === 'Rebuild',
      )
    rebuild()[0]!.click()
    await vi.waitFor(() =>
      expect(document.body.querySelector('input[name=confirm_name]')).toBeTruthy(),
    )
    const input = document.body.querySelector('input[name=confirm_name]') as HTMLInputElement
    input.value = 'web1'
    input.dispatchEvent(new Event('input'))
    await vi.waitFor(() => expect(rebuild().at(-1)!.disabled).toBe(false))
    rebuild().at(-1)!.click()
    await vi.waitFor(() => expect(router.currentRoute.value.name).toBe('server'))
    await vi.waitFor(() => expect(wrapper.text()).toContain('Rebuilding'))
  })

  it('resize form enables once shutdown completes', async () => {
    let off = false
    server.use(
      http.get('*/v1/projects/1', () =>
        json(200, { id: 1, name: 'Home lab', description: '', created: '2026-11-01T00:00:00Z' }),
      ),
      http.get('*/v1/servers/5', () => {
        return json(200, makeServer({ id: 5, status: off ? 'off' : 'running' }))
      }),
      http.get('*/v1/servers', () => json(200, emptyList)),
      http.get('*/v1/server_types', () =>
        json(200, {
          server_types: [
            makeServer().server_type,
            { ...makeServer().server_type, id: 4, name: 'big', cores: 4, disk_gb: 80 },
          ],
          meta: { pagination: { page: 1, per_page: 50, total_entries: 2 } },
        }),
      ),
      http.post('*/v1/servers/5/actions/shutdown', () =>
        json(201, {
          action: action({
            id: 6,
            command: 'shutdown_server',
            resources: [{ id: 5, type: 'server' }],
          }),
        }),
      ),
      http.get('*/v1/actions/6', () => {
        off = true
        return json(
          200,
          action({
            id: 6,
            command: 'shutdown_server',
            resources: [{ id: 5, type: 'server' }],
            status: 'success',
            progress: 100,
            finished: '2026-11-20T10:01:00Z',
          }),
        )
      }),
    )
    const { wrapper } = await mountDetail('/p/1/servers/5/resize')
    await vi.waitFor(() => expect(wrapper.text()).toContain('big'))
    const root = wrapper.element as Element
    const resize = () =>
      Array.from(root.querySelectorAll('button')).find((b) => b.textContent?.trim() === 'Resize')!
    expect(resize().disabled).toBe(true)
    Array.from(root.querySelectorAll('button'))
      .find((b) => b.textContent?.trim() === 'Shut down')!
      .click()
    await vi.waitFor(() =>
      expect(wrapper.text()).not.toContain('Shut the server down to resize it'),
    )
    root.querySelectorAll<HTMLInputElement>('input[type=radio]')[1]!.click()
    await vi.waitFor(() => expect(resize().disabled).toBe(false))
  })

  it('child route renders its tab', async () => {
    serve(makeServer({ id: 5 }))
    const { wrapper } = await mountDetail('/p/1/servers/5/rebuild')
    expect(wrapper.text()).toContain('Rebuilding erases the disk')
    const current = wrapper.find('nav[aria-label="Server sections"] a[aria-current="page"]')
    expect(current.text()).toBe('Rebuild')
  })

  it('tab bar links keep the server', async () => {
    serve(makeServer({ id: 5 }))
    const { wrapper } = await mountDetail('/p/1/servers/5')
    const links = wrapper
      .findAll('nav[aria-label="Server sections"] a')
      .map((a) => [a.text(), a.attributes('href')])
    expect(links).toEqual([
      ['Overview', '/p/1/servers/5'],
      ['Graphs', '/p/1/servers/5/graphs'],
      ['Console', '/p/1/servers/5/console'],
      ['Rebuild', '/p/1/servers/5/rebuild'],
      ['Firewalls', '/p/1/servers/5/firewalls'],
      ['Resize', '/p/1/servers/5/resize'],
      ['Volumes', '/p/1/servers/5/volumes'],
      ['Activity', '/p/1/servers/5/activity'],
    ])
  })

  it('builds tabs from child routes with serverTab meta, sorted by order', async () => {
    serve(makeServer({ id: 5 }))
    const firewalls = {
      path: 'firewalls',
      name: 'server-firewalls',
      component: RouterView,
      meta: { side: 'project' as const, serverTab: { label: 'Firewalls', order: 45 } },
    }
    const withFirewalls = routes.map((r) =>
      'children' in r
        ? {
            ...r,
            children: withServerTabs(builtInTabs, {
              '../firewalls/serverTabs.ts': { default: [firewalls] },
            }),
          }
        : r,
    )
    const router = testRouter([
      {
        path: '/p/:projectId',
        component: RouterView,
        children: [
          { path: 'servers/new', name: 'server-new', component: RouterView },
          ...withFirewalls,
        ],
      },
    ])
    await router.push('/p/1/servers/5')
    const res = mountApp(RouterView, { router })
    mounted = res.wrapper
    await vi.waitFor(() => expect(res.wrapper.text()).not.toContain('Loading server'))
    const labels = res.wrapper.findAll('nav[aria-label="Server sections"] a').map((a) => a.text())
    expect(labels).toEqual([
      'Overview',
      'Graphs',
      'Console',
      'Rebuild',
      'Firewalls',
      'Resize',
      'Activity',
    ])
  })

  it('shows the Backups tab only where the location offers backups', async () => {
    const labels = (w: VueWrapper) =>
      w.findAll('nav[aria-label="Server sections"] a').map((a) => a.text())
    serve(makeServer({ id: 5 }), false)
    const off = await mountDetail('/p/1/servers/5')
    expect(labels(off.wrapper)).not.toContain('Backups')
    off.wrapper.unmount()
    document.body.innerHTML = ''
    serve(makeServer({ id: 5 }), true)
    const on = await mountDetail('/p/1/servers/5')
    await vi.waitFor(() => expect(labels(on.wrapper)).toContain('Backups'))
    const tabs = labels(on.wrapper)
    expect(tabs.indexOf('Backups')).toBe(tabs.indexOf('Volumes') + 1)
    const link = on.wrapper.findAll('nav a').find((a) => a.text() === 'Backups')!
    expect(link.attributes('href')).toBe('/p/1/servers/5/backups')
  })

  it('the status chip says Restoring while a restore_backup action runs', async () => {
    serve(makeServer({ id: 5 }), true)
    server.use(
      http.get('*/v1/actions/3', () =>
        json(
          200,
          action({ id: 3, command: 'restore_backup', resources: [{ id: 5, type: 'server' }] }),
        ),
      ),
    )
    const { wrapper } = await mountDetail('/p/1/servers/5', { actionId: 3 })
    const header = wrapper.findAll('header').find((h) => h.find('h1').exists())!
    await vi.waitFor(() => expect(header.text()).toContain('Restoring'))
    expect(header.text()).not.toContain('Running')
  })

  it('the tab strip scrolls sideways only', async () => {
    serve(makeServer({ id: 5 }))
    const { wrapper } = await mountDetail('/p/1/servers/5')
    const nav = wrapper.get('nav[aria-label="Server sections"]')
    expect(nav.classes()).toContain('overflow-x-auto')
    expect(nav.classes()).toContain('overflow-y-hidden')
    for (const a of nav.findAll('a')) expect(a.classes()).not.toContain('-mb-px')
  })

  it('header and chips stay across tabs', async () => {
    serve(makeServer({ id: 5 }))
    const { wrapper, router } = await mountDetail('/p/1/servers/5')
    for (const tab of ['', '/graphs', '/console', '/rebuild', '/resize', '/activity']) {
      await router.push(`/p/1/servers/5${tab}`)
      await flushPromises()
      expect(wrapper.find('h1').text()).toBe('web1')
      expect(wrapper.text()).toContain('Running')
      expect(byText('Shut down')).toBeTruthy()
      expect(byText('Reboot')).toBeTruthy()
    }
  })

  it('breadcrumb links back to the server list', async () => {
    serve(makeServer())
    const { wrapper } = await mountDetail('/p/1/servers/1', undefined, WithTopBar)
    const navs = wrapper.findAll('nav[aria-label="Breadcrumb"]')
    expect(navs).toHaveLength(1)
    expect(navs[0]!.element.closest('[data-topbar]')).toBeTruthy()
    const links = navs[0]!.findAll('a')
    expect(links.map((a) => [a.text(), a.attributes('href')])).toEqual([
      ['Servers', '/p/1/servers'],
    ])
    expect(navs[0]!.find('[aria-current="page"]').text()).toBe('web1')
  })

  it('header holds the title, status badge, console link and power actions', async () => {
    serve(makeServer({ labels: { env: 'prod' } }))
    const { wrapper } = await mountDetail()
    const headers = wrapper.findAll('header').filter((h) => h.find('h1').exists())
    expect(headers).toHaveLength(1)
    const header = headers[0]!
    expect(header.find('h1').text()).toBe('web1')
    expect(header.find('svg').exists()).toBe(true)
    expect(header.text()).toContain('Running')
    const console = header.findAll('a').find((a) => a.text() === 'Console')
    expect(console?.attributes('href')).toBe('/p/1/servers/1/console')
    expect(header.findAll('button').map((b) => b.text())).toContain('Reboot')
    expect(wrapper.find('ul[aria-label="Labels"]').text()).toContain('env=prod')
  })

  it('a subtitle names the OS, type, location and created date', async () => {
    serve(makeServer({ image: { ...makeServer().image, name: 'debian-13' } }))
    const { wrapper } = await mountDetail()
    const header = wrapper.findAll('header').find((h) => h.find('h1').exists())!
    expect(header.find('p').text()).toBe('Debian 13 on small-2 in home, created 18 Nov 2026')
  })

  it('a 63-character name truncates in a one-column header at 390px', async () => {
    window.innerWidth = 390
    const name = 'a'.repeat(63)
    serve(makeServer({ name }))
    const { wrapper } = await mountDetail()
    const h1 = wrapper.find('header h1')
    expect(h1.text()).toBe(name)
    expect(h1.classes()).toContain('truncate')
    expect(h1.classes()).toContain('min-w-0')
    const header = h1.element.closest('header')!
    expect(header.classList.contains('max-sm:flex-col')).toBe(true)
    expect(header.classList.contains('max-sm:items-stretch')).toBe(true)
  })

  it('action progress sits in a panel', async () => {
    serve(makeServer())
    server.use(
      http.post('*/v1/servers/1/actions/reboot', () => json(201, { action: action() })),
      http.get('*/v1/actions/7', () => json(200, action())),
    )
    const { wrapper } = await mountDetail()
    byText('Reboot')!.click()
    await vi.waitFor(() => expect(wrapper.find('[role="status"]').exists()).toBe(true))
    const panel = wrapper.find('[role="status"]').element.parentElement!
    expect(panel.classList.contains('rounded-(--nb-radius-panel)')).toBe(true)
  })

  it('handoff ignored for another server', async () => {
    serve(makeServer({ id: 5 }))
    let polled = false
    server.use(
      http.get('*/v1/actions/3', () => {
        polled = true
        return json(
          200,
          action({ id: 3, command: 'create_server', resources: [{ id: 9, type: 'server' }] }),
        )
      }),
    )
    const { wrapper, router } = await mountDetail('/p/1/servers/5', { actionId: 3 })
    await flushPromises()
    await new Promise((r) => setTimeout(r, 50))
    expect(polled).toBe(true)
    expect(wrapper.find('[role="status"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('Creating')
    expect(router.options.history.state.actionId ?? null).toBeNull()
  })

  it('create shows created toast', async () => {
    serve(makeServer({ id: 5, status: 'initializing' }))
    let polls = 0
    server.use(
      http.get('*/v1/actions/3', () => {
        polls++
        const done = polls > 1
        return json(
          200,
          action({
            id: 3,
            command: 'create_server',
            resources: [{ id: 5, type: 'server' }],
            ...(done ? { status: 'success', progress: 100, finished: '2026-11-20T10:01:00Z' } : {}),
          }),
        )
      }),
      http.get('*/v1/servers', () => json(200, emptyList)),
    )
    const { wrapper, router } = await mountDetail('/p/1/servers/5', { actionId: 3 })
    await vi.waitFor(() => expect(wrapper.text()).toContain('Creating'))
    expect(router.options.history.state.actionId ?? null).toBeNull()
    await vi.waitFor(() => expect(toastText()).toContain('web1 created'), { timeout: 4000 })
  })

  it('detail power actions by status', async () => {
    serve(
      makeServer({
        status: 'running',
        public_net: { ipv4: { ip: '203.0.113.10' }, ipv6: { ip: '2001:db8::10' }, floating_ips: [] },
      }),
    )
    const { wrapper } = await mountDetail()
    expect(wrapper.text()).toContain('2001:db8::10')
    expect(wrapper.text()).toContain('2 vCPU, 4 GB, 40 GB')
    expect(byText('Shut down')).toBeTruthy()
    expect(byText('Reboot')).toBeTruthy()
    expect(byText('Power on')).toBeUndefined()
    expect(byText('Power off')).toBeUndefined()
    await openMore()
    const items = Array.from(document.body.querySelectorAll('[role="menuitem"]')).map((e) =>
      e.textContent?.trim(),
    )
    expect(items).toEqual(['Power off', 'Reset'])
  })

  it('off server shows Power on only', async () => {
    serve(makeServer({ status: 'off' }))
    await mountDetail()
    expect(byText('Power on')).toBeTruthy()
    expect(byText('Shut down')).toBeUndefined()
    expect(byText('Reboot')).toBeUndefined()
    expect(document.body.querySelector('button[aria-label="More power actions"]')).toBeNull()
  })

  it('error server offers Power on and Power off', async () => {
    serve(makeServer({ status: 'error' }))
    await mountDetail()
    expect(byText('Power on')!.disabled).toBe(false)
    expect(byText('Power off')!.disabled).toBe(false)
    expect(byText('Shut down')).toBeUndefined()
  })

  it('missing server shows Power on disabled with a reason', async () => {
    serve(makeServer({ status: 'missing' }))
    await mountDetail()
    const on = byText('Power on')!
    expect(on.disabled).toBe(true)
    expect(on.getAttribute('title')).toBe("Nebla can't find this server right now")
    expect(byText('Power off')).toBeUndefined()
  })

  it('transitional status disables every power action', async () => {
    serve(makeServer({ status: 'starting' }))
    await mountDetail()
    expect(byText('Shut down')!.disabled).toBe(true)
    expect(byText('Reboot')!.disabled).toBe(true)
    const more = document.body.querySelector(
      'button[aria-label="More power actions"]',
    ) as HTMLButtonElement
    expect(more.disabled).toBe(true)
  })

  it('reboot shows inline progress then toast', async () => {
    serve(makeServer())
    let polls = 0
    server.use(
      http.post('*/v1/servers/1/actions/reboot', () => json(201, { action: action() })),
      http.get('*/v1/actions/7', () => {
        polls++
        return json(
          200,
          action(
            polls > 1 ? { status: 'success', progress: 100, finished: '2026-11-20T10:00:02Z' } : {},
          ),
        )
      }),
    )
    const { wrapper } = await mountDetail()
    byText('Reboot')!.click()
    await vi.waitFor(() => expect(wrapper.text()).toContain('Rebooting'))
    expect(wrapper.find('[role="status"]').exists()).toBe(true)
    await vi.waitFor(() => expect(toastText()).toContain('web1 rebooted'), { timeout: 4000 })
  })

  it('failed action shows its message, stops polling and re-enables buttons', async () => {
    serve(makeServer())
    let polls = 0
    server.use(
      http.post('*/v1/servers/1/actions/reboot', () => json(201, { action: action() })),
      http.get('*/v1/actions/7', () => {
        polls++
        return json(
          200,
          action({
            status: 'error',
            error: { code: 'failed', message: 'Proxmox refused to reboot' },
            finished: '2026-11-20T10:00:02Z',
          }),
        )
      }),
    )
    const { wrapper } = await mountDetail()
    byText('Reboot')!.click()
    await vi.waitFor(() => expect(wrapper.text()).toContain('Proxmox refused to reboot'))
    const seen = polls
    vi.useFakeTimers()
    try {
      await vi.advanceTimersByTimeAsync(10_000)
    } finally {
      vi.useRealTimers()
    }
    expect(polls).toBe(seen)
    expect(byText('Shut down')!.disabled).toBe(false)
    expect(byText('Reboot')!.disabled).toBe(false)
  })

  it('server poll pauses while an action runs and resumes after', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    let serverCalls = 0
    let actionCalls = 0
    let done = false
    server.use(
      http.get('*/v1/projects/1', () =>
        json(200, { id: 1, name: 'Home lab', description: '', created: '2026-11-01T00:00:00Z' }),
      ),
      http.get('*/v1/servers/1', () => {
        serverCalls++
        return json(200, makeServer({ status: 'starting' }))
      }),
      http.get('*/v1/servers', () => json(200, emptyList)),
      http.get('*/v1/actions/3', () => {
        actionCalls++
        return json(
          200,
          action({
            id: 3,
            ...(done ? { status: 'success', progress: 100, finished: '2026-11-20T10:01:00Z' } : {}),
          }),
        )
      }),
    )
    const { wrapper } = await mountDetail('/p/1/servers/1', { actionId: 3 })
    await vi.waitFor(() => expect(wrapper.text()).toContain('Rebooting'))
    const seen = serverCalls
    const polls = actionCalls
    await vi.advanceTimersByTimeAsync(10_000)
    expect(actionCalls).toBeGreaterThan(polls)
    expect(serverCalls).toBe(seen)
    done = true
    await vi.advanceTimersByTimeAsync(2_500)
    await vi.waitFor(() => expect(serverCalls).toBe(seen + 1))
    await vi.advanceTimersByTimeAsync(3_500)
    expect(serverCalls).toBeGreaterThanOrEqual(seen + 2)
  })

  it('a power action shows on the Activity tab without waiting for a poll', async () => {
    serve(makeServer())
    let started = false
    server.use(
      http.post('*/v1/servers/1/actions/reboot', () => {
        started = true
        return json(201, { action: action() })
      }),
      http.get('*/v1/actions/7', () => json(200, action())),
      http.get('*/v1/servers/1/actions', () =>
        json(200, {
          actions: started ? [action()] : [],
          meta: { pagination: { page: 1, per_page: 25, total_entries: started ? 1 : 0 } },
        }),
      ),
    )
    const { wrapper } = await mountDetail('/p/1/servers/1/activity')
    await vi.waitFor(() => expect(wrapper.text()).toContain('Nothing has happened here yet'))
    byText('Reboot')!.click()
    await vi.waitFor(() => expect(wrapper.text()).toContain('Running 40%'), { timeout: 1000 })
  })

  it('power buttons lock as soon as an action starts', async () => {
    serve(makeServer())
    server.use(
      http.post('*/v1/servers/1/actions/reboot', () => json(201, { action: action() })),
      http.get('*/v1/actions/7', () => new Promise<never>(() => {})),
    )
    await mountDetail()
    byText('Reboot')!.click()
    await vi.waitFor(() => expect(byText('Reboot')!.disabled).toBe(true))
    await flushPromises()
    await new Promise((r) => setTimeout(r, 50))
    expect(byText('Reboot')!.disabled).toBe(true)
    expect(byText('Shut down')!.disabled).toBe(true)
  })

  it('delete requires typing the name and returns to list', async () => {
    serve(makeServer())
    server.use(
      http.delete('*/v1/servers/1', () =>
        json(200, {
          action: action({ id: 8, command: 'delete_server', status: 'success', progress: 100 }),
        }),
      ),
      http.get('*/v1/actions/8', () =>
        json(200, action({ id: 8, command: 'delete_server', status: 'success', progress: 100 })),
      ),
      http.get('*/v1/servers', () => json(200, emptyList)),
    )
    const { router } = await mountDetail()
    byText('Delete server')!.click()
    await flushPromises()
    const confirm = () =>
      buttons()
        .filter((b) => b.textContent?.trim() === 'Delete server')
        .at(-1)!
    expect(confirm().disabled).toBe(true)
    const input = document.body.querySelector('input[name="confirm_name"]') as HTMLInputElement
    input.value = 'web1'
    input.dispatchEvent(new Event('input'))
    await flushPromises()
    expect(confirm().disabled).toBe(false)
    confirm().click()
    await vi.waitFor(() => expect(router.currentRoute.value.name).toBe('servers'))
    expect(toastText()).toContain('web1 deleted')
  })

  it('rename sends PUT name and validates pattern locally', async () => {
    serve(makeServer())
    const bodies: unknown[] = []
    server.use(
      http.put('*/v1/servers/1', async ({ request }) => {
        const body = (await request.json()) as { name: string }
        bodies.push(body)
        return json(200, makeServer({ name: body.name }))
      }),
      http.get('*/v1/servers', () => json(200, emptyList)),
    )
    const { wrapper } = await mountDetail()
    byText('Rename')!.click()
    await flushPromises()
    const input = wrapper.find('input[name="name"]')
    await input.setValue('Web_1')
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(bodies).toHaveLength(0)
    expect(wrapper.text()).toContain('lowercase letters')
    await input.setValue('web-2')
    await wrapper.find('form').trigger('submit')
    await vi.waitFor(() => expect(bodies).toEqual([{ name: 'web-2' }]))
    await vi.waitFor(() => expect(wrapper.text()).toContain('web-2'))
  })
})

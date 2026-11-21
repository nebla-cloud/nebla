import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, h } from 'vue'
import { flushPromises } from '@vue/test-utils'
import { RouterView, type RouteRecordRaw } from 'vue-router'
import { mountApp, testQueryClient, testRouter } from '@/test/mount'
import { server } from '@/test/server'
import { resetSession, setMe } from '@/lib/session'
import { readLastProject } from '@/lib/project'
import type { Project, Role } from '@/api/types'
import { makeServer } from '@/features/servers/fixtures'
import CommandPalette from './CommandPalette.vue'

const Stub = defineComponent({ render: () => h('div') })
const host = (side: 'project' | 'admin') =>
  defineComponent({ render: () => h('div', [h(CommandPalette, { side }), h(RouterView)]) })

const routes: RouteRecordRaw[] = [
  {
    path: '/p/:projectId',
    component: host('project'),
    meta: { side: 'project' },
    children: [
      {
        path: 'servers',
        name: 'servers',
        component: Stub,
        meta: { side: 'project', nav: { label: 'Servers', order: 10 } },
      },
      { path: 'servers/:serverId', name: 'server', component: Stub },
      {
        path: 'ssh-keys',
        name: 'ssh-keys',
        component: Stub,
        meta: { side: 'project', nav: { label: 'SSH keys', order: 20, group: 'Access' } },
      },
      {
        path: 'tokens',
        name: 'tokens',
        component: Stub,
        meta: {
          side: 'project',
          roles: ['owner', 'admin'],
          nav: { label: 'API tokens', order: 25, group: 'Access' },
        },
      },
    ],
  },
  {
    path: '/admin',
    component: host('admin'),
    meta: { side: 'admin' },
    children: [
      {
        path: 'locations',
        name: 'admin-locations',
        component: Stub,
        meta: { side: 'admin', nav: { label: 'Locations', order: 10 } },
      },
      { path: 'projects/:projectId', name: 'admin-project', component: Stub },
    ],
  },
]

const project = (role: Role) =>
  ({ id: 1, name: 'Home lab', role, usage: { servers: 0 } }) as unknown as Project

let unmount: (() => void) | null = null
const requests = vi.fn()

async function mountAt(path: string, opts: { role?: Role; admin?: boolean; cache?: boolean } = {}) {
  setMe({
    id: 1,
    email: 'kai@example.com',
    name: 'Kai',
    is_admin: opts.admin ?? false,
    totp_enabled: false,
  })
  const qc = testQueryClient()
  qc.setQueryDefaults(['project'], { staleTime: Infinity })
  qc.setQueryData(['project', 1], project(opts.role ?? 'owner'))
  if (opts.cache !== false) {
    qc.setQueryData(['servers', 1, ''], {
      servers: [makeServer({ id: 7, name: 'web-7' }), makeServer({ id: 8, name: 'db-8' })],
      total: 2,
    })
    qc.setQueryData(
      ['ssh-keys', 1],
      [{ id: 3, name: 'Work laptop', fingerprint: 'aa', public_key: 'ssh-ed25519 AAA' }],
    )
  }
  const router = testRouter(routes)
  await router.push(path)
  const { wrapper } = mountApp(defineComponent({ render: () => h(RouterView) }), {
    router,
    queryClient: qc,
  })
  unmount = () => wrapper.unmount()
  await flushPromises()
  return router
}

function ctrlK(target: EventTarget = document.body): KeyboardEvent {
  const e = new KeyboardEvent('keydown', {
    key: 'k',
    ctrlKey: true,
    bubbles: true,
    cancelable: true,
  })
  target.dispatchEvent(e)
  return e
}

const dialog = () => document.querySelector('[role="dialog"]')
const groupLabels = () =>
  [...document.querySelectorAll('[data-slot="label"]')].map((e) => e.textContent?.trim())
const items = () => [...document.querySelectorAll('[data-slot="item"]')] as HTMLElement[]
const itemLabels = () => items().map((e) => e.textContent?.trim())

async function openPalette() {
  ctrlK()
  await flushPromises()
}

beforeEach(() => {
  // jsdom has no scrollIntoView; the listbox calls it on the highlighted item.
  Element.prototype.scrollIntoView = () => {}
  resetSession()
  document.body.innerHTML = ''
  requests.mockReset()
  server.events.on('request:start', requests)
})

afterEach(() => {
  server.events.removeAllListeners()
  unmount?.()
  unmount = null
})

describe('CommandPalette', () => {
  it('is not in the DOM until opened, then Ctrl+K on the body opens it', async () => {
    await mountAt('/p/1/servers')
    expect(dialog()).toBeNull()
    expect(document.querySelector('[data-slot="input"]')).toBeNull()
    await openPalette()
    expect(dialog()).not.toBeNull()
    expect(groupLabels()).toEqual(['Pages', 'Servers', 'SSH keys'])
  })

  it('stays shut for Ctrl+K in an input or inside the console', async () => {
    await mountAt('/p/1/servers')
    const field = document.createElement('input')
    const console = document.createElement('div')
    console.setAttribute('data-console', '')
    const canvas = document.createElement('canvas')
    console.appendChild(canvas)
    document.body.append(field, console)
    ctrlK(field)
    const e = ctrlK(canvas)
    await flushPromises()
    expect(dialog()).toBeNull()
    expect(e.defaultPrevented).toBe(false)
  })

  it('shows a member no API tokens entry', async () => {
    await mountAt('/p/1/servers', { role: 'member' })
    await openPalette()
    expect(itemLabels()).toContain('SSH keys')
    expect(itemLabels().join('|')).not.toContain('API tokens')
  })

  it('shows an owner the API tokens entry', async () => {
    await mountAt('/p/1/servers', { role: 'owner' })
    await openPalette()
    expect(itemLabels()).toContain('API tokens')
  })

  it('goes to a server and closes when one is chosen', async () => {
    const router = await mountAt('/p/1/servers')
    await openPalette()
    const item = items().find((e) => e.textContent?.includes('web-7'))!
    item.click()
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/p/1/servers/7')
    expect(dialog()).toBeNull()
  })

  it('goes to the keys page from a key', async () => {
    const router = await mountAt('/p/1/servers')
    await openPalette()
    items()
      .find((e) => e.textContent?.includes('Work laptop'))!
      .click()
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/p/1/ssh-keys')
  })

  it('makes no requests when opened, even with nothing cached', async () => {
    await mountAt('/p/1/servers', { cache: false })
    await openPalette()
    expect(dialog()).not.toBeNull()
    expect(groupLabels()).toEqual(['Pages'])
    expect(requests).not.toHaveBeenCalled()
  })

  it('makes no requests when opened over stale cached lists', async () => {
    await mountAt('/p/1/servers')
    await openPalette()
    expect(itemLabels()).toContain('web-7')
    expect(requests).not.toHaveBeenCalled()
  })

  it('adds the admin pages for a panel admin on the project side', async () => {
    await mountAt('/p/1/servers', { admin: true })
    await openPalette()
    expect(groupLabels()).toContain('Admin')
    expect(itemLabels()).toContain('Locations')
  })

  it('lists only admin pages on the admin side', async () => {
    const router = await mountAt('/admin/locations', { admin: true })
    await openPalette()
    expect(groupLabels()).toEqual(['Pages'])
    expect(itemLabels()).toEqual(['Locations'])
    expect(router.currentRoute.value.path).toBe('/admin/locations')
  })

  it('has a Search button that names its shortcut and opens the palette', async () => {
    await mountAt('/p/1/servers')
    const btn = document.querySelector('button[aria-label="Search"]') as HTMLButtonElement
    expect(btn.getAttribute('aria-keyshortcuts')).toBe('Meta+K Control+K')
    expect(btn.textContent).toContain('Search')
    expect(btn.querySelector('kbd')?.textContent?.trim()).toMatch(/^(⌘K|Ctrl K)$/)
    btn.click()
    await flushPromises()
    expect(dialog()).not.toBeNull()
  })

  it('leaves an admin page about a project alone: no last project, no fetch', async () => {
    localStorage.clear()
    await mountAt('/admin/projects/2', { admin: true })
    await openPalette()
    expect(readLastProject()).toBeNull()
    expect(requests).not.toHaveBeenCalled()
  })

  it('closes on Escape', async () => {
    await mountAt('/p/1/servers')
    await openPalette()
    document.activeElement!.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }),
    )
    await flushPromises()
    expect(dialog()).toBeNull()
  })
})

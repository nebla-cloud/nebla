import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { RouterView, createMemoryHistory } from 'vue-router'
import { http } from 'msw'
import { json, server } from '@/test/server'
import { mountApp, testRouter } from '@/test/mount'
import { createAppRouter } from '@/router'
import type { Action } from '@/api/types'
import { makeServer } from '@/features/servers/fixtures'
import serverRoutes from '@/features/servers/routes'
import { useCurrentCrumbs } from '@/lib/crumbs'
import CreateServerPage from './CreateServerPage.vue'

const meta = (n: number) => ({ pagination: { page: 1, per_page: 50, total_entries: n } })
const ED = 'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFake laptop@home'

const locations = [
  { id: 1, name: 'home', description: '', networks: { ipv4: true, ipv6: true } },
  { id: 2, name: 'rack-2', description: '', networks: { ipv4: false, ipv6: true } },
]
const images = [
  {
    id: 3,
    name: 'Debian 13',
    description: '',
    os_flavor: 'debian',
    os_version: '13',
    locations: [1],
  },
  {
    id: 4,
    name: 'Ubuntu 24.04',
    description: '',
    os_flavor: 'ubuntu',
    os_version: '24.04',
    locations: [1, 2],
  },
]
const type = (id: number, name: string, cores: number) => ({
  id,
  name,
  description: '',
  cores,
  memory_mb: cores * 2048,
  disk_gb: cores * 20,
  locations: [],
  deprecated: false,
})
const types = [type(1, 'tiny-1', 1), type(2, 'small-2', 2)]
let firewalls: { id: number; name: string; labels: object; rules: []; applied_to: []; created: string }[]
let keys: { id: number; name: string; public_key: string; fingerprint: string; created: string }[]

function key(id: number, name: string) {
  return { id, name, public_key: ED, fingerprint: `aa:bb:${id}`, created: '2026-11-01T10:00:00Z' }
}

function action(over: Partial<Action> = {}): Action {
  return {
    id: 7,
    command: 'create_server',
    status: 'running',
    progress: 10,
    resources: [{ id: 42, type: 'server' }],
    error: null,
    started: '2026-11-20T10:00:00Z',
    finished: null,
    ...over,
  }
}

function serveCatalog(locs = locations) {
  server.use(
    http.get('*/v1/projects/1', () =>
      json(200, {
        id: 1,
        name: 'Home lab',
        role: 'owner',
        quota: {
          max_servers: 10,
          max_vcpu: 32,
          max_memory_mb: 65536,
          max_disk_gb: 1000,
          max_ips: 10,
        },
        usage: { servers: 3, vcpu: 6, memory_mb: 12288, disk_gb: 120, ips: 3 },
        created: '2026-11-01T00:00:00Z',
      }),
    ),
    http.get('*/v1/locations', () => json(200, { locations: locs, meta: meta(locs.length) })),
    http.get('*/v1/images', () => json(200, { images, meta: meta(images.length) })),
    http.get('*/v1/server_types', () =>
      json(200, { server_types: types, meta: meta(types.length) }),
    ),
    http.get('*/v1/ssh_keys', () => json(200, { ssh_keys: keys, meta: meta(keys.length) })),
    http.get('*/v1/servers', () => json(200, { servers: [], meta: meta(0) })),
    http.get('*/v1/firewalls', () => json(200, { firewalls, meta: meta(firewalls.length) })),
  )
}

async function settle() {
  for (let i = 0; i < 5; i++) {
    await new Promise((r) => setTimeout(r, 10))
    await flushPromises()
  }
}

async function mountCreate() {
  const router = testRouter([
    {
      path: '/p/:projectId',
      component: RouterView,
      children: [
        { path: 'servers/new', name: 'server-new', component: CreateServerPage },
        ...serverRoutes,
      ],
    },
  ])
  await router.push('/p/1/servers/new')
  const res = mountApp(RouterView, { router })
  await settle()
  return { ...res, router }
}

const $ = <T extends Element = HTMLInputElement>(sel: string) =>
  document.body.querySelector(sel) as T
const summary = () => $<HTMLElement>('aside[aria-label="Summary"]')
const createButton = () =>
  Array.from(document.body.querySelectorAll('button')).find(
    (b) => b.textContent?.trim() === 'Create server',
  ) as HTMLButtonElement

const stepFieldset = (title: string) =>
  Array.from(document.body.querySelectorAll('li[data-step]'))
    .find((li) => li.querySelector('h2')?.textContent?.trim() === title)
    ?.querySelector('fieldset')

async function pick(sel: string) {
  $(sel).click()
  await flushPromises()
}

async function type_(sel: string, value: string) {
  const el = $<HTMLInputElement | HTMLTextAreaElement>(sel)
  el.value = value
  el.dispatchEvent(new Event('input'))
  await flushPromises()
}

beforeEach(() => {
  document.body.innerHTML = ''
  keys = [key(5, 'laptop')]
  firewalls = []
})
afterEach(() => {
  document.body.innerHTML = ''
})

describe('CreateServerPage', () => {
  it('server-new resolves before the server detail route', () => {
    const router = createAppRouter(createMemoryHistory())
    expect(router.resolve('/p/1/servers/new').name).toBe('server-new')
    expect(router.resolve('/p/1/servers/42').name).toBe('server')
  })

  it('lays the form out as numbered steps with state', async () => {
    serveCatalog()
    await mountCreate()
    const steps = Array.from(document.body.querySelectorAll<HTMLElement>('form ol > li[data-step]'))
    expect(steps.map((s) => [s.dataset.step, s.querySelector('h2')?.textContent?.trim()])).toEqual([
      ['1', 'Location'],
      ['2', 'Image'],
      ['3', 'Type'],
      ['4', 'Networking'],
      ['5', 'SSH keys'],
      ['6', 'Cloud-init'],
    ])
    // Defaults fill location, image, type and IPv4; no key is picked yet.
    expect(steps.map((s) => s.dataset.state)).toEqual([
      'done',
      'done',
      'done',
      'done',
      'current',
      'todo',
    ])
    for (const s of steps.slice(0, 5)) expect(s.querySelector('fieldset')).toBeTruthy()
    await pick('input[name="ssh_key"][value="5"]')
    expect(steps[4]!.dataset.state).toBe('done')
    expect(steps[5]!.dataset.state).toBe('current')
  })

  it('uses top bar crumbs, choice cards and an add-key card', async () => {
    serveCatalog()
    await mountCreate()
    expect(useCurrentCrumbs().value).toEqual([
      { label: 'Servers', to: { name: 'servers', params: { projectId: 1 } } },
      { label: 'Create server' },
    ])
    expect(document.body.querySelector('main nav, form nav, nav a[href="/p/1/servers"]')).toBeNull()
    expect(document.body.querySelector('h1')?.textContent?.trim()).toBe('Create server')
    const imageCard = $('input[name="image"][value="3"]').closest('label')!
    expect(imageCard.classList).toContain('nb-choice')
    expect(imageCard.textContent).toContain('D')
    expect(imageCard.textContent).toMatch(/Debian\s+13/)
    expect(imageCard.title).toBe('Debian 13')
    const typeCard = $('input[name="server_type"][value="2"]').closest('label')!
    expect(typeCard.dataset.variant).toBe('arch')
    expect(typeCard.textContent).toContain('2 vCPU')
    expect(typeCard.textContent).toContain('4 GB')
    expect(typeCard.textContent).toContain('40 GB')
    const addKey = Array.from(document.body.querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === 'Add key',
    )!
    expect(addKey.className).toContain('border-dashed')
    expect(document.body.querySelectorAll('aside [role="meter"]').length).toBe(5)
  })

  it('submits the full ServerInput', async () => {
    serveCatalog()
    const bodies: unknown[] = []
    server.use(
      http.post('*/v1/servers', async ({ request }) => {
        bodies.push(await request.json())
        return json(201, {
          server: makeServer({ id: 42, name: 'debian-13-small-2-1', status: 'initializing' }),
          action: action(),
          next_actions: [],
        })
      }),
      http.get('*/v1/servers/42', () =>
        json(200, makeServer({ id: 42, name: 'debian-13-small-2-1', status: 'initializing' })),
      ),
      http.get('*/v1/actions/7', () => json(200, action())),
    )
    const { router, wrapper } = await mountCreate()
    await pick('input[name="location"][value="1"]')
    await pick('input[name="image"][value="3"]')
    await pick('input[name="server_type"][value="2"]')
    await pick('input[name="ssh_key"][value="5"]')
    expect($('input[name="ipv4"]').checked).toBe(true)
    expect($('input[name="name"]').value).toBe('debian-13-small-2-1')
    expect(summary().textContent).toContain('Servers: 3 of 10, 4 after this')
    expect(createButton().disabled).toBe(false)
    createButton().click()
    await vi.waitFor(() => expect(router.currentRoute.value.name).toBe('server'))
    expect(bodies).toEqual([
      {
        name: 'debian-13-small-2-1',
        location: '1',
        image: '3',
        server_type: '2',
        ssh_keys: ['5'],
        public_net: { ipv4: true, ipv6: false },
      },
    ])
    expect(router.currentRoute.value.params.serverId).toBe('42')
    await vi.waitFor(() => expect(wrapper.text()).toContain('Creating'))
  })

  it('sends the firewalls field', async () => {
    firewalls = [
      { id: 8, name: 'web', labels: {}, rules: [], applied_to: [], created: '2026-11-20T10:00:00Z' },
    ]
    serveCatalog()
    const bodies: Record<string, unknown>[] = []
    server.use(
      http.post('*/v1/servers', async ({ request }) => {
        bodies.push((await request.json()) as Record<string, unknown>)
        return json(201, { server: makeServer({ id: 42 }), action: action(), next_actions: [] })
      }),
      http.get('*/v1/servers/42', () => json(200, makeServer({ id: 42 }))),
      http.get('*/v1/actions/7', () => json(200, action())),
    )
    const { router } = await mountCreate()
    await pick('input[name="firewall"][value="8"]')
    createButton().click()
    await vi.waitFor(() => expect(router.currentRoute.value.name).toBe('server'))
    expect(bodies[0]!.firewalls).toEqual([{ firewall: 8 }])
  })

  it('hides the firewall picker when the project has none', async () => {
    serveCatalog()
    const bodies: Record<string, unknown>[] = []
    server.use(
      http.post('*/v1/servers', async ({ request }) => {
        bodies.push((await request.json()) as Record<string, unknown>)
        return json(201, { server: makeServer({ id: 42 }), action: action(), next_actions: [] })
      }),
      http.get('*/v1/servers/42', () => json(200, makeServer({ id: 42 }))),
      http.get('*/v1/actions/7', () => json(200, action())),
    )
    const { router } = await mountCreate()
    expect($('input[name="firewall"]')).toBeNull()
    createButton().click()
    await vi.waitFor(() => expect(router.currentRoute.value.name).toBe('server'))
    expect(bodies[0]).not.toHaveProperty('firewalls')
  })

  it('sends user_data only when filled in', async () => {
    serveCatalog()
    const bodies: Record<string, unknown>[] = []
    server.use(
      http.post('*/v1/servers', async ({ request }) => {
        bodies.push((await request.json()) as Record<string, unknown>)
        return json(422, { error: { code: 'invalid_input', message: 'nope' } })
      }),
    )
    await mountCreate()
    await pick('input[name="image"][value="3"]')
    await pick('input[name="server_type"][value="2"]')
    createButton().click()
    await vi.waitFor(() => expect(bodies).toHaveLength(1))
    expect(bodies[0]).not.toHaveProperty('user_data')
    await type_('textarea[name="user_data"]', '#cloud-config\n')
    createButton().click()
    await vi.waitFor(() => expect(bodies).toHaveLength(2))
    expect(bodies[1]!.user_data).toBe('#cloud-config\n')
  })

  it('name stays user-edited', async () => {
    serveCatalog()
    await mountCreate()
    await pick('input[name="image"][value="3"]')
    await pick('input[name="server_type"][value="2"]')
    expect($('input[name="name"]').value).toBe('debian-13-small-2-1')
    await pick('input[name="server_type"][value="1"]')
    expect($('input[name="name"]').value).toBe('debian-13-tiny-1-1')
    await type_('input[name="name"]', 'my-box')
    await pick('input[name="server_type"][value="2"]')
    expect($('input[name="name"]').value).toBe('my-box')
  })

  it('switching location clears unavailable picks and disables Create', async () => {
    serveCatalog()
    await mountCreate()
    await pick('input[name="image"][value="3"]')
    await pick('input[name="server_type"][value="2"]')
    expect(createButton().disabled).toBe(false)
    await pick('input[name="location"][value="2"]')
    expect($('input[name="image"][value="3"]').disabled).toBe(true)
    expect($('input[name="image"][value="3"]').checked).toBe(false)
    expect($('input[name="ipv4"]').disabled).toBe(true)
    expect($('input[name="ipv4"]').checked).toBe(false)
    expect($('input[name="ipv6"]').checked).toBe(true)
    expect(document.body.textContent).toContain('No free IPv4 addresses in rack-2')
    expect(createButton().disabled).toBe(true)
  })

  it('blocks Create when the location has no free addresses', async () => {
    serveCatalog([{ id: 1, name: 'home', description: '', networks: { ipv4: false, ipv6: false } }])
    await mountCreate()
    await pick('input[name="image"][value="3"]')
    await pick('input[name="server_type"][value="2"]')
    const msg = 'No free addresses in home. Ask your Nebla admin to add an IP pool.'
    expect(stepFieldset('Networking')?.textContent).toContain(msg)
    expect(summary().textContent).toContain(msg)
    expect(document.body.textContent).not.toContain('No free IPv4 addresses in home')
    expect(createButton().disabled).toBe(true)
  })

  it('blocks Create when both address families are off', async () => {
    serveCatalog()
    await mountCreate()
    await pick('input[name="image"][value="3"]')
    await pick('input[name="server_type"][value="2"]')
    expect(createButton().disabled).toBe(false)
    await pick('input[name="ipv4"]')
    const msg = 'Pick IPv4 or IPv6 so the server can be reached.'
    expect(summary().textContent).toContain(msg)
    expect(document.body.textContent).not.toContain("won't have a public address")
    expect(createButton().disabled).toBe(true)
    await pick('input[name="ipv6"]')
    expect(createButton().disabled).toBe(false)
  })

  it('inline add key selects it', async () => {
    serveCatalog()
    server.use(
      http.post('*/v1/ssh_keys', () => {
        const k = key(9, 'desk')
        keys = [...keys, k]
        return json(201, k)
      }),
    )
    await mountCreate()
    const open = Array.from(document.body.querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === 'Add key',
    )!
    open.click()
    await settle()
    await type_('textarea[name="public_key"]', ED)
    $<HTMLButtonElement>('button[type="submit"][form="add-ssh-key"]').click()
    await settle()
    await vi.waitFor(() => expect($('input[name="ssh_key"][value="9"]')?.checked).toBe(true))
    expect($('input[name="ssh_key"][value="5"]').checked).toBe(false)
  })

  it('409 ip_pool_exhausted shows in summary', async () => {
    serveCatalog()
    server.use(
      http.post('*/v1/servers', () =>
        json(409, {
          error: { code: 'ip_pool_exhausted', message: 'no free ipv4 address in this location' },
        }),
      ),
    )
    const { router } = await mountCreate()
    await pick('input[name="image"][value="3"]')
    await pick('input[name="server_type"][value="2"]')
    createButton().click()
    await vi.waitFor(() =>
      expect(summary().textContent).toContain('no free ipv4 address in this location'),
    )
    expect(router.currentRoute.value.name).toBe('server-new')
  })

  it('no locations shows an empty state', async () => {
    serveCatalog([])
    const { wrapper } = await mountCreate()
    expect(wrapper.text()).toContain('No locations yet. Ask your Nebla admin to connect one.')
    expect(createButton()).toBeUndefined()
  })
})

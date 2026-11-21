import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { http } from 'msw'
import { json, server } from '@/test/server'
import { mountApp, testQueryClient, testRouter } from '@/test/mount'
import { makeServer } from './fixtures'
import ServerListPage from './ServerListPage.vue'

const meta = { pagination: { page: 1, per_page: 50, total_entries: 1 } }
const routes = [
  { path: '/p/:projectId/servers', name: 'servers', component: ServerListPage },
  { path: '/p/:projectId/servers/new', name: 'server-new', component: ServerListPage },
  { path: '/p/:projectId/servers/:serverId', name: 'server', component: ServerListPage },
  { path: '/p/:projectId/ssh-keys', name: 'ssh-keys', component: ServerListPage },
]

function useProject() {
  server.use(
    http.get('*/v1/projects/1', () =>
      json(200, { id: 1, name: 'Home lab', description: '', created: '2026-11-01T00:00:00Z' }),
    ),
  )
}

async function mountList() {
  const router = testRouter(routes)
  await router.push('/p/1/servers')
  const res = mountApp(ServerListPage, { router })
  await vi.waitFor(() => expect(res.wrapper.text()).not.toContain('Loading servers'))
  return res
}

beforeEach(() => {
  document.body.innerHTML = ''
  useProject()
})
afterEach(() => vi.useRealTimers())

describe('ServerListPage', () => {
  it('list shows name, image, type, IPv4 and location', async () => {
    server.use(http.get('*/v1/servers', () => json(200, { servers: [makeServer()], meta })))
    const { wrapper } = await mountList()
    const text = wrapper.text()
    for (const v of ['web1', 'Debian 13', 'small-2', '203.0.113.10', 'home']) {
      expect(text).toContain(v)
    }
    expect(text).not.toMatch(/vmid|pve/i)
  })

  it('has a hidden column head outside the list and the type with its vCPU count', async () => {
    server.use(
      http.get('*/v1/servers', () =>
        json(200, {
          servers: [makeServer({ image: { ...makeServer().image, name: 'debian-13' } })],
          meta,
        }),
      ),
    )
    const { wrapper } = await mountList()
    const head = wrapper.find('[aria-hidden="true"].md\\:flex')
    expect(head.findAll('span').map((s) => s.text())).toEqual(
      expect.arrayContaining(['Name', 'Image', 'Type', 'Public IPv4', 'Location']),
    )
    expect(head.element.closest('ul')).toBeNull()
    // Name lines up with the names, past the 22px arch and the 12px gap.
    expect(head.find('span').classes()).toContain('pl-[34px]')
    expect(wrapper.findAll('li')).toHaveLength(1)
    const li = wrapper.find('li')
    expect(li.text()).toContain('Debian 13')
    expect(li.find('[title="debian-13"]').exists()).toBe(true)
    expect(li.text()).toMatch(/small-2\s+· 2 vCPU/)
  })

  it('stopped server shows Off instead of IP', async () => {
    server.use(
      http.get('*/v1/servers', () => json(200, { servers: [makeServer({ status: 'off' })], meta })),
    )
    const { wrapper } = await mountList()
    expect(wrapper.text()).toContain('Off')
    expect(wrapper.text()).not.toContain('203.0.113.10')
  })

  it('non-running shows status word', async () => {
    server.use(
      http.get('*/v1/servers', () =>
        json(200, { servers: [makeServer({ status: 'initializing' })], meta }),
      ),
    )
    const { wrapper } = await mountList()
    expect(wrapper.text()).toContain('Initializing')
    expect(wrapper.text()).not.toContain('203.0.113.10')
  })

  it('list polls while a server is initializing and stops after', async () => {
    vi.useFakeTimers({ toFake: ['setInterval', 'setTimeout', 'clearInterval', 'clearTimeout'] })
    let calls = 0
    server.use(
      http.get('*/v1/servers', () => {
        calls++
        const status = calls === 1 ? 'initializing' : 'running'
        return json(200, { servers: [makeServer({ status })], meta })
      }),
    )
    const router = testRouter([
      { path: '/p/:projectId/servers', name: 'servers', component: ServerListPage },
      { path: '/p/:projectId/servers/new', name: 'server-new', component: ServerListPage },
      { path: '/p/:projectId/servers/:serverId', name: 'server', component: ServerListPage },
    ])
    await router.push('/p/1/servers')
    mountApp(ServerListPage, { router })
    await vi.waitFor(() => expect(calls).toBe(1))
    await vi.advanceTimersByTimeAsync(3100)
    await vi.waitFor(() => expect(calls).toBe(2))
    await vi.advanceTimersByTimeAsync(10_000)
    await flushPromises()
    expect(calls).toBe(2)
  })

  it('empty project shows create call to action', async () => {
    server.use(http.get('*/v1/servers', () => json(200, { servers: [], meta })))
    const { wrapper } = await mountList()
    expect(wrapper.find('h2').text()).toBe('Create your first server')
    expect(wrapper.text()).toContain(
      'Pick an image and a type, add your SSH key, and it boots in about a minute with a static IP.',
    )
    const links = wrapper.findAll('a').filter((a) => a.text() === 'Create server')
    expect(links.length).toBeGreaterThan(0)
    // Links by route name, so it resolves through the server-new route.
    expect(links[0]!.attributes('href')).toBe('/p/1/servers/new')
    const keys = wrapper.findAll('a').find((a) => a.text() === 'Add an SSH key first')
    expect(keys?.attributes('href')).toBe('/p/1/ssh-keys')
  })

  it('empty state shows the CLI line with placeholders when nothing is cached', async () => {
    let catalog = 0
    server.use(
      http.get('*/v1/servers', () => json(200, { servers: [], meta })),
      http.get('*/v1/locations', () => (catalog++, json(200, { locations: [] }))),
      http.get('*/v1/images', () => (catalog++, json(200, { images: [] }))),
      http.get('*/v1/server_types', () => (catalog++, json(200, { server_types: [] }))),
    )
    const { wrapper } = await mountList()
    expect(wrapper.text()).toContain('Or from your terminal')
    expect(wrapper.text()).toContain(
      'nebla server create --name web1 --type <type> --image <image> --location <location>',
    )
    await flushPromises()
    expect(catalog).toBe(0)
  })

  it('empty state fills the CLI line from the cached create-form catalog', async () => {
    server.use(http.get('*/v1/servers', () => json(200, { servers: [], meta })))
    const queryClient = testQueryClient()
    queryClient.setQueryData(['locations', 1], [{ id: 5, name: 'Home lab' }])
    queryClient.setQueryData(
      ['server-types', 1],
      [
        { id: 9, name: 'old-1', locations: [], deprecated: true },
        { id: 2, name: 'small-2', locations: [], deprecated: false },
        { id: 3, name: 'medium-4', locations: [], deprecated: false },
      ],
    )
    queryClient.setQueryData(
      ['images', 1],
      [
        { id: 1, name: 'debian-13', locations: [5] },
        { id: 2, name: 'ubuntu-24.04', locations: [] },
      ],
    )
    const router = testRouter(routes)
    await router.push('/p/1/servers')
    const { wrapper } = mountApp(ServerListPage, { router, queryClient })
    await vi.waitFor(() => expect(wrapper.text()).toContain('Or from your terminal'))
    expect(wrapper.text()).toContain(
      "nebla server create --name web1 --type small-2 --image debian-13 --location 'Home lab'",
    )
  })

  it('the column head has a CPU, 1h column from xl up', async () => {
    server.use(http.get('*/v1/servers', () => json(200, { servers: [makeServer()], meta })))
    const { wrapper } = await mountList()
    const head = wrapper.find('[aria-hidden="true"].md\\:flex')
    const cpu = head.findAll('span').find((s) => s.text() === 'CPU, 1h')
    expect(cpu?.classes()).toEqual(expect.arrayContaining(['hidden', 'xl:block']))
    const cell = wrapper.find('li [data-col="cpu"]')
    expect(cell.classes()).toEqual(expect.arrayContaining(['hidden', 'xl:block']))
    // Head and rows share the column template.
    expect(cpu!.element.parentElement!.className).toBe(
      cell.element.parentElement!.className.replace(/ flex flex-wrap.*$/, ''),
    )
  })

  it('filter is sent as label_selector and kept in URL', async () => {
    const seen: (string | null)[] = []
    server.use(
      http.get('*/v1/servers', ({ request }) => {
        seen.push(new URL(request.url).searchParams.get('label_selector'))
        return json(200, { servers: [makeServer({ labels: { env: 'prod' } })], meta })
      }),
    )
    const { wrapper, router } = await mountList()
    const input = wrapper.find('input[name="labels"]')
    await input.setValue('env=prod')
    await input.trigger('keydown.enter')
    await wrapper.find('form').trigger('submit')
    await vi.waitFor(() => expect(seen).toContain('env=prod'))
    expect(router.currentRoute.value.query.labels).toBe('env=prod')
  })

  it('prefills the filter from the URL', async () => {
    server.use(http.get('*/v1/servers', () => json(200, { servers: [makeServer()], meta })))
    const router = testRouter([
      { path: '/p/:projectId/servers', name: 'servers', component: ServerListPage },
      { path: '/p/:projectId/servers/new', name: 'server-new', component: ServerListPage },
      { path: '/p/:projectId/servers/:serverId', name: 'server', component: ServerListPage },
    ])
    await router.push('/p/1/servers?labels=env%3Dprod')
    const { wrapper } = mountApp(ServerListPage, { router })
    await vi.waitFor(() => expect(wrapper.text()).not.toContain('Loading servers'))
    expect((wrapper.find('input[name="labels"]').element as HTMLInputElement).value).toBe(
      'env=prod',
    )
  })

  it('422 shows under the field', async () => {
    server.use(
      http.get('*/v1/servers', ({ request }) =>
        new URL(request.url).searchParams.has('label_selector')
          ? json(422, { error: { code: 'invalid_input', message: 'bad selector term' } })
          : json(200, { servers: [makeServer()], meta }),
      ),
    )
    const { wrapper } = await mountList()
    await wrapper.find('input[name="labels"]').setValue('=')
    await wrapper.find('form').trigger('submit')
    await vi.waitFor(() => expect(wrapper.text()).toContain('bad selector term'))
    expect(wrapper.find('input[name="labels"]').exists()).toBe(true)
  })

  it('chips show three and +N', async () => {
    const labels = { a: '1', b: '2', c: '3', d: '4', e: '5' }
    server.use(
      http.get('*/v1/servers', () => json(200, { servers: [makeServer({ labels })], meta })),
    )
    const { wrapper } = await mountList()
    const text = wrapper.text()
    expect(text).toContain('a=1')
    expect(text).toContain('c=3')
    expect(text).not.toContain('d=4')
    expect(text).toContain('+2')
  })

  it('describes the list with the project name and running count', async () => {
    const servers = [makeServer(), makeServer({ id: 2, name: 'db1', status: 'off' })]
    server.use(http.get('*/v1/servers', () => json(200, { servers, meta })))
    const { wrapper } = await mountList()
    await vi.waitFor(() => expect(wrapper.text()).toContain('2 servers in Home lab, 1 running'))
  })

  it('uses the singular for one server', async () => {
    server.use(http.get('*/v1/servers', () => json(200, { servers: [makeServer()], meta })))
    const { wrapper } = await mountList()
    await vi.waitFor(() => expect(wrapper.text()).toContain('1 server in Home lab, 1 running'))
  })

  it('hides the description while a label filter is active', async () => {
    server.use(http.get('*/v1/servers', () => json(200, { servers: [makeServer()], meta })))
    const router = testRouter([
      { path: '/p/:projectId/servers', name: 'servers', component: ServerListPage },
      { path: '/p/:projectId/servers/new', name: 'server-new', component: ServerListPage },
      { path: '/p/:projectId/servers/:serverId', name: 'server', component: ServerListPage },
    ])
    await router.push('/p/1/servers?labels=env%3Dprod')
    const { wrapper } = mountApp(ServerListPage, { router })
    await vi.waitFor(() => expect(wrapper.text()).toContain('web1'))
    await flushPromises()
    expect(wrapper.text()).not.toMatch(/servers? in /)
  })

  it('shows no description for an empty list', async () => {
    server.use(http.get('*/v1/servers', () => json(200, { servers: [], meta })))
    const { wrapper } = await mountList()
    expect(wrapper.text()).not.toMatch(/servers? in /)
  })

  it('row link name contains the server name and the row has an OS tile', async () => {
    server.use(http.get('*/v1/servers', () => json(200, { servers: [makeServer()], meta })))
    const { wrapper } = await mountList()
    const link = wrapper.find('a[href="/p/1/servers/1"]')
    expect(link.text()).toContain('web1')
    expect(link.find('[aria-label="Running"]').exists()).toBe(true)
    expect(link.find('span[aria-hidden="true"]').text()).toBe('D')
    expect(link.find('.nb-mono').text()).toBe('203.0.113.10')
  })

  describe('status chips', () => {
    const three = [
      makeServer({ id: 1, name: 'web1', status: 'running' }),
      makeServer({ id: 2, name: 'db1', status: 'off' }),
      makeServer({ id: 3, name: 'job1', status: 'starting' }),
    ]

    it('counts from the list, busy under All only', async () => {
      server.use(http.get('*/v1/servers', () => json(200, { servers: three, meta })))
      const { wrapper } = await mountList()
      const group = wrapper.find('[role="radiogroup"]')
      expect(group.attributes('aria-label')).toBe('Filter by status')
      const radios = group.findAll('[role="radio"]')
      expect(radios.map((r) => r.text().replace(/\s+/g, ' '))).toEqual([
        'All 3',
        'Running 1',
        'Off 1',
      ])
      expect(radios[0]!.attributes('aria-checked')).toBe('true')
    })

    it('selecting Off shows one row and sets ?status=off, keeping labels', async () => {
      server.use(http.get('*/v1/servers', () => json(200, { servers: three, meta })))
      const { wrapper, router } = await mountList()
      await router.replace({ query: { labels: 'env=prod' } })
      await vi.waitFor(() => expect(wrapper.findAll('[role="radio"]')).toHaveLength(3))
      await wrapper.findAll('[role="radio"]')[2]!.trigger('click')
      await flushPromises()
      expect(router.currentRoute.value.query).toEqual({ labels: 'env=prod', status: 'off' })
      expect(wrapper.findAll('a[href="/p/1/servers/2"]')).toHaveLength(1)
      expect(wrapper.findAll('a[href="/p/1/servers/1"]')).toHaveLength(0)
      expect(wrapper.text()).toContain('db1')
      expect(wrapper.text()).not.toMatch(/servers? in /)
      await wrapper.findAll('[role="radio"]')[0]!.trigger('click')
      await flushPromises()
      expect(router.currentRoute.value.query).toEqual({ labels: 'env=prod' })
    })

    it('arrow keys move between chips', async () => {
      server.use(http.get('*/v1/servers', () => json(200, { servers: three, meta })))
      const { wrapper, router } = await mountList()
      await wrapper.findAll('[role="radio"]')[0]!.trigger('keydown', { key: 'ArrowRight' })
      await flushPromises()
      expect(router.currentRoute.value.query.status).toBe('running')
      const radios = wrapper.findAll('[role="radio"]')
      expect(radios.map((r) => r.attributes('tabindex'))).toEqual(['-1', '0', '-1'])
    })

    it('a server turning busy on the next poll leaves the Off filter', async () => {
      vi.useFakeTimers({ toFake: ['setInterval', 'setTimeout', 'clearInterval', 'clearTimeout'] })
      let calls = 0
      server.use(
        http.get('*/v1/servers', () => {
          calls++
          const second = makeServer({
            id: 2,
            name: 'db1',
            status: calls === 1 ? 'off' : 'starting',
          })
          return json(200, {
            servers: [makeServer({ id: 1, status: 'initializing' }), second],
            meta,
          })
        }),
      )
      const router = testRouter([
        { path: '/p/:projectId/servers', name: 'servers', component: ServerListPage },
        { path: '/p/:projectId/servers/new', name: 'server-new', component: ServerListPage },
        { path: '/p/:projectId/servers/:serverId', name: 'server', component: ServerListPage },
      ])
      await router.push('/p/1/servers?status=off')
      const { wrapper } = mountApp(ServerListPage, { router })
      await vi.waitFor(() => expect(wrapper.text()).toContain('db1'))
      expect(wrapper.text()).toContain('Off 1')
      await vi.advanceTimersByTimeAsync(3100)
      await vi.waitFor(() => expect(calls).toBe(2))
      await flushPromises()
      expect(wrapper.text()).toContain('No servers match this filter')
      expect(wrapper.text()).toContain('Off 0')
      expect(wrapper.text()).toContain('All 2')
    })

    it('Clear filters resets status and labels', async () => {
      server.use(
        http.get('*/v1/servers', () =>
          json(200, { servers: [makeServer({ status: 'running' })], meta }),
        ),
      )
      const router = testRouter([
        { path: '/p/:projectId/servers', name: 'servers', component: ServerListPage },
        { path: '/p/:projectId/servers/new', name: 'server-new', component: ServerListPage },
        { path: '/p/:projectId/servers/:serverId', name: 'server', component: ServerListPage },
      ])
      await router.push('/p/1/servers?status=off&labels=env%3Dprod')
      const { wrapper } = mountApp(ServerListPage, { router })
      await vi.waitFor(() => expect(wrapper.text()).toContain('No servers match this filter'))
      const btn = wrapper.findAll('button').find((b) => b.text() === 'Clear filters')!
      await btn.trigger('click')
      await flushPromises()
      expect(router.currentRoute.value.query).toEqual({})
      await vi.waitFor(() => expect(wrapper.text()).toContain('web1'))
      expect((wrapper.find('input[name="labels"]').element as HTMLInputElement).value).toBe('')
    })

    it('label-only no match keeps the plain message, and no chips without servers', async () => {
      server.use(http.get('*/v1/servers', () => json(200, { servers: [], meta })))
      const router = testRouter([
        { path: '/p/:projectId/servers', name: 'servers', component: ServerListPage },
        { path: '/p/:projectId/servers/new', name: 'server-new', component: ServerListPage },
      ])
      await router.push('/p/1/servers?labels=env%3Dprod')
      const a = mountApp(ServerListPage, { router })
      await vi.waitFor(() => expect(a.wrapper.text()).toContain('No servers match this filter.'))
      expect(a.wrapper.text()).not.toContain('Clear filters')
      const b = await mountList()
      expect(b.wrapper.find('[role="radiogroup"]').exists()).toBe(false)
    })
  })
})

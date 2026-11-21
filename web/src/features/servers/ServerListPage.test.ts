import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { http } from 'msw'
import { json, server } from '@/test/server'
import { mountApp, testRouter } from '@/test/mount'
import { makeServer } from './fixtures'
import ServerListPage from './ServerListPage.vue'

const meta = { pagination: { page: 1, per_page: 50, total_entries: 1 } }

function useProject() {
  server.use(
    http.get('*/v1/projects/1', () =>
      json(200, { id: 1, name: 'Home lab', description: '', created: '2026-11-01T00:00:00Z' }),
    ),
  )
}

async function mountList() {
  const router = testRouter([
    { path: '/p/:projectId/servers', name: 'servers', component: ServerListPage },
    { path: '/p/:projectId/servers/new', name: 'server-new', component: ServerListPage },
    { path: '/p/:projectId/servers/:serverId', name: 'server', component: ServerListPage },
  ])
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
    expect(wrapper.text()).toContain('No servers yet')
    const links = wrapper.findAll('a').filter((a) => a.text() === 'Create server')
    expect(links.length).toBeGreaterThan(0)
    // Links by route name, so it resolves through the server-new route.
    expect(links[0]!.attributes('href')).toBe('/p/1/servers/new')
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
})

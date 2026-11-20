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
    expect(links[0]!.attributes('href')).toBe('/p/1/servers/new')
  })
})

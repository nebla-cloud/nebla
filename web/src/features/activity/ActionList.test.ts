import { beforeEach, describe, expect, it } from 'vitest'
import { http } from 'msw'
import { json, server } from '@/test/server'
import { mountApp, testRouter } from '@/test/mount'
import type { Action } from '@/api/types'
import { makeServer } from '@/features/servers/fixtures'
import ActionList from './ActionList.vue'

function act(over: Partial<Action> = {}): Action {
  return {
    id: 1,
    command: 'reboot_server',
    status: 'success',
    progress: 100,
    resources: [{ id: 1, type: 'server' }],
    error: null,
    started: '2026-11-20T10:00:00Z',
    finished: '2026-11-20T10:00:12Z',
    ...over,
  }
}

let serverLists = 0

async function mountList(actions: Action[], showResource = true, total = 1) {
  serverLists = 0
  server.use(
    http.get('*/v1/projects/1', () =>
      json(200, { id: 1, name: 'Home', description: '', created: '2026-11-01T00:00:00Z' }),
    ),
    http.get('*/v1/servers', () => {
      serverLists++
      return json(200, {
        servers: [makeServer()],
        meta: { pagination: { page: 1, per_page: 50, total_entries: total } },
      })
    }),
  )
  const router = testRouter([
    { path: '/p/:projectId/activity', component: { render: () => null } },
    { path: '/p/:projectId/servers/:serverId', name: 'server', component: { render: () => null } },
  ])
  await router.push('/p/1/activity')
  return mountApp(ActionList, { router, props: { actions, showResource } })
}

beforeEach(() => {
  document.body.innerHTML = ''
})

describe('ActionList', () => {
  it('rows show title status duration', async () => {
    const { wrapper } = await mountList([
      act(),
      act({
        id: 2,
        status: 'running',
        progress: 40,
        finished: null,
        started: new Date().toISOString(),
      }),
      act({ id: 3, finished: '2026-11-20T10:03:00Z' }),
    ])
    const text = wrapper.text()
    expect(text).toContain('Reboot')
    expect(text).toContain('Done')
    expect(text).toContain('12 s')
    expect(text).toContain('3 min')
    expect(text).toContain('Running 40%')
    expect(wrapper.find('time[title]').exists()).toBe(true)
  })

  it('failed shows error', async () => {
    const { wrapper } = await mountList([
      act({ status: 'error', error: { code: 'x', message: 'Node unreachable' } }),
    ])
    expect(wrapper.text()).toContain('Failed')
    expect(wrapper.text()).toContain('Node unreachable')
  })

  it('resource link and deleted fallback', async () => {
    const { wrapper } = await mountList([
      act(),
      act({ id: 2, resources: [{ id: 99, type: 'server' }] }),
    ])
    await expect.poll(() => wrapper.text()).toContain('web1')
    const link = wrapper.find('a[href="/p/1/servers/1"]')
    expect(link.text()).toBe('web1')
    expect(wrapper.text()).toContain('Deleted server')
  })

  it('unknown server in an incomplete list is named by id and not linked', async () => {
    const { wrapper } = await mountList(
      [act(), act({ id: 2, resources: [{ id: 99, type: 'server' }] })],
      true,
      80,
    )
    await expect.poll(() => wrapper.text()).toContain('web1')
    expect(wrapper.text()).toContain('Server 99')
    expect(wrapper.text()).not.toContain('Deleted server')
    expect(wrapper.find('a[href="/p/1/servers/99"]').exists()).toBe(false)
  })

  it('deleted server is not linked', async () => {
    const { wrapper } = await mountList([act({ id: 2, resources: [{ id: 99, type: 'server' }] })])
    await expect.poll(() => wrapper.text()).toContain('Deleted server')
    expect(wrapper.find('a').exists()).toBe(false)
  })

  it('without the resource column it never lists servers', async () => {
    const { wrapper } = await mountList([act()], false)
    await new Promise((r) => setTimeout(r, 50))
    expect(wrapper.text()).toContain('Reboot')
    expect(serverLists).toBe(0)
  })

  it('status text is neutral, with a coloured dot', async () => {
    const { wrapper } = await mountList([
      act(),
      act({ id: 2, status: 'running', progress: 40, finished: null }),
    ])
    expect(wrapper.html()).not.toMatch(/text-(warning|success)/)
    expect(wrapper.findAll('[data-status-dot]')).toHaveLength(2)
  })
})

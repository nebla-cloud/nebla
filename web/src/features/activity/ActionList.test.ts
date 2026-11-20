import { afterEach, describe, expect, it, vi } from 'vitest'
import { http } from 'msw'
import { flushPromises, type VueWrapper } from '@vue/test-utils'
import { json, server } from '@/test/server'
import { mountApp, testRouter } from '@/test/mount'
import type { Action } from '@/api/types'
import { setMe } from '@/lib/session'
import { makeServer } from '@/features/servers/fixtures'
import ActivityTab from '@/features/servers/tabs/ActivityTab.vue'
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
let adminHits = 0
let projectHits = 0
let wrappers: VueWrapper[] = []

const failed = (over: Partial<Action> = {}) =>
  act({
    id: 5,
    status: 'error',
    error: { code: 'failed', message: "The server couldn't be created." },
    ...over,
  })

function asUser(isAdmin: boolean) {
  setMe({ id: 1, name: 'Me', email: 'me@example.com', is_admin: isAdmin, totp_enabled: false })
}

async function mountList(actions: Action[], showResource = true, total = 1) {
  serverLists = 0
  adminHits = 0
  server.use(
    http.get('*/v1/admin/actions/:id', ({ params }) => {
      adminHits++
      return json(200, {
        ...failed({ id: Number(params.id) }),
        project_id: 1,
        project_name: 'Home',
        error: { code: 'failed', message: 'x', detail: 'clone: VMID 4242 on pve1' },
      })
    }),
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
  const m = mountApp(ActionList, { router, props: { actions, showResource } })
  wrappers.push(m.wrapper)
  return m
}

const detailsButtons = (w: VueWrapper) =>
  w.findAll('button').filter((b) => b.text().includes('Technical details'))

afterEach(() => {
  wrappers.forEach((w) => w.unmount())
  wrappers = []
  setMe(null)
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

  it('finished rows show when they finished, running rows do not', async () => {
    const { wrapper } = await mountList([
      act({ finished: new Date(Date.now() - 120_000).toISOString() }),
      act({ id: 2, status: 'running', progress: 40, finished: null }),
    ])
    const [done, running] = wrapper.findAll('li')
    const t = done!.find('time[data-finished]')
    expect(t.exists()).toBe(true)
    expect(t.text()).toBe('finished 2 min ago')
    expect(running!.find('time[data-finished]').exists()).toBe(false)
    expect(running!.text()).not.toContain('finished')
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
    const { wrapper, queryClient } = await mountList([act()], false)
    await flushPromises()
    expect(wrapper.text()).toContain('Reboot')
    expect(queryClient.isFetching()).toBe(0)
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

  it('an admin on a project page sees technical details on failed rows only', async () => {
    asUser(true)
    const { wrapper } = await mountList([act(), failed()])
    expect(detailsButtons(wrapper)).toHaveLength(1)
    expect(adminHits).toBe(0)
    await detailsButtons(wrapper)[0]!.trigger('click')
    await vi.waitFor(() => expect(wrapper.text()).toContain('clone: VMID 4242 on pve1'))
    expect(adminHits).toBe(1)
  })

  it('non-admin never requests admin detail', async () => {
    asUser(false)
    let hit = false
    server.use(
      http.all('*/v1/admin/*', () => {
        hit = true
        return json(500, { error: { code: 'x', message: 'admin endpoint hit' } })
      }),
    )
    const { wrapper } = await mountList([act(), failed()])
    await vi.waitFor(() => expect(wrapper.text()).toContain('web1'))
    expect(wrapper.text()).toContain("The server couldn't be created.")
    expect(wrapper.text()).not.toContain('Technical details')
    await flushPromises()
    expect(hit).toBe(false)
  })

  it('the server activity tab shows details to admins too', async () => {
    asUser(true)
    await mountList([], false)
    server.use(
      http.get('*/v1/servers/1/actions', () =>
        json(200, {
          actions: [failed()],
          meta: { pagination: { page: 1, per_page: 25, total_entries: 1 } },
        }),
      ),
    )
    const router = testRouter([
      { path: '/p/:projectId/servers/:serverId/activity', component: { render: () => null } },
    ])
    await router.push('/p/1/servers/1/activity')
    const { wrapper } = mountApp(ActivityTab, { router, props: { server: makeServer({ id: 1 }) } })
    wrappers.push(wrapper)
    await vi.waitFor(() => expect(detailsButtons(wrapper)).toHaveLength(1))
    await detailsButtons(wrapper)[0]!.trigger('click')
    await vi.waitFor(() => expect(wrapper.text()).toContain('clone: VMID 4242 on pve1'))
  })

  it('outside a project it skips project lookups and links project names', async () => {
    asUser(true)
    projectHits = 0
    adminHits = 0
    server.use(
      http.get('*/v1/projects/:id', () => {
        projectHits++
        return json(404, { error: { code: 'not_found', message: 'Not found' } })
      }),
    )
    const router = testRouter([
      { path: '/admin/activity', component: { render: () => null }, meta: { side: 'admin' } },
      {
        path: '/admin/projects/:projectId',
        name: 'admin-project',
        component: { render: () => null },
      },
    ])
    await router.push('/admin/activity')
    const row = {
      ...failed(),
      project_id: 4,
      project_name: 'Alpha',
      error: { code: 'timed_out', message: 'It took too long.', detail: 'UPID timeout' },
    }
    const { wrapper, queryClient } = mountApp(ActionList, {
      router,
      props: { actions: [row], showResource: false, projectNames: true, admin: true },
    })
    wrappers.push(wrapper)
    expect(wrapper.find('a[href="/admin/projects/4"]').text()).toBe('Alpha')
    await detailsButtons(wrapper)[0]!.trigger('click')
    expect(wrapper.text()).toContain('UPID timeout')
    await flushPromises()
    expect(queryClient.isFetching()).toBe(0)
    expect(projectHits).toBe(0)
    expect(adminHits).toBe(0)
  })
})

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { http } from 'msw'
import { json, server } from '@/test/server'
import { mountApp, testRouter } from '@/test/mount'
import type { Action } from '@/api/types'
import ActivityPage from './ActivityPage.vue'

function act(id: number, over: Partial<Action> = {}): Action {
  return {
    id,
    command: 'start_server',
    status: 'success',
    progress: 100,
    resources: [{ id: 1, type: 'server' }],
    error: null,
    started: '2026-11-20T10:00:00Z',
    finished: '2026-11-20T10:00:12Z',
    ...over,
  }
}

function meta(page: number, total: number) {
  return { pagination: { page, per_page: 25, total_entries: total } }
}

async function mountPage() {
  server.use(
    http.get('*/v1/projects/1', () =>
      json(200, { id: 1, name: 'Home', description: '', created: '2026-11-01T00:00:00Z' }),
    ),
    http.get('*/v1/servers', () =>
      json(200, { servers: [], meta: { pagination: { page: 1, per_page: 50, total_entries: 0 } } }),
    ),
  )
  const router = testRouter([{ path: '/p/:projectId/activity', component: { render: () => null } },
    { path: '/p/:projectId/servers/:serverId', name: 'server', component: { render: () => null } },])
  await router.push('/p/1/activity')
  return mountApp(ActivityPage, { router })
}

beforeEach(() => {
  document.body.innerHTML = ''
})
afterEach(() => vi.useRealTimers())

describe('ActivityPage', () => {
  it('status filter sends status', async () => {
    const urls: URL[] = []
    server.use(
      http.get('*/v1/actions', ({ request }) => {
        urls.push(new URL(request.url))
        return json(200, { actions: [act(1)], meta: meta(1, 1) })
      }),
    )
    const { wrapper } = await mountPage()
    await vi.waitFor(() => expect(urls.length).toBe(1))
    expect(urls[0]!.searchParams.get('sort')).toBe('id:desc')
    expect(urls[0]!.searchParams.get('status')).toBeNull()
    const failed = wrapper.findAll('button').find((b) => b.text() === 'Failed')!
    await failed.trigger('click')
    await vi.waitFor(() => expect(urls.at(-1)!.searchParams.get('status')).toBe('error'))
  })

  it('load more fetches page 2', async () => {
    const pages: string[] = []
    server.use(
      http.get('*/v1/actions', ({ request }) => {
        const p = new URL(request.url).searchParams.get('page')!
        pages.push(p)
        return json(200, {
          actions: [p === '1' ? act(2) : act(1, { command: 'reboot_server' })],
          meta: meta(Number(p), 26),
        })
      }),
    )
    const { wrapper } = await mountPage()
    await vi.waitFor(() => expect(wrapper.text()).toContain('Power on'))
    await wrapper.findAll('button').find((b) => b.text() === 'Load more')!.trigger('click')
    await vi.waitFor(() => expect(wrapper.text()).toContain('Reboot'))
    expect(pages).toEqual(['1', '2'])
    expect(wrapper.text()).not.toContain('Load more')
  })

  it('polls while running and stops after', async () => {
    let calls = 0
    server.use(
      http.get('*/v1/actions', () => {
        calls++
        const a = calls < 3 ? act(1, { status: 'running', progress: 10, finished: null }) : act(1)
        return json(200, { actions: [a], meta: meta(1, 1) })
      }),
    )
    const { wrapper } = await mountPage()
    await vi.waitFor(() => expect(wrapper.text()).toContain('Running 10%'))
    await vi.waitFor(() => expect(calls).toBe(3), { timeout: 9000 })
    await vi.waitFor(() => expect(wrapper.text()).toContain('Done'))
    await new Promise((r) => setTimeout(r, 3500))
    expect(calls).toBe(3)
  }, 20000)
})

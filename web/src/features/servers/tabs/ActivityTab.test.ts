import { beforeEach, describe, expect, it } from 'vitest'
import { http } from 'msw'
import { json, server } from '@/test/server'
import { mountApp, testRouter } from '@/test/mount'
import { makeServer } from '../fixtures'
import ActivityTab from './ActivityTab.vue'

beforeEach(() => {
  document.body.innerHTML = ''
})

describe('ActivityTab', () => {
  it('uses the server endpoint', async () => {
    const urls: URL[] = []
    server.use(
      http.get('*/v1/projects/1', () =>
        json(200, { id: 1, name: 'Home', description: '', created: '2026-11-01T00:00:00Z' }),
      ),
      http.get('*/v1/servers/5/actions', ({ request }) => {
        urls.push(new URL(request.url))
        return json(200, {
          actions: [],
          meta: { pagination: { page: 1, per_page: 25, total_entries: 0 } },
        })
      }),
      http.get('*/v1/servers', () =>
        json(200, { servers: [], meta: { pagination: { page: 1, per_page: 50, total_entries: 0 } } }),
      ),
    )
    const router = testRouter([
      { path: '/p/:projectId/servers/:serverId/activity', component: { render: () => null } },
    ])
    await router.push('/p/1/servers/5/activity')
    const { wrapper } = mountApp(ActivityTab, { router, props: { server: makeServer({ id: 5 }) } })
    await expect.poll(() => urls.length).toBe(1)
    expect(urls[0]!.pathname + urls[0]!.search).toContain('/servers/5/actions')
    expect(urls[0]!.searchParams.get('sort')).toBe('id:desc')
    await expect.poll(() => wrapper.text()).toContain('Nothing has happened here yet.')
  })
})

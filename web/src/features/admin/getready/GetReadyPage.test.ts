import { describe, expect, it } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { http } from 'msw'
import { json, server } from '@/test/server'
import { mountApp, testRouter } from '@/test/mount'
import { setMe, resetSession } from '@/lib/session'
import GetReadyPage from './GetReadyPage.vue'

const meta = { pagination: { page: 1, per_page: 50, total_entries: 1 } }

function serve(over: { nodes?: string[]; types?: unknown[] } = {}) {
  server.use(
    http.get('*/v1/admin/locations', () =>
      json(200, {
        locations: [
          {
            id: 1,
            name: 'home',
            nodes: (over.nodes ?? ['online', 'online', 'online']).map((s) => ({
              id: 1,
              name: 'n',
              status: s,
              enabled: true,
            })),
          },
        ],
        meta,
      }),
    ),
    http.get('*/v1/admin/catalog', () => json(200, { catalog: [] })),
    http.get('*/v1/admin/images', () => json(200, { images: [], meta })),
    http.get('*/v1/admin/server_types', () => json(200, { server_types: over.types ?? [], meta })),
    http.get('*/v1/admin/ip_pools', () => json(200, { ip_pools: [], meta })),
  )
}

const names = ['admin-locations', 'admin-catalog', 'admin-server-types', 'admin-ip-pools']
const router = () =>
  testRouter(names.map((name) => ({ path: `/${name}`, name, component: { render: () => null } })))

async function mountPage() {
  resetSession()
  setMe({ id: 1, is_admin: true } as never)
  const m = mountApp(GetReadyPage, { router: router() })
  await flushPromises()
  await flushPromises()
  return m
}

describe('GetReadyPage', () => {
  it('current step is the first not done and has the primary button', async () => {
    serve()
    const { wrapper } = await mountPage()
    expect(wrapper.text()).toContain('Four steps, then anyone in a project can create servers.')
    expect(wrapper.text()).toContain('home: 3 nodes online')
    const steps = wrapper.findAll('[data-step]')
    expect(steps.map((s) => s.attributes('data-state'))).toEqual([
      'done',
      'current',
      'todo',
      'todo',
    ])
    expect(steps[1]!.text()).toContain('Choose images')
    expect(steps[1]!.attributes('data-state')).toBe('current')
    expect(steps[1]!.find('a').text()).toBe('Choose images')
    expect(steps[2]!.attributes('data-state')).toBe('todo')
  })

  it('each step links to its admin page', async () => {
    serve()
    const { wrapper } = await mountPage()
    const hrefs = wrapper.findAll('[data-step]').map((s) => s.find('a').attributes('href'))
    expect(hrefs).toEqual(names.map((n) => `/${n}`))
  })
})

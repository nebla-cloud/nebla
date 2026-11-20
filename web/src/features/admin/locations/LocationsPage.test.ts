import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { http } from 'msw'
import { json, server } from '@/test/server'
import { mountApp, testRouter } from '@/test/mount'
import type { AdminLocation } from '@/api/types'
import routes from './routes'
import LocationsPage from './LocationsPage.vue'
import LocationDetailPage from './LocationDetailPage.vue'

beforeEach(() => {
  document.body.innerHTML = ''
})

function loc(nodes: AdminLocation['nodes'] = []): AdminLocation {
  return {
    id: 1,
    name: 'home',
    description: '',
    pve: {
      url: 'https://pve.example.test:8006',
      token_id: 'nebla@pve!panel',
      has_secret: true,
      ca_fingerprint: '',
      insecure_tls: false,
    },
    disk_storage: 'local-lvm',
    iso_storage: 'local',
    import_storage: '',
    nodes,
    created: '2026-01-01T00:00:00Z',
    updated: '2026-01-01T00:00:00Z',
  }
}

const node = (id: number, name: string, status: string, enabled: boolean) => ({
  id,
  name,
  status,
  enabled,
  disk_storage_override: '',
  max_cpu: 8,
  max_mem: 16 * 1024 ** 3,
  cpu: 0.1,
  mem: 1024 ** 3,
  updated: '2026-01-01T00:00:00Z',
})

const list = (locations: AdminLocation[]) =>
  http.get('*/v1/admin/locations', () =>
    json(200, {
      locations,
      meta: { pagination: { page: 1, per_page: 50, total_entries: locations.length } },
    }),
  )

describe('admin locations', () => {
  it('lists locations', async () => {
    server.use(list([loc([node(2, 'pve1', 'online', true)])]))
    const { wrapper } = mountApp(LocationsPage, {
      router: testRouter(routes.map((r) => ({ ...r, path: '/admin/' + r.path }))),
    })
    await flushPromises()
    expect(wrapper.text()).toContain('home')
    expect(wrapper.text()).toContain('1 node')
  })

  it('detail lists nodes with status and lets you sync and toggle a node', async () => {
    const nodes = [node(2, 'pve1', 'online', true), node(3, 'pve2', 'offline', false)]
    let synced = false
    let put: unknown
    server.use(
      list([loc(nodes)]),
      http.post('*/v1/admin/locations/1/sync_nodes', () => {
        synced = true
        return json(200, loc(nodes))
      }),
      http.put('*/v1/admin/locations/1/nodes/2', async ({ request }) => {
        put = await request.json()
        return json(200, { ...nodes[0], enabled: false })
      }),
    )
    const router = testRouter([
      { path: '/admin/locations', name: 'admin-locations', component: LocationsPage },
      {
        path: '/admin/locations/:locationId',
        name: 'admin-location',
        component: LocationDetailPage,
      },
    ])
    await router.push('/admin/locations/1')
    const { wrapper } = mountApp(LocationDetailPage, { router })
    await flushPromises()
    expect(wrapper.text()).toContain('pve1')
    expect(wrapper.text()).toContain('online')
    expect(wrapper.text()).toContain('offline')

    const sync = wrapper.findAll('button').find((b) => b.text().includes('Sync nodes'))!
    await sync.trigger('click')
    await flushPromises()
    expect(synced).toBe(true)

    await wrapper.find('button[role="switch"]').trigger('click')
    await flushPromises()
    expect(put).toEqual({ enabled: false })
  })

  it('delete confirms by name', async () => {
    let deleted = false
    server.use(
      list([loc()]),
      http.delete('*/v1/admin/locations/1', () => {
        deleted = true
        return new Response(null, { status: 204 })
      }),
    )
    const router = testRouter([
      { path: '/admin/locations', name: 'admin-locations', component: LocationsPage },
      {
        path: '/admin/locations/:locationId',
        name: 'admin-location',
        component: LocationDetailPage,
      },
    ])
    await router.push('/admin/locations/1')
    const { wrapper } = mountApp(LocationDetailPage, { router })
    await vi.waitFor(() => expect(wrapper.text()).toContain('Delete location'))
    const del = wrapper.findAll('button').find((b) => b.text() === 'Delete location')!
    await del.trigger('click')
    await flushPromises()
    const confirm = () =>
      Array.from(document.body.querySelectorAll('button')).find(
        (b) => b.textContent?.trim() === 'Delete',
      )!
    expect(confirm().disabled).toBe(true)
    const input = document.body.querySelector<HTMLInputElement>('input[name="confirm_name"]')!
    input.value = 'home'
    input.dispatchEvent(new Event('input'))
    await flushPromises()
    expect(confirm().disabled).toBe(false)
    confirm().click()
    await flushPromises()
    await vi.waitFor(() => expect(deleted).toBe(true))
  })
})

import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { http } from 'msw'
import { json, server } from '@/test/server'
import { mountApp } from '@/test/mount'
import type { CatalogEntry, CatalogLocationState } from './queries'
import CatalogPage from './CatalogPage.vue'

const location = (id: number, name: string) => ({
  id,
  name,
  description: '',
  pve: { url: 'https://pve.example:8006', token_id: 'nebla@pve!t', tls_insecure: false },
  disk_storage: 'local-lvm',
  iso_storage: 'local',
  import_storage: 'local',
  nodes: [],
  created: '2026-11-01T10:00:00Z',
  updated: '2026-11-01T10:00:00Z',
})

const state = (location_id: number, s: string): CatalogLocationState => ({
  location_id,
  available: true,
  reason: '',
  state: s,
  nodes: [],
})

const entry = (key: string, description: string, retired = false, s = 'off'): CatalogEntry => ({
  key,
  description,
  os_flavor: key.split('-')[0]!,
  os_version: key.split('-')[1] ?? '',
  retired,
  locations: [state(1, retired ? 'retired' : s), state(2, retired ? 'retired' : 'off')],
})

function serve(opts: { locations?: unknown[]; catalog?: () => CatalogEntry[] } = {}) {
  let catalogCalls = 0
  server.use(
    http.get('*/v1/admin/locations', () =>
      json(200, {
        locations: opts.locations ?? [location(1, 'home'), location(2, 'rack-2')],
        meta: { pagination: { page: 1, per_page: 50, total_entries: 2 } },
      }),
    ),
    http.get('*/v1/admin/catalog', () => {
      catalogCalls++
      return json(200, {
        catalog: opts.catalog?.() ?? [
          entry('debian-13', 'Debian 13'),
          entry('ubuntu-24.04', 'Ubuntu 24.04'),
        ],
      })
    }),
    http.get('*/v1/admin/images', () =>
      json(200, { images: [], meta: { pagination: { page: 1, per_page: 50, total_entries: 0 } } }),
    ),
  )
  return () => catalogCalls
}

afterEach(() => {
  vi.useRealTimers()
  document.body.innerHTML = ''
})

describe('CatalogPage', () => {
  it('grid has one column per location and one row per entry', async () => {
    serve()
    const { wrapper } = mountApp(CatalogPage)
    await flushPromises()
    const heads = wrapper.findAll('thead th').map((th) => th.text())
    expect(heads).toEqual(['Image', 'home', 'rack-2'])
    const rows = wrapper.findAll('tbody tr')
    expect(rows).toHaveLength(2)
    expect(rows[0]!.findAll('th, td')).toHaveLength(3)
    expect(wrapper.text()).toContain('Official cloud images Nebla builds for each location.')
  })

  it('retired entries sort last', async () => {
    serve({
      catalog: () => [
        entry('debian-12', 'Debian 12', true),
        entry('debian-13', 'Debian 13'),
        entry('ubuntu-24.04', 'Ubuntu 24.04'),
      ],
    })
    const { wrapper } = mountApp(CatalogPage)
    await flushPromises()
    const titles = wrapper.findAll('tbody tr th').map((th) => th.text())
    expect(titles[0]).toContain('Debian 13')
    expect(titles[1]).toContain('Ubuntu 24.04')
    expect(titles[2]).toContain('Debian 12')
  })

  it('polls while building', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    let building = true
    const calls = serve({
      catalog: () => [entry('debian-13', 'Debian 13', false, building ? 'building' : 'ready')],
    })
    mountApp(CatalogPage)
    await vi.waitFor(() => expect(calls()).toBe(1))
    await vi.advanceTimersByTimeAsync(3000)
    await vi.waitFor(() => expect(calls()).toBe(2))
    building = false
    await vi.advanceTimersByTimeAsync(3000)
    await vi.waitFor(() => expect(calls()).toBe(3))
    await flushPromises()
    await vi.advanceTimersByTimeAsync(9000)
    expect(calls()).toBe(3)
  })

  it('shows hand-registered images read-only', async () => {
    serve()
    server.use(
      http.get('*/v1/admin/images', () =>
        json(200, {
          images: [
            {
              id: 1,
              name: 'debian-13',
              description: 'Debian 13',
              os_flavor: 'debian',
              os_version: '13',
              catalog_key: 'debian-13',
              templates: [{ location_id: 1, vmid: 9000 }],
              created: '2026-11-01T10:00:00Z',
            },
            {
              id: 2,
              name: 'win-2025',
              description: 'Windows Server 2025',
              os_flavor: 'windows',
              os_version: '2025',
              catalog_key: null,
              templates: [{ location_id: 2, vmid: 9100 }],
              created: '2026-11-01T10:00:00Z',
            },
          ],
          meta: { pagination: { page: 1, per_page: 50, total_entries: 2 } },
        }),
      ),
    )
    const { wrapper } = mountApp(CatalogPage)
    await flushPromises()
    const own = wrapper.find('[data-own-images]')
    expect(own.text()).toContain('Your own images')
    expect(own.text()).toContain('Windows Server 2025')
    expect(own.text()).toContain('rack-2')
    expect(own.text()).not.toContain('Debian 13')
    expect(own.find('button').exists()).toBe(false)
  })

  it('asks for a location first when there are none', async () => {
    serve({ locations: [] })
    const { wrapper } = mountApp(CatalogPage)
    await flushPromises()
    expect(wrapper.text()).toContain('Connect a location first')
    expect(wrapper.find('a[href="/admin/locations"]').exists()).toBe(true)
    expect(wrapper.find('table').exists()).toBe(false)
  })
})

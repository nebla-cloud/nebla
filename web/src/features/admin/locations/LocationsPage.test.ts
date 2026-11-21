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
    firewall_enabled: null,
    volume_storage: '',
    volume_storage_ok: false,
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

  const detailRouter = () =>
    testRouter([
      { path: '/admin/locations', name: 'admin-locations', component: LocationsPage },
      {
        path: '/admin/locations/:locationId',
        name: 'admin-location',
        component: LocationDetailPage,
      },
    ])

  it('shows the volume grant hint until volume_storage_ok', async () => {
    const nodes = [
      node(2, 'pve1', 'online', true),
      { ...node(3, 'pve2', 'online', true), disk_storage_override: 'ceph' },
      { ...node(4, 'pve3', 'online', false), disk_storage_override: 'off' },
    ]
    const router = detailRouter()
    server.use(list([loc(nodes)]))
    await router.push('/admin/locations/1')
    const { wrapper } = mountApp(LocationDetailPage, { router })
    await vi.waitFor(() => expect(wrapper.text()).toContain('pve1'))
    const hint = wrapper.find('[data-testid="volume-grant"]')
    expect(hint.text()).toContain('Volumes need one more permission')
    expect(hint.text()).toContain('Re-run the setup command, or run:')
    expect(hint.find('pre').text()).toBe(
      'pveum acl modify /storage/local-lvm --users nebla@pve --roles NeblaISO\n' +
        'pveum acl modify /storage/ceph --users nebla@pve --roles NeblaISO',
    )
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })
    await hint
      .findAll('button')
      .find((b) => b.text() === 'Copy')!
      .trigger('click')
    await flushPromises()
    expect(writeText).toHaveBeenCalledWith(hint.find('pre').text())
    expect(hint.text()).toContain('Copied')
  })

  it('hides the volume grant hint when ok or without nodes', async () => {
    for (const l of [
      { ...loc([node(2, 'pve1', 'online', true)]), volume_storage_ok: true },
      loc(),
    ]) {
      document.body.innerHTML = ''
      const router = detailRouter()
      server.use(list([l]))
      await router.push('/admin/locations/1')
      const { wrapper } = mountApp(LocationDetailPage, { router })
      await vi.waitFor(() => expect(wrapper.text()).toContain('Delete location'))
      expect(wrapper.find('[data-testid="volume-grant"]').exists()).toBe(false)
    }
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

  describe('enrollment', () => {
    const enr = (
      id: number,
      name: string,
      state: string,
      expired = false,
      location_id: number | null = null,
    ) => ({
      id,
      name,
      description: '',
      location_id,
      token_name: 't',
      state,
      expired,
      expires_at: '2026-01-01T00:30:00Z',
      created: '2026-01-01T00:00:00Z',
      report: null,
    })
    const routers = () =>
      testRouter([
        { path: '/admin/locations', name: 'admin-locations', component: LocationsPage },
        {
          path: '/admin/locations/:locationId',
          name: 'admin-location',
          component: LocationDetailPage,
        },
        {
          path: '/admin/locations/enroll/:enrollmentId',
          name: 'admin-location-enroll',
          component: { template: '<div />' },
        },
      ])

    it('lists live enrollments with their state', async () => {
      server.use(
        list([loc()]),
        http.get('*/v1/admin/location_enrollments', () =>
          json(200, {
            enrollments: [
              enr(5, 'lab', 'waiting'),
              enr(6, 'garage', 'reported'),
              enr(8, 'home', 'waiting', true, 1),
            ],
          }),
        ),
      )
      const { wrapper } = mountApp(LocationsPage, { router: routers() })
      await vi.waitFor(() => expect(wrapper.text()).toContain('lab · Waiting for setup'))
      const text = wrapper.text()
      expect(text).toContain('garage · Ready to finish')
      expect(text).toContain('home (re-run) · Expired')
      expect(text).toContain('0 nodes')
      const hrefs = wrapper.findAll('a').map((a) => a.attributes('href'))
      expect(hrefs).toContain('/admin/locations/enroll/5')
      expect(hrefs).toContain('/admin/locations/enroll/6')
      expect(hrefs).toContain('/admin/locations/1')
    })

    it('keeps the empty state below the enrollments', async () => {
      server.use(
        list([]),
        http.get('*/v1/admin/location_enrollments', () =>
          json(200, { enrollments: [enr(5, 'lab', 'waiting')] }),
        ),
      )
      const { wrapper } = mountApp(LocationsPage, { router: routers() })
      await vi.waitFor(() => expect(wrapper.text()).toContain('lab · Waiting for setup'))
      await vi.waitFor(() => expect(wrapper.text()).toContain('No locations yet'))
    })

    it('connect manually reveals the full form', async () => {
      server.use(
        list([]),
        http.get('*/v1/admin/location_enrollments', () => json(200, { enrollments: [] })),
      )
      const { wrapper } = mountApp(LocationsPage, { router: routers() })
      await vi.waitFor(() => expect(wrapper.text()).toContain('No locations yet'))
      const open = wrapper.findAll('button').find((b) => b.text() === 'Connect a location')!
      await open.trigger('click')
      expect(wrapper.find('input[name=token_secret]').exists()).toBe(false)
      expect(wrapper.text()).toContain('Get setup command')
      const manual = wrapper.findAll('button').find((b) => b.text() === 'Connect manually')!
      await manual.trigger('click')
      expect(wrapper.find('input[name=token_secret]').exists()).toBe(true)
    })

    it('re-run setup opens a live re-run instead of starting one', async () => {
      let posted = false
      const live = {
        ...enr(9, 'home', 'reported', false, 1),
        expires_at: new Date(Date.now() + 3600_000).toISOString(),
      }
      server.use(
        list([loc()]),
        http.get('*/v1/admin/location_enrollments', () =>
          json(200, { enrollments: [enr(4, 'home', 'waiting', true, 1), live] }),
        ),
        http.post('*/v1/admin/location_enrollments', () => {
          posted = true
          return json(500, {})
        }),
      )
      const router = routers()
      await router.push('/admin/locations/1')
      const { wrapper } = mountApp(LocationDetailPage, { router })
      await vi.waitFor(() =>
        expect(wrapper.findAll('a').map((a) => a.attributes('href'))).toContain(
          '/admin/locations/enroll/9',
        ),
      )
      const link = wrapper.findAll('a').find((a) => a.text() === 'Re-run setup')!
      expect(link.attributes('href')).toBe('/admin/locations/enroll/9')
      await link.trigger('click')
      await vi.waitFor(() =>
        expect(router.currentRoute.value.path).toBe('/admin/locations/enroll/9'),
      )
      expect(posted).toBe(false)
    })

    it('re-run setup starts an enrollment for the location', async () => {
      let body: unknown
      server.use(
        list([loc()]),
        http.get('*/v1/admin/location_enrollments', () =>
          json(200, { enrollments: [enr(4, 'home', 'waiting', true, 1)] }),
        ),
        http.post('*/v1/admin/location_enrollments', async ({ request }) => {
          body = await request.json()
          return json(201, {
            enrollment: enr(7, 'home', 'waiting', false, 1),
            token: 'nbe_x',
            command: 'c',
            offline_script: 's',
          })
        }),
      )
      const router = routers()
      await router.push('/admin/locations/1')
      const { wrapper } = mountApp(LocationDetailPage, { router })
      await vi.waitFor(() => expect(wrapper.text()).toContain('Re-run setup'))
      await wrapper
        .findAll('button')
        .find((b) => b.text() === 'Re-run setup')!
        .trigger('click')
      await vi.waitFor(() =>
        expect(router.currentRoute.value.path).toBe('/admin/locations/enroll/7'),
      )
      expect(body).toEqual({ location_id: 1 })
    })
  })

  it.each([
    [false, true],
    [null, false],
    [true, false],
  ])('detail warns about the datacenter firewall when it is %s', async (enabled, warned) => {
    server.use(list([{ ...loc(), firewall_enabled: enabled }]))
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
    await vi.waitFor(() => expect(wrapper.text()).not.toContain('Loading location'))
    expect(wrapper.text().includes('datacenter firewall is off')).toBe(warned)
  })
})

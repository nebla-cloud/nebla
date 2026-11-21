import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { RouterView, createRouter, createWebHistory } from 'vue-router'
import { http } from 'msw'
import { json, server } from '@/test/server'
import { mountApp, testRouter } from '@/test/mount'
import type { AdminLocation, LocationEnrollment } from '@/api/types'
import { lastCreated } from './queries'
import EnrollPage from './EnrollPage.vue'
import LocationDetailPage from './LocationDetailPage.vue'

beforeEach(() => {
  document.body.innerHTML = ''
  lastCreated.value = {}
})

const mounted: { unmount: () => void }[] = []

afterEach(() => {
  vi.useRealTimers()
  mounted.splice(0).forEach((w) => w.unmount())
})

const report = {
  pve_version: '9.0.3',
  node: 'pve1',
  token_id: 'nebla@pve!nebla-home-abc123',
  nodes: [
    { name: 'pve1', ip: '10.0.0.1', online: true, fingerprint: 'AA:BB' },
    { name: 'pve2', ip: '10.0.0.2', online: true, fingerprint: 'CC:DD' },
  ],
  storages: [
    {
      id: 'local-lvm',
      type: 'lvmthin',
      content: ['images', 'rootdir'],
      shared: false,
      nebla_iso: false,
    },
    { id: 'local', type: 'dir', content: ['iso', 'vztmpl'], shared: false, nebla_iso: false },
    { id: 'nebla-iso', type: 'dir', content: ['iso', 'import'], shared: false, nebla_iso: true },
    { id: 'nebla-imp', type: 'dir', content: ['import'], shared: false, nebla_iso: true },
  ],
}

const enrollment = (over: Partial<LocationEnrollment> = {}): LocationEnrollment => ({
  id: 7,
  name: 'home',
  description: '',
  location_id: null,
  token_name: 'nebla-home-abc123',
  state: 'waiting',
  expired: false,
  expires_at: new Date(Date.now() + 25 * 60_000).toISOString(),
  created: '2026-01-01T00:00:00Z',
  report: null,
  ...over,
})

const remember = (e = enrollment()) => {
  lastCreated.value = {
    [e.id]: {
      enrollment: e,
      token: 'nbe_secret',
      command: 'curl -fsSL X | bash',
      offline_script: 'echo offline',
    },
  }
}

const get = (e: LocationEnrollment) =>
  http.get('*/v1/admin/location_enrollments/7', () => json(200, e))

async function mountPage() {
  const router = testRouter([
    { path: '/admin/locations', name: 'admin-locations', component: EnrollPage },
    {
      path: '/admin/locations/enroll/:enrollmentId',
      name: 'admin-location-enroll',
      component: EnrollPage,
    },
    { path: '/admin/locations/:locationId', name: 'admin-location', component: LocationDetailPage },
  ])
  await router.push('/admin/locations/enroll/7')
  const m = mountApp(RouterView, { router })
  await vi.waitFor(() => expect(m.wrapper.find('h1').exists()).toBe(true))
  mounted.push(m.wrapper)
  return { ...m, router }
}

const button = (
  w: {
    findAll: (s: string) => {
      text: () => string
      trigger: (e: string) => Promise<void>
      attributes: (n: string) => string | undefined
    }[]
  },
  label: string,
) => w.findAll('button').find((b) => b.text() === label)!

describe('EnrollPage', () => {
  it('shows the command and polls until the node reports', async () => {
    remember()
    let calls = 0
    server.use(
      http.get('*/v1/admin/location_enrollments/7', () => {
        calls++
        return json(200, calls < 2 ? enrollment() : enrollment({ state: 'reported', report }))
      }),
    )
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const { wrapper } = await mountPage()
    await vi.waitFor(() => expect(wrapper.find('[data-testid=enroll-command]').exists()).toBe(true))
    expect(wrapper.find('[data-testid=enroll-command]').text()).toBe('curl -fsSL X | bash')
    expect(wrapper.text()).toContain('Waiting for the node…')
    expect(wrapper.text()).toContain('Connect "home"')
    const link = wrapper.find('a[href="/v1/enroll.sh"]')
    expect(link.attributes('target')).toBe('_blank')
    await vi.advanceTimersByTimeAsync(3100)
    await vi.waitFor(() => expect(wrapper.find('select[name=disk_storage]').exists()).toBe(true))
    expect(wrapper.find('[data-testid=enroll-command]').exists()).toBe(false)
  })

  it('shows the offline script', async () => {
    remember()
    server.use(get(enrollment()))
    const { wrapper } = await mountPage()
    expect(wrapper.find('[data-testid=enroll-offline]').text()).toBe('echo offline')
  })

  it('asks for a new command after a reload', async () => {
    server.use(get(enrollment()))
    const { wrapper } = await mountPage()
    expect(wrapper.text()).toContain('Get a new command to continue')
    expect(wrapper.find('[data-testid=enroll-command]').exists()).toBe(false)
    expect(button(wrapper, 'New command')).toBeTruthy()
  })

  it('shows expiry with a new command button', async () => {
    remember()
    server.use(get(enrollment({ expired: true })))
    let issued = false
    server.use(
      http.post('*/v1/admin/location_enrollments/7/token', () => {
        issued = true
        return json(200, {
          enrollment: enrollment(),
          token: 'nbe_new',
          command: 'curl NEW',
          offline_script: 'echo new',
        })
      }),
    )
    const { wrapper } = await mountPage()
    expect(wrapper.text()).toContain('This command expired.')
    await button(wrapper, 'New command').trigger('click')
    await flushPromises()
    expect(issued).toBe(true)
  })

  it('posts a pasted blob and explains a 401', async () => {
    remember()
    let body: unknown
    server.use(
      get(enrollment()),
      http.post('*/v1/admin/location_enrollments/7/report', async ({ request }) => {
        body = await request.json()
        return json(401, { error: { code: 'enrollment_invalid', message: 'Invalid enrollment.' } })
      }),
    )
    const { wrapper } = await mountPage()
    await wrapper.find('textarea[name=blob]').setValue('  abc  ')
    await button(wrapper, 'Submit').trigger('click')
    await flushPromises()
    expect(body).toEqual({ blob: 'abc' })
    expect(wrapper.text()).toContain('That line belongs to a different setup command.')
  })

  it('lists only matching storages', async () => {
    remember()
    server.use(get(enrollment({ state: 'reported', report })))
    const { wrapper } = await mountPage()
    const opts = (n: string) =>
      wrapper.findAll(`select[name=${n}] option`).map((o) => o.attributes('value'))
    expect(opts('disk_storage')).toEqual(['local-lvm'])
    expect(opts('iso_storage')).toEqual(['nebla-iso'])
    expect(opts('import_storage')).toEqual(['', 'nebla-iso', 'nebla-imp'])
    expect((wrapper.find('input[name=url]').element as HTMLInputElement).value).toBe(
      'https://10.0.0.1:8006',
    )
    expect(wrapper.text()).toContain('CC:DD')
    expect(button(wrapper, 'Connect location')).toBeTruthy()
  })

  describe('finish prefill', () => {
    const wide = {
      ...report,
      storages: [
        ...report.storages,
        { id: 'ceph', type: 'rbd', content: ['images'], shared: true, nebla_iso: false },
        { id: 'nebla-iso2', type: 'nfs', content: ['iso'], shared: true, nebla_iso: true },
      ],
    }
    const loc = (over: Partial<AdminLocation> = {}): AdminLocation => ({
      id: 3,
      name: 'home',
      description: '',
      pve: {
        url: 'https://pve.example:8006',
        token_id: 't',
        has_secret: true,
        ca_fingerprint: '',
        insecure_tls: false,
      },
      disk_storage: 'ceph',
      iso_storage: 'nebla-iso2',
      import_storage: 'nebla-imp',
      firewall_enabled: null,
      nodes: [],
      created: '2026-01-01T00:00:00Z',
      updated: '2026-01-01T00:00:00Z',
      ...over,
    })
    const locations = (l: AdminLocation) =>
      http.get('*/v1/admin/locations', () =>
        json(200, {
          locations: [l],
          meta: { pagination: { page: 1, per_page: 50, total_entries: 1 } },
        }),
      )
    const val = (w: { find: (s: string) => { element: Element } }, sel: string) =>
      (w.find(sel).element as HTMLInputElement | HTMLSelectElement).value
    const urlOpts = (w: { findAll: (s: string) => { attributes: (n: string) => unknown }[] }) =>
      w.findAll('select[name=node_url] option').map((o) => o.attributes('value'))

    it('a re-run keeps the current URL and storages', async () => {
      server.use(
        get(enrollment({ state: 'reported', report: wide, location_id: 3 })),
        locations(loc()),
      )
      const { wrapper } = await mountPage()
      await vi.waitFor(() => expect(wrapper.find('input[name=url]').exists()).toBe(true))
      expect(val(wrapper, 'input[name=url]')).toBe('https://pve.example:8006')
      expect(val(wrapper, 'select[name=disk_storage]')).toBe('ceph')
      expect(val(wrapper, 'select[name=iso_storage]')).toBe('nebla-iso2')
      expect(val(wrapper, 'select[name=import_storage]')).toBe('nebla-imp')
      expect(urlOpts(wrapper)).toEqual(['', 'https://10.0.0.1:8006', 'https://10.0.0.2:8006'])
      expect(button(wrapper, 'Save')).toBeTruthy()
    })

    it('a re-run falls back to defaults for storages the node no longer offers', async () => {
      server.use(
        get(enrollment({ state: 'reported', report: wide, location_id: 3 })),
        locations(loc({ disk_storage: 'gone', iso_storage: 'local', import_storage: 'gone' })),
      )
      const { wrapper } = await mountPage()
      await vi.waitFor(() => expect(wrapper.find('input[name=url]').exists()).toBe(true))
      expect(val(wrapper, 'input[name=url]')).toBe('https://pve.example:8006')
      expect(val(wrapper, 'select[name=disk_storage]')).toBe('local-lvm')
      expect(val(wrapper, 'select[name=iso_storage]')).toBe('nebla-iso')
      expect(val(wrapper, 'select[name=import_storage]')).toBe('')
    })

    it('a new enrollment keeps the defaults', async () => {
      server.use(get(enrollment({ state: 'reported', report: wide })), locations(loc()))
      const { wrapper } = await mountPage()
      await vi.waitFor(() => expect(wrapper.find('input[name=url]').exists()).toBe(true))
      expect(val(wrapper, 'input[name=url]')).toBe('https://10.0.0.1:8006')
      expect(val(wrapper, 'select[name=disk_storage]')).toBe('local-lvm')
      expect(val(wrapper, 'select[name=iso_storage]')).toBe('nebla-iso')
      expect(val(wrapper, 'select[name=import_storage]')).toBe('')
      expect(urlOpts(wrapper)).toEqual(['', 'https://10.0.0.2:8006'])
    })

    it('brackets IPv6 and skips nodes without an ip', async () => {
      const v6 = {
        ...report,
        nodes: [
          { name: 'pve1', ip: '2001:db8::1', online: true, fingerprint: 'AA:BB' },
          { name: 'pve2', ip: '', online: false, fingerprint: 'CC:DD' },
          { name: 'pve3', ip: 'fd00::3', online: true, fingerprint: 'EE:FF' },
        ],
      }
      server.use(get(enrollment({ state: 'reported', report: v6 })))
      const { wrapper } = await mountPage()
      expect(val(wrapper, 'input[name=url]')).toBe('https://[2001:db8::1]:8006')
      expect(urlOpts(wrapper)).toEqual(['', 'https://[fd00::3]:8006'])
    })
  })

  it.each([
    ['the server', { expired: true }],
    ['the client clock', { expires_at: new Date(Date.now() - 1000).toISOString() }],
  ])('shows an expired report without the finish form (%s)', async (_, over) => {
    server.use(get(enrollment({ state: 'reported', report, ...over })))
    const { wrapper } = await mountPage()
    expect(wrapper.text()).toContain('This report expired. Cancel and run setup again.')
    expect(wrapper.find('form[data-testid=enroll-finish]').exists()).toBe(false)
    expect(button(wrapper, 'Cancel')).toBeTruthy()
  })

  it('shows the fingerprint mismatch under the form', async () => {
    remember()
    server.use(
      get(enrollment({ state: 'reported', report })),
      http.post('*/v1/admin/location_enrollments/7/finish', () =>
        json(422, {
          error: { code: 'fingerprint_mismatch', message: 'Certificate does not match, saw ZZ.' },
        }),
      ),
    )
    const { wrapper } = await mountPage()
    await wrapper.find('form[data-testid=enroll-finish]').trigger('submit')
    await flushPromises()
    expect(wrapper.text()).toContain('Certificate does not match, saw ZZ.')
  })

  it('finishes and shows the cleanup alert once on the location page', async () => {
    remember()
    let sent: unknown
    const location: AdminLocation = {
      id: 3,
      name: 'home',
      description: '',
      pve: {
        url: 'https://10.0.0.1:8006',
        token_id: 't',
        has_secret: true,
        ca_fingerprint: '',
        insecure_tls: false,
      },
      disk_storage: 'local-lvm',
      iso_storage: 'nebla-iso',
      import_storage: '',
      firewall_enabled: null,
      nodes: [],
      created: '2026-01-01T00:00:00Z',
      updated: '2026-01-01T00:00:00Z',
    }
    server.use(
      get(enrollment({ state: 'reported', report })),
      http.get('*/v1/admin/locations', () =>
        json(200, {
          locations: [location],
          meta: { pagination: { page: 1, per_page: 50, total_entries: 1 } },
        }),
      ),
      http.post('*/v1/admin/location_enrollments/7/finish', async ({ request }) => {
        sent = await request.json()
        return json(200, { location, cleanup: ['pveum user token remove nebla@pve old'] })
      }),
    )
    const { wrapper, router } = await mountPage()
    await wrapper.find('form[data-testid=enroll-finish]').trigger('submit')
    await flushPromises()
    expect(sent).toEqual({
      url: 'https://10.0.0.1:8006',
      disk_storage: 'local-lvm',
      iso_storage: 'nebla-iso',
    })
    await vi.waitFor(() => expect(router.currentRoute.value.name).toBe('admin-location'))
    await flushPromises()
    expect(wrapper.find('[data-testid=enroll-cleanup]').text()).toContain(
      'pveum user token remove nebla@pve old',
    )
    await router.push('/admin/locations')
    await router.push('/admin/locations/3')
    await flushPromises()
    expect(wrapper.find('[data-testid=enroll-cleanup]').exists()).toBe(false)
  })

  it('cancel deletes and returns to the list', async () => {
    remember()
    let deleted = false
    server.use(
      get(enrollment()),
      http.delete('*/v1/admin/location_enrollments/7', () => {
        deleted = true
        return new Response(null, { status: 204 })
      }),
    )
    const { router, wrapper } = await mountPage()
    await button(wrapper, 'Cancel').trigger('click')
    await vi.waitFor(() => expect(deleted).toBe(true))
    await flushPromises()
    expect(router.currentRoute.value.name).toBe('admin-locations')
  })

  it('stops polling once reported', async () => {
    remember()
    let calls = 0
    server.use(
      http.get('*/v1/admin/location_enrollments/7', () => {
        calls++
        return json(200, enrollment({ state: 'reported', report }))
      }),
    )
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const { wrapper } = await mountPage()
    await vi.waitFor(() => expect(wrapper.find('select[name=disk_storage]').exists()).toBe(true))
    const seen = calls
    await vi.advanceTimersByTimeAsync(10_000)
    expect(calls).toBe(seen)
  })

  it('disables Connect when no disk storage was reported', async () => {
    remember()
    server.use(get(enrollment({ state: 'reported', report: { ...report, storages: [] } })))
    const { wrapper } = await mountPage()
    expect(wrapper.text()).toContain('reported no usable storage')
    expect(button(wrapper, 'Connect location').attributes('disabled')).toBeDefined()
  })

  it('clears cleanup from history so a remount shows no alert', async () => {
    const location = {
      id: 3,
      name: 'home',
      description: '',
      pve: {
        url: 'https://x:8006',
        token_id: 't',
        has_secret: true,
        ca_fingerprint: '',
        insecure_tls: false,
      },
      disk_storage: 'a',
      iso_storage: 'b',
      import_storage: '',
      firewall_enabled: null,
      nodes: [],
      created: '2026-01-01T00:00:00Z',
      updated: '2026-01-01T00:00:00Z',
    }
    server.use(
      http.get('*/v1/admin/locations', () =>
        json(200, {
          locations: [location],
          meta: { pagination: { page: 1, per_page: 50, total_entries: 1 } },
        }),
      ),
    )
    const stub = { render: () => null }
    const mk = () =>
      createRouter({
        history: createWebHistory(),
        routes: [
          {
            path: '/admin/locations/:locationId',
            name: 'admin-location',
            component: LocationDetailPage,
          },
          { path: '/admin/locations', name: 'admin-locations', component: stub },
          { path: '/:pathMatch(.*)*', component: stub },
        ],
      })
    const router = mk()
    const first = mountApp(RouterView, { router })
    mounted.push(first.wrapper)
    await router.isReady()
    await router.push({
      name: 'admin-location',
      params: { locationId: 3 },
      state: { cleanup: ['pveum x'] },
    })
    await vi.waitFor(() =>
      expect(first.wrapper.find('[data-testid=enroll-cleanup]').exists()).toBe(true),
    )
    expect(window.history.state.cleanup).toBeNull()
    first.wrapper.unmount()
    const router2 = mk()
    await router2.push('/admin/locations/3')
    const second = mountApp(RouterView, { router: router2 })
    mounted.push(second.wrapper)
    await vi.waitFor(() => expect(second.wrapper.text()).toContain('Nodes'))
    expect(second.wrapper.find('[data-testid=enroll-cleanup]').exists()).toBe(false)
  })
})

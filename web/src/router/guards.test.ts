import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { defineComponent, h } from 'vue'
import { flushPromises } from '@vue/test-utils'
import { http } from 'msw'
import { createMemoryHistory, RouterView, type Router } from 'vue-router'
import { json, server } from '@/test/server'
import { mountApp } from '@/test/mount'
import { queryClient } from '@/lib/queryClient'
import { resetSession, setMe } from '@/lib/session'
import { api, setProjectId } from '@/api/client'
import { createAppRouter } from './index'
import { safeNext } from './guards'

const manifest = (over: Record<string, unknown> = {}) => ({
  setup_required: false,
  signup_enabled: false,
  branding: { product_name: 'Nebla', logo_url: '', primary_color: '' },
  admin_sections: [],
  ...over,
})
const user = (over: Record<string, unknown> = {}) => ({
  id: 1,
  email: 'kai@example.com',
  name: 'Kai',
  is_admin: false,
  totp_enabled: false,
  ...over,
})
const project = (id: number) => ({
  id,
  name: `Project ${id}`,
  role: 'owner',
  quota: { max_servers: 5, max_vcpu: 8, max_memory_mb: 8192, max_disk_gb: 100, max_ips: 5 },
  usage: { servers: 0, vcpu: 0, memory_mb: 0, disk_gb: 0, ips: 0 },
  created: '2026-11-18T10:00:00Z',
})

const Stub = defineComponent({ render: () => h('div', 'stub') })

function appRouter(): Router {
  const router = createAppRouter(createMemoryHistory())
  router.addRoute('project', { path: 'servers', name: 'servers', component: Stub })
  return router
}

function api_(opts: {
  manifest?: object
  me?: object | null
  projects?: object[]
  hidden?: 403 | 404
}) {
  const projects = opts.projects ?? []
  server.use(
    http.get('*/v1/ui/manifest', () => json(200, opts.manifest ?? manifest())),
    http.get('*/v1/me', () =>
      opts.me === null
        ? json(401, { error: { code: 'unauthorized', message: 'no session' } })
        : json(200, opts.me ?? user()),
    ),
    http.get('*/v1/projects', () => json(200, { projects })),
    http.get('*/v1/projects/:id', ({ params }) => {
      const p = projects.find((x) => (x as { id: number }).id === Number(params.id))
      return p
        ? json(200, p)
        : json(404, { error: { code: 'not_found', message: 'project not found' } })
    }),
  )
}

const readyLists = {
  locations: [{ id: 1, name: 'home', nodes: [{ id: 1, name: 'n1', status: 'online' }] }],
  server_types: [{ id: 1, name: 'small' }],
  ip_pools: [
    {
      id: 1,
      gateway: '10.0.0.1',
      range_start: '10.0.0.10',
      range_end: '10.0.0.20',
      allocated: 0,
    },
  ],
  catalog: [{ key: 'debian-13', locations: [{ location_id: 1, state: 'ready' }] }],
}

function adminLists(over: Record<string, unknown[]> = {}) {
  const meta = { pagination: { page: 1, per_page: 50, total_entries: 0 } }
  server.use(
    http.get('*/v1/admin/locations', () => json(200, { locations: over.locations ?? [], meta })),
    http.get('*/v1/admin/server_types', () =>
      json(200, { server_types: over.server_types ?? [], meta }),
    ),
    http.get('*/v1/admin/ip_pools', () => json(200, { ip_pools: over.ip_pools ?? [], meta })),
    http.get('*/v1/admin/images', () => json(200, { images: over.images ?? [], meta })),
    http.get('*/v1/admin/catalog', () => json(200, { catalog: over.catalog ?? [] })),
  )
}

beforeEach(() => {
  resetSession()
  queryClient.clear()
  setProjectId(null)
  localStorage.clear()
})
afterEach(() => {
  document.body.innerHTML = ''
})

describe('router guards', () => {
  it('setup_required redirects to /setup', async () => {
    api_({ manifest: manifest({ setup_required: true }), me: null })
    const router = appRouter()
    await router.push('/')
    expect(router.currentRoute.value.name).toBe('setup')
  })

  it('unauthenticated goes to login with next', async () => {
    api_({ me: null })
    const router = appRouter()
    await router.push('/p/1/servers')
    expect(router.currentRoute.value.fullPath).toBe('/login?next=/p/1/servers')
  })

  it('root picks last project', async () => {
    api_({ projects: [project(2), project(3)] })
    localStorage.setItem('nebla.lastProject', '3')
    const router = appRouter()
    await router.push('/')
    expect(router.currentRoute.value.fullPath).toBe('/p/3/servers')
  })

  it('root falls back to the first project', async () => {
    api_({ projects: [project(2), project(3)] })
    localStorage.setItem('nebla.lastProject', '8')
    const router = appRouter()
    await router.push('/')
    expect(router.currentRoute.value.fullPath).toBe('/p/2/servers')
  })

  it('no projects goes to projects page', async () => {
    api_({ projects: [] })
    const router = appRouter()
    await router.push('/')
    expect(router.currentRoute.value.name).toBe('projects')
  })

  it('root picks get-ready for an unready admin', async () => {
    api_({ me: user({ is_admin: true }), projects: [project(3)] })
    adminLists()
    const router = appRouter()
    await router.push('/')
    expect(router.currentRoute.value.name).toBe('admin-get-ready')
  })

  it('root skips get-ready once ready', async () => {
    api_({ me: user({ is_admin: true }), projects: [project(3)] })
    adminLists(readyLists)
    const router = appRouter()
    await router.push('/')
    expect(router.currentRoute.value.fullPath).toBe('/p/3/servers')
  })

  it('/admin goes to get-ready until ready, then locations', async () => {
    api_({ me: user({ is_admin: true }), projects: [project(3)] })
    adminLists()
    const router = appRouter()
    await router.push('/admin')
    expect(router.currentRoute.value.name).toBe('admin-get-ready')
  })

  it('/admin goes to locations once ready', async () => {
    api_({ me: user({ is_admin: true }), projects: [project(3)] })
    adminLists(readyLists)
    const router = appRouter()
    await router.push('/admin')
    expect(router.currentRoute.value.name).toBe('admin-locations')
  })

  it('a failed readiness check falls through to the project pick', async () => {
    api_({ me: user({ is_admin: true }), projects: [project(3)] })
    server.use(
      http.get('*/v1/admin/locations', () =>
        json(500, { error: { code: 'internal', message: 'boom' } }),
      ),
    )
    const router = appRouter()
    await router.push('/')
    expect(router.currentRoute.value.fullPath).toBe('/p/3/servers')
  })

  it('a failed readiness check sends /admin to locations', async () => {
    api_({ me: user({ is_admin: true }), projects: [project(3)] })
    server.use(
      http.get('*/v1/admin/server_types', () =>
        json(500, { error: { code: 'internal', message: 'boom' } }),
      ),
    )
    const router = appRouter()
    await router.push('/admin')
    expect(router.currentRoute.value.name).toBe('admin-locations')
  })

  it('a non-admin makes no admin fetch', async () => {
    api_({ projects: [project(3)] })
    let hits = 0
    server.use(http.get('*/v1/admin/*', () => (hits++, json(200, {}))))
    const router = appRouter()
    await router.push('/')
    expect(router.currentRoute.value.fullPath).toBe('/p/3/servers')
    expect(hits).toBe(0)
  })

  it('non-admin on /admin gets not-found', async () => {
    api_({ projects: [project(2)] })
    const router = appRouter()
    await router.push('/admin')
    expect(router.currentRoute.value.name).toBe('not-found')
    expect(router.currentRoute.value.path).toBe('/admin')
  })

  it('logged-in user on /login goes to root', async () => {
    api_({ projects: [project(2)] })
    const router = appRouter()
    await router.push('/login')
    expect(router.currentRoute.value.fullPath).toBe('/p/2/servers')
  })

  it('unknown project id shows not found', async () => {
    api_({ projects: [project(2)] })
    const router = appRouter()
    await router.push('/p/9/servers')
    expect(router.currentRoute.value.name).toBe('not-found')
    expect(router.currentRoute.value.path).toBe('/p/9/servers')

    const App = defineComponent({ render: () => h(RouterView) })
    const { wrapper } = mountApp(App, { router, queryClient })
    await flushPromises()
    expect(wrapper.text()).toContain("We can't find that page")
    const link = wrapper.find('a[href="/projects"]')
    expect(link.exists()).toBe(true)
  })
})

describe('session expiry', () => {
  it('drops the cache so the next user never sees the previous project', async () => {
    api_({ projects: [project(1)] })
    const router = appRouter()
    await router.push('/p/1/servers')
    const App = defineComponent({ render: () => h(RouterView) })
    const { wrapper } = mountApp(App, { router, queryClient })
    await flushPromises()
    expect(wrapper.text()).toContain('Project 1')

    server.use(
      http.get('*/v1/servers', () =>
        json(401, { error: { code: 'unauthorized', message: 'expired' } }),
      ),
    )
    await api.GET('/servers')
    await flushPromises()
    expect(router.currentRoute.value.name).toBe('login')
    expect(queryClient.getQueryData(['project', 1])).toBeUndefined()
    expect(queryClient.getQueryData(['projects'])).toBeUndefined()

    api_({ me: user({ id: 2, email: 'bo@example.com' }), projects: [project(5)], hidden: 403 })
    setMe(user({ id: 2, email: 'bo@example.com' }))
    await router.replace('/p/1/servers')
    await flushPromises()
    expect(router.currentRoute.value.name).toBe('not-found')
    expect(wrapper.text()).not.toContain('Project 1')
    expect(wrapper.text()).toContain("We can't find that page")
  })
})

describe('safeNext', () => {
  it('accepts same-origin paths only', () => {
    expect(safeNext('/p/1/servers?x=1')).toBe('/p/1/servers?x=1')
    expect(safeNext('//evil.example')).toBeNull()
    expect(safeNext('/\\evil.example')).toBeNull()
    expect(safeNext('https://evil.example')).toBeNull()
    expect(safeNext(['/a'])).toBeNull()
    expect(safeNext(undefined)).toBeNull()
  })
})

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, h } from 'vue'
import { flushPromises } from '@vue/test-utils'
import { http } from 'msw'
import { createMemoryHistory, RouterView, type Router } from 'vue-router'
import { json, server } from '@/test/server'
import { mountApp } from '@/test/mount'
import { queryClient } from '@/lib/queryClient'
import { resetSession, setMe } from '@/lib/session'
import { api } from '@/api/client'
import App from '@/App.vue'
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

function appRouter(reload?: (url: string) => void): Router {
  const router = createAppRouter(createMemoryHistory(), reload)
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
  localStorage.clear()
  sessionStorage.clear()
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

describe('project header', () => {
  it('follows the final URL when project navigations overlap', async () => {
    api_({ projects: [project(1), project(2), project(3)] })
    const router = appRouter()
    await router.push('/p/1/servers')

    let release!: () => void
    let reached!: () => void
    const hit = new Promise<void>((r) => (reached = r))
    const gate = new Promise<void>((r) => (release = r))
    const seen: (string | null)[] = []
    server.use(
      http.get('*/v1/projects/2', async () => {
        reached()
        await gate
        return json(200, project(2))
      }),
      http.get('*/v1/servers', ({ request }) => {
        seen.push(request.headers.get('Project-ID'))
        return json(200, { servers: [] })
      }),
      http.post('*/v1/servers', ({ request }) => {
        seen.push(request.headers.get('Project-ID'))
        return json(422, { error: { code: 'invalid_input', message: 'x' } })
      }),
    )

    const slow = router.push('/p/2/servers')
    await hit
    await router.push('/p/3/servers')
    release()
    await slow
    await flushPromises()

    expect(router.currentRoute.value.fullPath).toBe('/p/3/servers')
    await api.GET('/servers')
    await api.POST('/servers', { body: {} as never })
    expect(seen).toEqual(['3', '3'])
  })
})

describe('load errors', () => {
  const tryAgain = () =>
    Array.from(document.body.querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === 'Try again',
    )

  it('shows an error screen when the manifest fails, and Try again recovers', async () => {
    server.use(
      http.get('*/v1/ui/manifest', () =>
        json(500, { error: { code: 'internal', message: 'database is down' } }),
      ),
    )
    const router = appRouter()
    mountApp(App, { router, queryClient })
    await router.push('/p/1/servers').catch(() => {})
    await flushPromises()
    expect(document.body.textContent).toContain('database is down')
    expect(tryAgain()).toBeDefined()

    api_({ projects: [project(1)] })
    tryAgain()!.click()
    await vi.waitFor(() => expect(router.currentRoute.value.fullPath).toBe('/p/1/servers'))
    await flushPromises()
    expect(document.body.textContent).not.toContain('database is down')
    expect(tryAgain()).toBeUndefined()
  })

  it('shows the error screen when /me fails on first load', async () => {
    api_({})
    server.use(
      http.get('*/v1/me', () => json(502, { error: { code: 'pve_unreachable', message: 'x' } })),
    )
    const router = appRouter()
    mountApp(App, { router, queryClient })
    await router.push('/').catch(() => {})
    await flushPromises()
    expect(document.body.textContent).toContain("The server's host didn't respond.")
    expect(tryAgain()).toBeDefined()
  })

  it('reloads once on a chunk load failure, then shows the error screen', async () => {
    api_({ projects: [project(1)] })
    const broken = () =>
      Promise.reject(new TypeError('Failed to fetch dynamically imported module: /assets/a.js'))
    const reload = vi.fn()

    const first = appRouter(reload)
    first.addRoute('project', { path: 'broken', component: broken })
    await first.push('/p/1/broken').catch(() => {})
    expect(reload).toHaveBeenCalledOnce()
    expect(reload).toHaveBeenCalledWith('/p/1/broken')

    // The reloaded page fails the same way: no second reload.
    const second = appRouter(reload)
    second.addRoute('project', { path: 'broken', component: broken })
    mountApp(App, { router: second, queryClient })
    await second.push('/p/1/broken').catch(() => {})
    await flushPromises()
    expect(reload).toHaveBeenCalledOnce()
    expect(document.body.textContent).toContain('Failed to fetch dynamically imported module')
    expect(tryAgain()).toBeDefined()
  })

  it('a good navigation lets a later chunk failure reload again', async () => {
    api_({ projects: [project(1)] })
    sessionStorage.setItem('nebla.chunkReload', '1')
    const reload = vi.fn()
    const router = appRouter(reload)
    router.addRoute('project', {
      path: 'broken',
      component: () => Promise.reject(new TypeError('error loading dynamically imported module')),
    })
    await router.push('/p/1/servers')
    await router.push('/p/1/broken').catch(() => {})
    expect(reload).toHaveBeenCalledOnce()
  })
})

describe('load errors from superseded navigations', () => {
  function slowRoute(router: Router, path: string, error: Error) {
    let release!: () => void
    let reached!: () => void
    const gate = new Promise<void>((r) => (release = r))
    const hit = new Promise<void>((r) => (reached = r))
    router.addRoute('project', {
      path,
      component: () => {
        reached()
        return gate.then(() => Promise.reject(error))
      },
    })
    return { release, hit }
  }

  async function race(error: Error, reload = vi.fn()) {
    api_({ projects: [project(1), project(2), project(3)] })
    const router = appRouter(reload)
    const slow = slowRoute(router, 'slow', error)
    const { wrapper } = mountApp(App, { router, queryClient })
    await router.push('/p/1/servers')
    const pending = router.push('/p/2/slow').catch(() => {})
    await slow.hit
    await router.push('/p/3/servers')
    slow.release()
    await pending
    await flushPromises()
    return { router, wrapper, reload }
  }

  it('a failed superseded navigation leaves the newer page alone', async () => {
    const { router, wrapper } = await race(new Error('boom'))
    expect(router.currentRoute.value.fullPath).toBe('/p/3/servers')
    expect(wrapper.text()).not.toContain('boom')
    expect(wrapper.text()).not.toContain('Try again')
    expect(wrapper.text()).toContain('stub')
  })

  it('a superseded chunk failure does not reload', async () => {
    const { reload, wrapper } = await race(
      new TypeError('Failed to fetch dynamically imported module: /assets/a.js'),
    )
    expect(reload).not.toHaveBeenCalled()
    expect(wrapper.text()).not.toContain('Try again')
  })

  it('the latest navigation failing still shows the screen', async () => {
    api_({ projects: [project(1)] })
    const router = appRouter()
    const slow = slowRoute(router, 'slow', new Error('boom'))
    const { wrapper } = mountApp(App, { router, queryClient })
    await router.push('/p/1/servers')
    const pending = router.push('/p/1/slow').catch(() => {})
    await slow.hit
    slow.release()
    await pending
    await flushPromises()
    expect(wrapper.text()).toContain('boom')
    expect(wrapper.text()).toContain('Try again')
  })

  it('a 401 during a navigation goes to login without the screen', async () => {
    api_({ projects: [project(1)] })
    const router = appRouter()
    const { wrapper } = mountApp(App, { router, queryClient })
    await router.push('/p/1/servers')
    server.use(
      http.get('*/v1/projects/2', () =>
        json(401, { error: { code: 'unauthorized', message: 'expired' } }),
      ),
    )
    await router.push('/p/2/servers').catch(() => {})
    await vi.waitFor(() => expect(router.currentRoute.value.name).toBe('login'))
    await flushPromises()
    expect(wrapper.text()).not.toContain('Try again')
    expect(wrapper.text()).not.toContain('expired')
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

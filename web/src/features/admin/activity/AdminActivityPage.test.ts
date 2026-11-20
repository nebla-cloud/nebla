import { afterEach, describe, expect, it, vi } from 'vitest'
import { http } from 'msw'
import { flushPromises, type VueWrapper } from '@vue/test-utils'
import { json, server } from '@/test/server'
import { mountApp, testRouter } from '@/test/mount'
import type { components } from '@/api/schema'
import { setMe } from '@/lib/session'
import { POLL_MS } from '@/features/activity/queries'
import AdminActivityPage from './AdminActivityPage.vue'

type AdminAction = components['schemas']['AdminAction']

function act(id: number, over: Partial<AdminAction> = {}): AdminAction {
  return {
    id,
    project_id: 4,
    project_name: 'Alpha',
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

const projects = [4, 5].map((id) => ({
  id,
  name: id === 4 ? 'Alpha' : 'Beta',
  quota: { max_servers: 0, max_vcpu: 0, max_memory_mb: 0, max_disk_gb: 0, max_ips: 0 },
  owner_email: 'a@example.com',
  usage: { servers: 0, vcpu: 0, memory_mb: 0, disk_gb: 0, ips: 0 },
  member_count: 1,
  created: '2026-03-04T10:00:00Z',
}))

let wrappers: VueWrapper[] = []
let urls: URL[] = []
let detailHits = 0

function serve(list: (call: number) => AdminAction[] = () => [act(1)], total = 1) {
  urls = []
  detailHits = 0
  server.use(
    http.get('*/v1/admin/actions', ({ request }) => {
      urls.push(new URL(request.url))
      return json(200, {
        actions: list(urls.length),
        meta: { pagination: { page: 1, per_page: 50, total_entries: total } },
      })
    }),
    http.get('*/v1/admin/actions/:id', () => {
      detailHits++
      return json(404, { error: { code: 'not_found', message: 'Not found' } })
    }),
    http.get('*/v1/admin/projects', () =>
      json(200, { projects, meta: { pagination: { page: 1, per_page: 50, total_entries: 2 } } }),
    ),
  )
}

async function mount(path = '/admin/activity') {
  setMe({ id: 1, name: 'Admin', email: 'admin@example.com', is_admin: true, totp_enabled: false })
  const router = testRouter([
    {
      path: '/admin/activity',
      name: 'admin-activity',
      component: { render: () => null },
      meta: { side: 'admin' },
    },
    {
      path: '/admin/projects/:projectId',
      name: 'admin-project',
      component: { render: () => null },
      meta: { side: 'admin' },
    },
  ])
  await router.push(path)
  const m = mountApp(AdminActivityPage, { router })
  wrappers.push(m.wrapper)
  return m
}

const button = (w: VueWrapper, label: string) =>
  w.findAll('button').find((b) => b.text() === label)!

afterEach(() => {
  vi.useRealTimers()
  wrappers.forEach((w) => w.unmount())
  wrappers = []
  setMe(null)
  document.body.innerHTML = ''
})

describe('AdminActivityPage', () => {
  it('rows show the project name linking to the admin project page', async () => {
    serve()
    const { wrapper } = await mount()
    await vi.waitFor(() => expect(wrapper.text()).toContain('Power on'))
    expect(wrapper.find('a[href="/admin/projects/4"]').text()).toBe('Alpha')
    expect(urls[0]!.searchParams.get('sort')).toBe('id:desc')
    expect(urls[0]!.searchParams.get('per_page')).toBe('50')
    expect(urls[0]!.searchParams.get('project_id')).toBeNull()
    expect(urls[0]!.searchParams.get('status')).toBeNull()
  })

  it('the status filter writes to the URL query', async () => {
    serve()
    const { wrapper, router } = await mount('/admin/activity?page=2')
    await vi.waitFor(() => expect(urls.length).toBe(1))
    await button(wrapper, 'Failed').trigger('click')
    await vi.waitFor(() => expect(router.currentRoute.value.query.status).toBe('error'))
    expect(router.currentRoute.value.query.page).toBeUndefined()
    await vi.waitFor(() => expect(urls.at(-1)!.searchParams.getAll('status')).toEqual(['error']))
    await button(wrapper, 'All').trigger('click')
    await vi.waitFor(() => expect(router.currentRoute.value.query.status).toBeUndefined())
  })

  it('the status filter reads from the URL query', async () => {
    serve()
    const { wrapper } = await mount('/admin/activity?status=success')
    await vi.waitFor(() => expect(urls.length).toBe(1))
    expect(urls[0]!.searchParams.getAll('status')).toEqual(['success'])
    expect(button(wrapper, 'Succeeded').attributes('aria-pressed')).toBe('true')
  })

  it('the project filter writes to the URL query', async () => {
    serve()
    const { wrapper, router } = await mount()
    await vi.waitFor(() => expect(wrapper.findAll('select option').length).toBe(3))
    await wrapper.find('select').setValue('5')
    await vi.waitFor(() => expect(router.currentRoute.value.query.project).toBe('5'))
    await vi.waitFor(() => expect(urls.at(-1)!.searchParams.get('project_id')).toBe('5'))
  })

  it('?project=4 sends project_id=4', async () => {
    serve()
    const { wrapper } = await mount('/admin/activity?project=4')
    await vi.waitFor(() => expect(urls.length).toBe(1))
    expect(urls[0]!.searchParams.get('project_id')).toBe('4')
    await vi.waitFor(() => expect(wrapper.findAll('select option').length).toBe(3))
    expect((wrapper.find('select').element as HTMLSelectElement).value).toBe('4')
  })

  it('details render from the list payload without a second request', async () => {
    serve(() => [
      act(3, {
        status: 'error',
        progress: 30,
        error: { code: 'failed', message: 'It failed.', detail: 'clone: VMID 4242 on pve1' },
      }),
    ])
    const { wrapper, queryClient } = await mount()
    await vi.waitFor(() => expect(wrapper.text()).toContain('It failed.'))
    await button(wrapper, 'Technical details').trigger('click')
    expect(wrapper.text()).toContain('clone: VMID 4242 on pve1')
    await flushPromises()
    expect(queryClient.isFetching()).toBe(0)
    expect(detailHits).toBe(0)
  })

  it('polls while any row is running and stops when none are', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    serve((n) => [n < 3 ? act(1, { status: 'running', progress: 10, finished: null }) : act(1)])
    const { wrapper } = await mount()
    await vi.waitFor(() => expect(wrapper.text()).toContain('Running 10%'))
    await vi.advanceTimersByTimeAsync(POLL_MS)
    await vi.waitFor(() => expect(urls.length).toBe(2))
    await vi.advanceTimersByTimeAsync(POLL_MS)
    await vi.waitFor(() => expect(wrapper.text()).toContain('Done'))
    expect(urls.length).toBe(3)
    await vi.advanceTimersByTimeAsync(POLL_MS * 3)
    expect(urls.length).toBe(3)
  })

  it('an empty page past the end moves back to the last page', async () => {
    serve(() => [], 1)
    const { router } = await mount('/admin/activity?page=4&status=error')
    await vi.waitFor(() => expect(router.currentRoute.value.query.page).toBeUndefined())
    expect(router.currentRoute.value.query.status).toBe('error')
  })
})

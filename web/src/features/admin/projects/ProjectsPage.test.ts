import { afterEach, describe, expect, it, vi } from 'vitest'
import { http } from 'msw'
import type { VueWrapper } from '@vue/test-utils'
import { json, server } from '@/test/server'
import { mountApp, testRouter } from '@/test/mount'
import ProjectsPage from './ProjectsPage.vue'

const quota = { max_servers: 5, max_vcpu: 0, max_memory_mb: 0, max_disk_gb: 0, max_ips: 0 }
const usage = { servers: 3, vcpu: 6, memory_mb: 8192, disk_gb: 100, ips: 3 }
const projects = [
  {
    id: 7,
    name: 'Alpha',
    quota,
    owner_email: 'ada@example.com',
    usage,
    member_count: 2,
    created: '2026-03-04T10:00:00Z',
  },
  {
    id: 8,
    name: 'Beta',
    quota: { ...quota, max_servers: 0 },
    owner_email: 'bo@example.com',
    usage,
    member_count: 1,
    created: '2026-04-05T10:00:00Z',
  },
]

let wrappers: VueWrapper[] = []

function serve() {
  server.use(
    http.get('*/v1/admin/projects', () =>
      json(200, {
        projects,
        meta: { pagination: { page: 1, per_page: 50, total_entries: 2 } },
      }),
    ),
  )
}

function mount() {
  const m = mountApp(ProjectsPage, { router: testRouter() })
  wrappers.push(m.wrapper)
  return m
}

afterEach(() => {
  wrappers.forEach((w) => w.unmount())
  wrappers = []
  document.body.innerHTML = ''
})

describe('ProjectsPage', () => {
  it('shows the columns', async () => {
    serve()
    const { wrapper } = mount()
    await vi.waitFor(() => expect(wrapper.text()).toContain('Alpha'))
    const head = wrapper.find('thead').text()
    for (const h of ['Name', 'Owner email', 'Servers', 'Members', 'Created']) {
      expect(head).toContain(h)
    }
    const row = wrapper.findAll('tbody tr')[0]!.text()
    expect(row).toContain('ada@example.com')
    expect(row).toContain('3 of 5')
    expect(row).toContain('2')
    expect(row).toContain('4 Mar 2026')
  })

  it('zero quota reads no limit', async () => {
    serve()
    const { wrapper } = mount()
    await vi.waitFor(() => expect(wrapper.text()).toContain('Beta'))
    const row = wrapper.findAll('tbody tr')[1]!.text()
    expect(row).toContain('No limit')
    expect(row).not.toContain('of')
  })

  it('the row links to the detail page', async () => {
    serve()
    const { wrapper } = mount()
    await vi.waitFor(() => expect(wrapper.text()).toContain('Alpha'))
    expect(wrapper.find('a[href="/admin/projects/7"]').exists()).toBe(true)
  })
})

// Copyright (C) 2026 The Nebla contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { http } from 'msw'
import { flushPromises, type VueWrapper } from '@vue/test-utils'
import { json, server } from '@/test/server'
import { mountApp, testQueryClient, testRouter } from '@/test/mount'
import { resetSession } from '@/lib/session'
import ProjectDetailPage from './ProjectDetailPage.vue'

const quota = {
  max_servers: 5,
  max_vcpu: 0,
  max_memory_mb: 16384,
  max_disk_gb: 200,
  max_ips: 0,
  max_volumes: 5,
  max_volume_gb: 100,
}
const detail = {
  id: 7,
  name: 'Alpha',
  quota,
  owner_email: 'ada@example.com',
  usage: { servers: 3, vcpu: 6, memory_mb: 8192, disk_gb: 100, ips: 3, volumes: 1, volume_gb: 20 },
  member_count: 4,
  created: '2026-03-04T10:00:00Z',
  members: [
    { user_id: 1, email: 'ada@example.com', name: 'Ada', role: 'owner' },
    { user_id: 2, email: 'bo@example.com', name: 'Bo', role: 'admin' },
    { user_id: 3, email: 'cy@example.com', name: 'Cy', role: 'member' },
    { user_id: 4, email: 'di@example.com', name: '', role: 'read_only' },
  ],
}

let wrappers: VueWrapper[] = []
let puts: unknown[] = []

function serve(extra: Parameters<typeof server.use> = []) {
  server.use(
    http.get('*/v1/admin/projects/7', () => json(200, detail)),
    http.put('*/v1/admin/projects/7/quota', async ({ request }) => {
      puts.push(await request.json())
      return json(200, quota)
    }),
    ...extra,
  )
}

async function mount(queryClient = testQueryClient()) {
  const router = testRouter([
    {
      path: '/admin/projects/:projectId',
      name: 'admin-project',
      component: { render: () => null },
    },
    { path: '/admin/projects', name: 'admin-projects', component: { render: () => null } },
    { path: '/projects', name: 'projects', component: { render: () => null } },
  ])
  await router.push('/admin/projects/7')
  await router.isReady()
  const m = mountApp(ProjectDetailPage, { router, queryClient })
  wrappers.push(m.wrapper)
  return m
}

const input = (name: string) =>
  document.body.querySelector(`input[name="${name}"]`) as HTMLInputElement
const buttons = () => [...document.body.querySelectorAll('button')]
const btn = (label: string) => buttons().find((b) => b.textContent?.trim() === label)!

async function ready(wrapper: VueWrapper) {
  await vi.waitFor(() => expect(wrapper.text()).toContain('Alpha'))
}

async function setValue(name: string, v: string) {
  const el = input(name)
  el.value = v
  el.dispatchEvent(new Event('input', { bubbles: true }))
  await vi.waitFor(() => expect(input(name).value).toBe(v))
}

beforeEach(() => {
  puts = []
  resetSession()
})

afterEach(() => {
  wrappers.forEach((w) => w.unmount())
  wrappers = []
  document.body.innerHTML = ''
})

describe('ProjectDetailPage', () => {
  it('shows usage rows with memory in GB', async () => {
    serve()
    const { wrapper } = await mount()
    await ready(wrapper)
    const text = wrapper.text()
    for (const l of ['Servers', 'vCPU', 'Memory', 'Disk', 'IPs']) expect(text).toContain(l)
    expect(wrapper.find('[data-row="memory_mb"]').text()).toContain('8 GB')
    expect(wrapper.find('[data-row="disk_gb"]').text()).toContain('100 GB')
    expect(wrapper.find('[data-row="servers"]').text()).toContain('3')
    expect(input('max_memory_mb').value).toBe('16')
    expect(input('max_disk_gb').value).toBe('200')
    expect(input('max_vcpu').value).toBe('')
  })

  it('empty limit sends 0 and memory goes back to MB', async () => {
    serve()
    const { wrapper } = await mount()
    await ready(wrapper)
    await setValue('max_servers', '')
    await setValue('max_memory_mb', '32')
    btn('Save limits').click()
    await vi.waitFor(() => expect(puts).toHaveLength(1))
    expect(puts[0]).toEqual({
      max_servers: 0,
      max_vcpu: 0,
      max_memory_mb: 32768,
      max_disk_gb: 200,
      max_ips: 0,
      max_volumes: 5,
      max_volume_gb: 100,
    })
  })

  it('below current use note', async () => {
    serve()
    const { wrapper } = await mount()
    await ready(wrapper)
    const note = 'Below current use. Nothing is removed, but new servers will be refused.'
    expect(wrapper.text()).not.toContain(note)
    await setValue('max_servers', '2')
    await vi.waitFor(() => expect(wrapper.text()).toContain(note))
  })

  it('keeps typed limits when a refetch returns the same quota', async () => {
    serve()
    const qc = testQueryClient()
    // A new but equal quota object, as a refetch can hand over.
    qc.setQueryDefaults(['admin', 'project'], { structuralSharing: false })
    const { wrapper, queryClient } = await mount(qc)
    await ready(wrapper)
    await setValue('max_servers', '9')
    const before = queryClient.getQueryData(['admin', 'project', 7])
    await queryClient.refetchQueries({ queryKey: ['admin', 'project'] })
    await vi.waitFor(() =>
      expect(queryClient.getQueryData(['admin', 'project', 7])).not.toBe(before),
    )
    await flushPromises()
    expect(input('max_servers').value).toBe('9')
  })

  it('lists members read-only with role words', async () => {
    serve()
    const { wrapper } = await mount()
    await ready(wrapper)
    const text = wrapper.find('[data-section="members"]').text()
    for (const w of ['Owner', 'Admin', 'Member', 'Read-only', 'bo@example.com']) {
      expect(text).toContain(w)
    }
    expect(wrapper.find('[data-section="members"] select').exists()).toBe(false)
  })

  it('links to the activity view', async () => {
    serve()
    const { wrapper } = await mount()
    await ready(wrapper)
    const a = wrapper.findAll('a').find((l) => l.text().includes('View activity'))!
    expect(a.attributes('href')).toBe('/admin/activity?project=7')
  })

  it('deletes by name and returns to the list', async () => {
    let deleted = 0
    serve([
      http.delete('*/v1/admin/projects/7', () => {
        deleted++
        return new Response(null, { status: 204 })
      }),
    ])
    const { wrapper, router, queryClient } = await mount()
    await ready(wrapper)
    await queryClient.prefetchQuery({
      queryKey: ['admin', 'projects', 1],
      queryFn: () => ({}),
    })
    btn('Delete project').click()
    await vi.waitFor(() => expect(input('confirm_name')).not.toBeNull())
    await setValue('confirm_name', 'Alpha')
    await vi.waitFor(() => expect(btn('Delete project').disabled).toBe(false))
    ;(document.body.querySelector('button[form="confirm-by-name"]') as HTMLElement).click()
    await vi.waitFor(() => expect(deleted).toBe(1))
    await vi.waitFor(() => expect(router.currentRoute.value.path).toBe('/admin/projects'))
    expect(queryClient.getQueryState(['admin', 'projects', 1])?.isInvalidated).toBe(true)
  })

  it('shows the API message on project_not_empty', async () => {
    serve([
      http.delete('*/v1/admin/projects/7', () =>
        json(409, {
          error: { code: 'project_not_empty', message: "Delete the project's servers and volumes first." },
        }),
      ),
    ])
    const { wrapper } = await mount()
    await ready(wrapper)
    btn('Delete project').click()
    await vi.waitFor(() => expect(input('confirm_name')).not.toBeNull())
    await setValue('confirm_name', 'Alpha')
    await vi.waitFor(() => expect(btn('Delete project').disabled).toBe(false))
    ;(document.body.querySelector('button[form="confirm-by-name"]') as HTMLElement).click()
    await vi.waitFor(() =>
      expect(document.body.textContent).toContain(
        'This project still has servers or volumes. Its members need to delete them first.',
      ),
    )
  })

  it('shows the not-found state on 404', async () => {
    server.use(
      http.get('*/v1/admin/projects/7', () =>
        json(404, { error: { code: 'not_found', message: 'nope' } }),
      ),
    )
    const { wrapper } = await mount()
    await vi.waitFor(() => expect(wrapper.text()).toContain("We can't find that page"))
  })
})

import { afterEach, describe, expect, it, vi } from 'vitest'
import { http } from 'msw'
import { flushPromises, type VueWrapper } from '@vue/test-utils'
import { json, server } from '@/test/server'
import { mountApp } from '@/test/mount'
import TechnicalDetails from './TechnicalDetails.vue'

let wrappers: VueWrapper[] = []
let calls = 0

function serve(status = 200) {
  calls = 0
  server.use(
    http.get('*/v1/admin/actions/:id', ({ params }) => {
      calls++
      if (status !== 200) return json(status, { error: { code: 'not_found', message: 'Nope' } })
      return json(200, {
        id: Number(params.id),
        project_id: 4,
        project_name: 'Alpha',
        command: 'create_server',
        status: 'error',
        progress: 30,
        resources: [],
        error: { code: 'failed', message: 'It failed.', detail: 'clone: VMID 4242 on pve1 locked' },
        started: '2026-11-20T10:00:00Z',
        finished: '2026-11-20T10:01:00Z',
      })
    }),
  )
}

function mount(props: Record<string, unknown>) {
  const m = mountApp(TechnicalDetails, { props })
  wrappers.push(m.wrapper)
  return m
}

const toggle = (w: VueWrapper) =>
  w.findAll('button').find((b) => b.text().includes('Technical details'))!

afterEach(() => {
  wrappers.forEach((w) => w.unmount())
  wrappers = []
  document.body.innerHTML = ''
})

describe('TechnicalDetails', () => {
  it('is closed by default and does not fetch', async () => {
    serve()
    const { wrapper, queryClient } = mount({ actionId: 9 })
    expect(toggle(wrapper).attributes('aria-expanded')).toBe('false')
    expect(wrapper.text()).not.toContain('VMID')
    await flushPromises()
    expect(queryClient.isFetching()).toBe(0)
    expect(calls).toBe(0)
  })

  it('opening fetches once and shows code and detail in monospace with copy', async () => {
    serve()
    const { wrapper } = mount({ actionId: 9 })
    await toggle(wrapper).trigger('click')
    await vi.waitFor(() => expect(wrapper.text()).toContain('clone: VMID 4242 on pve1 locked'))
    expect(toggle(wrapper).attributes('aria-expanded')).toBe('true')
    const mono = wrapper.find('.font-mono')
    expect(mono.text()).toContain('failed')
    expect(mono.text()).toContain('clone: VMID 4242')
    expect(wrapper.findAll('button').some((b) => b.text() === 'Copy')).toBe(true)
    await toggle(wrapper).trigger('click')
    await toggle(wrapper).trigger('click')
    await vi.waitFor(() => expect(wrapper.text()).toContain('clone: VMID 4242'))
    expect(calls).toBe(1)
  })

  it('a 404 shows that details are not available', async () => {
    serve(404)
    const { wrapper } = mount({ actionId: 9 })
    await toggle(wrapper).trigger('click')
    await vi.waitFor(() => expect(wrapper.text()).toContain("Details aren't available."))
  })

  it('a 403 shows that details are not available', async () => {
    serve(403)
    const { wrapper } = mount({ actionId: 9 })
    await toggle(wrapper).trigger('click')
    await vi.waitFor(() => expect(wrapper.text()).toContain("Details aren't available."))
  })

  it('given the detail it never fetches', async () => {
    serve()
    const { wrapper, queryClient } = mount({
      actionId: 9,
      detail: { code: 'timed_out', detail: 'task UPID:pve1 timed out' },
    })
    await toggle(wrapper).trigger('click')
    expect(wrapper.find('.font-mono').text()).toContain('timed_out')
    expect(wrapper.text()).toContain('task UPID:pve1 timed out')
    await flushPromises()
    expect(queryClient.isFetching()).toBe(0)
    expect(calls).toBe(0)
  })

  it('announces loading as a status', async () => {
    server.use(http.get('*/v1/admin/actions/:id', () => new Promise<Response>(() => {})))
    const { wrapper } = mount({ actionId: 9 })
    await toggle(wrapper).trigger('click')
    await vi.waitFor(() => expect(wrapper.find('[role="status"]').text()).toBe('Loading details'))
  })
})

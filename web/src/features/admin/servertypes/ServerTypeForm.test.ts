import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { http } from 'msw'
import { json, server } from '@/test/server'
import { mountApp } from '@/test/mount'
import ServerTypeForm from './ServerTypeForm.vue'

const locations = [
  { id: 1, name: 'home', description: '', nodes: [] },
  { id: 2, name: 'colo', description: '', nodes: [] },
]

let bodies: Record<string, unknown>[] = []
let existing: unknown[] = []

const typeRow = (over: Record<string, unknown>) => ({
  id: 1,
  name: 'x',
  description: '',
  cores: 1,
  memory_mb: 1024,
  disk_gb: 10,
  deprecated: false,
  locations: [],
  created_at: '2026-01-01T00:00:00Z',
  ...over,
})

function value(wrapper: ReturnType<typeof mountApp>['wrapper'], name: string): string {
  return (wrapper.find(`input[name="${name}"]`).element as HTMLInputElement).value
}

beforeEach(() => {
  document.body.innerHTML = ''
  bodies = []
  existing = []
  server.use(
    http.get('*/v1/admin/server_types', () => json(200, { server_types: existing, meta: {} })),
    http.get('*/v1/admin/locations', () => json(200, { locations, meta: {} })),
    http.post('*/v1/admin/server_types', async ({ request }) => {
      bodies.push((await request.json()) as Record<string, unknown>)
      return json(201, { id: 9 })
    }),
  )
})

async function fill(wrapper: ReturnType<typeof mountApp>['wrapper']) {
  await flushPromises()
  await wrapper.find('input[name="name"]').setValue('small')
  await wrapper.find('input[name="cores"]').setValue('2')
  await wrapper.find('input[name="memory_gb"]').setValue('4')
  await wrapper.find('input[name="disk_gb"]').setValue('40')
}

describe('ServerTypeForm', () => {
  it('server type form converts GB to memory_mb', async () => {
    const { wrapper } = mountApp(ServerTypeForm)
    await fill(wrapper)
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(bodies).toHaveLength(1)
    expect(bodies[0]).toMatchObject({ name: 'small', cores: 2, memory_mb: 4096, disk_gb: 40 })
  })

  it('server type restricted to locations sends ids', async () => {
    const { wrapper } = mountApp(ServerTypeForm)
    await fill(wrapper)
    await wrapper.find('input[name="scope"][value="some"]').setValue(true)
    await flushPromises()
    await wrapper.find('input[name="location-1"]').setValue(true)
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(bodies[0]!.locations).toEqual([1])

    await wrapper.find('input[name="scope"][value="all"]').setValue(true)
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(bodies).toHaveLength(2)
    expect('locations' in bodies[1]!).toBe(false)
  })

  it('picking a template fills the fields', async () => {
    const { wrapper } = mountApp(ServerTypeForm)
    await flushPromises()
    await wrapper.find('button[data-template="nano"]').trigger('click')
    expect(value(wrapper, 'name')).toBe('Nano')
    expect(value(wrapper, 'cores')).toBe('1')
    expect(value(wrapper, 'memory_gb')).toBe('0.5')
    expect(value(wrapper, 'disk_gb')).toBe('10')
    expect(wrapper.find('button[data-template="nano"]').attributes('aria-pressed')).toBe('true')
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(bodies[0]).toMatchObject({ name: 'Nano', cores: 1, memory_mb: 512, disk_gb: 10 })
  })

  it('a template whose name exists is disabled', async () => {
    existing = [typeRow({ name: 'small', disk_gb: 25 })]
    const { wrapper } = mountApp(ServerTypeForm)
    await vi.waitFor(() =>
      expect(wrapper.find('button[data-template="small"]').attributes('disabled')).toBeDefined(),
    )
    expect(wrapper.find('button[data-template="small"]').text()).toContain('Already added')
  })

  it('edited template values are what gets submitted', async () => {
    const { wrapper } = mountApp(ServerTypeForm)
    await flushPromises()
    await wrapper.find('button[data-template="medium"]').trigger('click')
    await wrapper.find('input[name="cores"]').setValue('3')
    expect(wrapper.find('button[data-template="medium"]').attributes('aria-pressed')).toBe('false')
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(bodies[0]).toMatchObject({ name: 'Medium', cores: 3, memory_mb: 4096, disk_gb: 50 })
  })

  it('picking a template clears earlier field errors', async () => {
    const { wrapper } = mountApp(ServerTypeForm)
    await flushPromises()
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(wrapper.text()).toContain('Give the server type a name.')
    await wrapper.find('button[data-template="large"]').trigger('click')
    expect(wrapper.text()).not.toContain('Give the server type a name.')
  })

  it('no template grid when editing', async () => {
    const { wrapper } = mountApp(ServerTypeForm, { props: { item: typeRow({}) } })
    await flushPromises()
    expect(wrapper.find('[data-template]').exists()).toBe(false)
  })

  it('grid still works when existing types fail to load', async () => {
    server.use(
      http.get('*/v1/admin/server_types', () =>
        json(500, { error: { code: 'internal', message: 'boom' } }),
      ),
    )
    const { wrapper, queryClient } = mountApp(ServerTypeForm)
    await vi.waitFor(() =>
      expect(queryClient.getQueryState(['admin', 'server-types'])?.status).toBe('error'),
    )
    await flushPromises()
    expect(wrapper.find('button[data-template="small"]').exists()).toBe(true)
    expect(wrapper.find('button[data-template="small"]').attributes('disabled')).toBeUndefined()
  })

  it('picking a template clears an earlier server error', async () => {
    server.use(
      http.post('*/v1/admin/server_types', () =>
        json(409, { error: { code: 'conflict', message: 'A server type with that name exists.' } }),
      ),
    )
    const { wrapper } = mountApp(ServerTypeForm)
    await fill(wrapper)
    await wrapper.find('form').trigger('submit')
    await vi.waitFor(() => expect(wrapper.text()).toContain('A server type with that name exists.'))
    await wrapper.find('button[data-template="medium"]').trigger('click')
    expect(wrapper.text()).not.toContain('A server type with that name exists.')
  })
})

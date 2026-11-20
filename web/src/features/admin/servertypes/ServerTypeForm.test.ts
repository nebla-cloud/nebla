import { beforeEach, describe, expect, it } from 'vitest'
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

beforeEach(() => {
  document.body.innerHTML = ''
  bodies = []
  server.use(
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
})

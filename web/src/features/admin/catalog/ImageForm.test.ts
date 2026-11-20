import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { http } from 'msw'
import { json, server } from '@/test/server'
import { mountApp } from '@/test/mount'
import type { AdminImage } from './queries'
import ImageForm from './ImageForm.vue'

const node = (id: number, name: string) => ({
  id,
  name,
  status: 'online',
  enabled: true,
  disk_storage_override: '',
  max_cpu: 8,
  max_mem: 1,
  cpu: 0,
  mem: 0,
})
const locations = [
  { id: 1, name: 'home', description: '', nodes: [node(1, 'pve1'), node(2, 'pve2')] },
  { id: 2, name: 'colo', description: '', nodes: [node(3, 'edge1')] },
]
const catalogEntry = (key: string, flavor: string) => ({
  key,
  description: key,
  os_flavor: flavor,
  os_version: '1',
  retired: false,
  locations: [],
})

let posts: Record<string, unknown>[] = []
let puts: Record<string, unknown>[] = []
let failWith: { status: number; message: string } | null = null

beforeEach(() => {
  document.body.innerHTML = ''
  posts = []
  puts = []
  failWith = null
  server.use(
    http.get('*/v1/admin/locations', () => json(200, { locations, meta: {} })),
    http.get('*/v1/admin/catalog', () =>
      json(200, {
        catalog: [catalogEntry('debian-13', 'debian'), catalogEntry('ubuntu-24.04', 'ubuntu')],
      }),
    ),
    http.post('*/v1/admin/images', async ({ request }) => {
      if (failWith) {
        return json(failWith.status, { error: { code: 'invalid', message: failWith.message } })
      }
      posts.push((await request.json()) as Record<string, unknown>)
      return json(201, { id: 5 })
    }),
    http.put('*/v1/admin/images/:id', async ({ request }) => {
      puts.push((await request.json()) as Record<string, unknown>)
      return json(200, { id: 5 })
    }),
  )
})

async function open(item?: AdminImage) {
  const m = mountApp(ImageForm, { props: { item } })
  await vi.waitFor(() => {
    const texts = m.wrapper.findAll('option').map((o) => o.text())
    expect(texts).toContain('ubuntu')
    expect(texts).toContain('colo')
  })
  return m
}

type W = Awaited<ReturnType<typeof open>>['wrapper']

async function fillBasics(w: W) {
  await w.find('input[name="description"]').setValue('Windows Server 2025')
  await w.find('input[name="name"]').setValue('win-2025')
  await w.find('select[name="os_flavor"]').setValue('other')
  await w.find('input[name="os_flavor_other"]').setValue('windows')
  await w.find('input[name="os_version"]').setValue('2025')
}

async function fillRow(w: W, i: number, loc: string, nodeName: string, vmid: string) {
  await w.find(`select[name="location-${i}"]`).setValue(loc)
  await w.find(`select[name="node-${i}"]`).setValue(nodeName)
  await w.find(`input[name="vmid-${i}"]`).setValue(vmid)
}

const submit = async (w: W) => {
  await w.find('form').trigger('submit')
  await flushPromises()
}

describe('ImageForm', () => {
  it('add sends the image with its templates', async () => {
    const { wrapper } = await open()
    await fillBasics(wrapper)
    await fillRow(wrapper, 0, '1', 'pve2', '9100')
    await submit(wrapper)
    await vi.waitFor(() => expect(posts).toHaveLength(1))
    expect(posts[0]).toEqual({
      name: 'win-2025',
      description: 'Windows Server 2025',
      os_flavor: 'windows',
      os_version: '2025',
      templates: [{ location_id: 1, node: 'pve2', vmid: 9100 }],
    })
  })

  it('offers the catalog flavors and Other', async () => {
    const { wrapper } = await open()
    const opts = wrapper.findAll('select[name="os_flavor"] option').map((o) => o.text())
    expect(opts).toEqual(expect.arrayContaining(['debian', 'ubuntu', 'Other']))
  })

  it('a catalog flavor is sent as chosen', async () => {
    const { wrapper } = await open()
    await fillBasics(wrapper)
    await wrapper.find('select[name="os_flavor"]').setValue('debian')
    expect(wrapper.find('input[name="os_flavor_other"]').exists()).toBe(false)
    await fillRow(wrapper, 0, '2', 'edge1', '9200')
    await submit(wrapper)
    await vi.waitFor(() => expect(posts).toHaveLength(1))
    expect(posts[0]!.os_flavor).toBe('debian')
  })

  it('shared storage omits node', async () => {
    const { wrapper } = await open()
    await fillBasics(wrapper)
    await fillRow(wrapper, 0, '1', '', '9100')
    await submit(wrapper)
    await vi.waitFor(() => expect(posts).toHaveLength(1))
    expect(posts[0]!.templates).toEqual([{ location_id: 1, vmid: 9100 }])
  })

  it('node choices follow the location', async () => {
    const { wrapper } = await open()
    await wrapper.find('select[name="location-0"]').setValue('2')
    const opts = wrapper.findAll('select[name="node-0"] option').map((o) => o.text())
    expect(opts).toEqual(['Shared storage', 'edge1'])
  })

  it('blocks a second row for the same location and node', async () => {
    const { wrapper } = await open()
    await fillBasics(wrapper)
    await fillRow(wrapper, 0, '1', 'pve1', '9100')
    await wrapper.find('button[data-add-template]').trigger('click')
    await fillRow(wrapper, 1, '1', 'pve1', '9101')
    await submit(wrapper)
    expect(wrapper.text()).toContain('Each location and node can have only one template.')
    expect(posts).toHaveLength(0)
    await wrapper.find('select[name="node-1"]').setValue('pve2')
    await submit(wrapper)
    await vi.waitFor(() => expect(posts).toHaveLength(1))
    expect((posts[0]!.templates as unknown[]).length).toBe(2)
  })

  it('removes a template row', async () => {
    const { wrapper } = await open()
    expect(wrapper.find('button[aria-label="Remove template 1"]').exists()).toBe(false)
    await wrapper.find('button[data-add-template]').trigger('click')
    expect(wrapper.findAll('select[name^="location-"]')).toHaveLength(2)
    await wrapper.find('button[aria-label="Remove template 2"]').trigger('click')
    expect(wrapper.findAll('select[name^="location-"]')).toHaveLength(1)
  })

  it('edit prefills and sends PUT', async () => {
    const item: AdminImage = {
      id: 5,
      name: 'win-2025',
      description: 'Windows Server 2025',
      os_flavor: 'windows',
      os_version: '2025',
      catalog_key: null,
      templates: [{ location_id: 2, node: 'edge1', vmid: 9200 }],
      created: '2026-11-01T10:00:00Z',
    }
    const { wrapper } = await open(item)
    expect((wrapper.find('input[name="name"]').element as HTMLInputElement).value).toBe('win-2025')
    expect((wrapper.find('select[name="os_flavor"]').element as HTMLSelectElement).value).toBe(
      'other',
    )
    expect((wrapper.find('input[name="os_flavor_other"]').element as HTMLInputElement).value).toBe(
      'windows',
    )
    expect((wrapper.find('select[name="location-0"]').element as HTMLSelectElement).value).toBe('2')
    expect((wrapper.find('select[name="node-0"]').element as HTMLSelectElement).value).toBe('edge1')
    await wrapper.find('input[name="vmid-0"]').setValue('9300')
    await submit(wrapper)
    await vi.waitFor(() => expect(puts).toHaveLength(1))
    expect(puts[0]!.templates).toEqual([{ location_id: 2, node: 'edge1', vmid: 9300 }])
    expect(posts).toHaveLength(0)
  })

  it('shows the API 422 message inline', async () => {
    failWith = { status: 422, message: 'Name must use lowercase letters and dashes.' }
    const { wrapper } = await open()
    await fillBasics(wrapper)
    await fillRow(wrapper, 0, '1', 'pve1', '9100')
    await submit(wrapper)
    await vi.waitFor(() =>
      expect(wrapper.text()).toContain('Name must use lowercase letters and dashes.'),
    )
  })

  it('asks for the required fields', async () => {
    const { wrapper } = await open()
    await submit(wrapper)
    expect(wrapper.text()).toContain('Give the image a name.')
    expect(wrapper.text()).toContain('Choose a location.')
    expect(wrapper.text()).toContain('Enter the VMID as a whole number.')
    expect(posts).toHaveLength(0)
  })
})

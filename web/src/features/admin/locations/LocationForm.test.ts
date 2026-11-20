import { beforeEach, describe, expect, it } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { http } from 'msw'
import { json, server } from '@/test/server'
import { mountApp } from '@/test/mount'
import type { AdminLocation } from '@/api/types'
import LocationForm from './LocationForm.vue'

beforeEach(() => {
  document.body.innerHTML = ''
})

const existing: AdminLocation = {
  id: 1,
  name: 'home',
  description: '',
  pve: {
    url: 'https://pve.example.test:8006',
    token_id: 'nebla@pve!panel',
    has_secret: true,
    ca_fingerprint: '',
    insecure_tls: false,
  },
  disk_storage: 'local-lvm',
  iso_storage: 'local',
  import_storage: 'local',
  nodes: [],
  created: '2026-01-01T00:00:00Z',
  updated: '2026-01-01T00:00:00Z',
}

async function fillCreate(w: ReturnType<typeof mountApp>['wrapper'], name = 'home') {
  await w.find('input[name="name"]').setValue(name)
  await w.find('input[name="url"]').setValue('https://pve.example.test:8006')
  await w.find('input[name="token_id"]').setValue('nebla@pve!panel')
  await w.find('input[name="token_secret"]').setValue('s3cret')
  await w.find('input[name="disk_storage"]').setValue('local-lvm')
  await w.find('input[name="iso_storage"]').setValue('local')
  await w.find('input[name="import_storage"]').setValue('local')
}

describe('LocationForm', () => {
  it('create sends the location input', async () => {
    let body: unknown
    server.use(
      http.post('*/v1/admin/locations', async ({ request }) => {
        body = await request.json()
        return json(201, existing)
      }),
    )
    const { wrapper } = mountApp(LocationForm)
    await fillCreate(wrapper)
    await wrapper.find('button[role="checkbox"]').trigger('click')
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(body).toEqual({
      name: 'home',
      pve: {
        url: 'https://pve.example.test:8006',
        token_id: 'nebla@pve!panel',
        token_secret: 's3cret',
        insecure_tls: true,
      },
      disk_storage: 'local-lvm',
      iso_storage: 'local',
      import_storage: 'local',
    })
  })

  it('checks the name pattern locally', async () => {
    let called = false
    server.use(
      http.post('*/v1/admin/locations', () => {
        called = true
        return json(201, existing)
      }),
    )
    const { wrapper } = mountApp(LocationForm)
    await fillCreate(wrapper, 'Home 1')
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(called).toBe(false)
    expect(wrapper.text()).toContain('lowercase letters')
  })

  it('edit omits token_secret when left blank', async () => {
    let body: Record<string, unknown> | undefined
    server.use(
      http.put('*/v1/admin/locations/1', async ({ request }) => {
        body = (await request.json()) as Record<string, unknown>
        return json(200, existing)
      }),
    )
    const { wrapper } = mountApp(LocationForm, { props: { location: existing } })
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(body).toBeDefined()
    expect(body!.pve).not.toHaveProperty('token_secret')
    expect(body!.name).toBe('home')
  })

  it('shows the friendly message for pve_unreachable', async () => {
    server.use(
      http.post('*/v1/admin/locations', () =>
        json(422, { error: { code: 'pve_unreachable', message: 'dial tcp: timeout' } }),
      ),
    )
    const { wrapper } = mountApp(LocationForm)
    await fillCreate(wrapper)
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(wrapper.text()).toContain("Proxmox didn't respond")
  })
})

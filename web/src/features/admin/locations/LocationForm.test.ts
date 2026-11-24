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

import { beforeEach, describe, expect, it } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { http } from 'msw'
import { json, server } from '@/test/server'
import { mountApp } from '@/test/mount'
import type { AdminLocation } from '@/api/types'
import { catalogKey } from '../catalog/queries'
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
  firewall_enabled: null,
  volume_storage: '',
  volume_storage_ok: false,
  backup_storage: '',
  backup_storage_ok: false,
  backup_window: { start: '02:00', end: '06:00' },
  backups_on_other_storage: 0,
  sdn_zone: null,
  vnet_prefix: 'nb',
  private_networks: {
    state: 'not_set_up',
    zone: null,
    available: 0,
    in_use: 0,
    cooling: 0,
    unusable: [],
    degraded: [],
  },
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
  it('sends a volume storage only when one is set', async () => {
    let body: Record<string, unknown> = {}
    server.use(
      http.post('*/v1/admin/locations', async ({ request }) => {
        body = (await request.json()) as Record<string, unknown>
        return json(201, existing)
      }),
    )
    const { wrapper } = mountApp(LocationForm)
    await fillCreate(wrapper)
    await wrapper.find('input[name="volume_storage"]').setValue('ceph')
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(body.volume_storage).toBe('ceph')
  })

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
    expect(wrapper.text()).toContain("The server's host didn't respond")
  })

  it('marks the OS image catalog stale once a location is saved', async () => {
    server.use(http.post('*/v1/admin/locations', () => json(201, existing)))
    const { wrapper, queryClient } = mountApp(LocationForm)
    queryClient.setQueryData(catalogKey, [])
    await fillCreate(wrapper)
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(queryClient.getQueryState(catalogKey)?.isInvalidated).toBe(true)
  })

  describe('backups', () => {
    const select = (w: ReturnType<typeof mountApp>['wrapper']) =>
      w.find('input[name="backup_storage"]')

    it('is not on the create form', () => {
      const { wrapper } = mountApp(LocationForm)
      expect(select(wrapper).exists()).toBe(false)
    })

    it('suggests the reported PBS storages and the current one', async () => {
      const { wrapper } = mountApp(LocationForm, {
        props: {
          location: { ...existing, backup_storage: 'old' },
          backupStorages: ['pbs'],
        },
      })
      expect(wrapper.text()).toContain('Backup storage')
      expect(wrapper.text()).toContain('Backup window (UTC)')
      expect(
        wrapper.findAll('datalist#backup-storages option').map((o) => o.attributes('value')),
      ).toEqual(['pbs', 'old'])
      expect((select(wrapper).element as HTMLInputElement).value).toBe('old')
      expect(
        (wrapper.find('input[name="backup_window_start"]').element as HTMLInputElement).value,
      ).toBe('02:00')
      expect(
        (wrapper.find('input[name="backup_window_end"]').element as HTMLInputElement).value,
      ).toBe('06:00')
    })

    it('sends the storage and the window', async () => {
      let body: Record<string, unknown> | undefined
      server.use(
        http.put('*/v1/admin/locations/1', async ({ request }) => {
          body = (await request.json()) as Record<string, unknown>
          return json(200, existing)
        }),
      )
      const { wrapper } = mountApp(LocationForm, {
        props: { location: existing, backupStorages: ['pbs'] },
      })
      await select(wrapper).setValue('pbs')
      await wrapper.find('input[name="backup_window_start"]').setValue('01:30')
      await wrapper.find('input[name="backup_window_end"]').setValue('05:00')
      await wrapper.find('form').trigger('submit')
      await flushPromises()
      expect(body).toMatchObject({
        backup_storage: 'pbs',
        backup_window: { start: '01:30', end: '05:00' },
      })
    })

    it('shows the server message on the field', async () => {
      server.use(
        http.put('*/v1/admin/locations/1', () =>
          json(422, {
            error: {
              code: 'invalid_input',
              message: 'backup storage must be a Proxmox Backup Server storage with backup content',
            },
          }),
        ),
      )
      const { wrapper } = mountApp(LocationForm, { props: { location: existing } })
      await select(wrapper).setValue('nope')
      await wrapper.find('form').trigger('submit')
      await flushPromises()
      expect(wrapper.text()).toContain('backup storage must be a Proxmox Backup Server storage')
    })

    it('refuses a window that starts when it ends', async () => {
      let called = false
      server.use(
        http.put('*/v1/admin/locations/1', () => {
          called = true
          return json(200, existing)
        }),
      )
      const { wrapper } = mountApp(LocationForm, { props: { location: existing } })
      await wrapper.find('input[name="backup_window_end"]').setValue('02:00')
      await wrapper.find('form').trigger('submit')
      await flushPromises()
      expect(called).toBe(false)
      expect(wrapper.text()).toContain('The window must start and end at different times.')
    })
  })
})

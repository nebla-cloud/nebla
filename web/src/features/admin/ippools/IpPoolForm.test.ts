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
import type { IpPool } from './queries'
import IpPoolForm from './IpPoolForm.vue'
import IpPoolsPage from './IpPoolsPage.vue'

const locations = [{ id: 1, name: 'home', description: '', nodes: [] }]
let bodies: Record<string, unknown>[] = []

const pool: IpPool = {
  id: 5,
  location_id: 1,
  family: 'ipv4',
  bridge: 'vmbr0',
  cidr: '203.0.113.0/24',
  gateway: '203.0.113.1',
  dns: ['1.1.1.1'],
  range_start: '203.0.113.10',
  range_end: '203.0.113.200',
  allocated: 42,
  created: '2026-01-01T00:00:00Z',
}

beforeEach(() => {
  document.body.innerHTML = ''
  bodies = []
  server.use(
    http.get('*/v1/admin/locations', () => json(200, { locations, meta: {} })),
    http.get('*/v1/admin/ip_pools', () => json(200, { ip_pools: [pool], meta: {} })),
    http.post('*/v1/admin/ip_pools', async ({ request }) => {
      bodies.push((await request.json()) as Record<string, unknown>)
      return json(201, pool)
    }),
  )
})

async function fill(wrapper: ReturnType<typeof mountApp>['wrapper'], gateway = '203.0.113.1') {
  await flushPromises()
  await wrapper.find('select[name="location_id"]').setValue('1')
  await wrapper.find('select[name="family"]').setValue('ipv4')
  await wrapper.find('input[name="bridge"]').setValue('vmbr0')
  await wrapper.find('input[name="cidr"]').setValue('203.0.113.0/24')
  await wrapper.find('input[name="gateway"]').setValue(gateway)
  await wrapper.find('input[name="dns"]').setValue('1.1.1.1, 9.9.9.9')
  await wrapper.find('input[name="range_start"]').setValue('203.0.113.10')
  await wrapper.find('input[name="range_end"]').setValue('203.0.113.200')
}

describe('IpPoolForm', () => {
  it('ip pool form sends input', async () => {
    const { wrapper } = mountApp(IpPoolForm)
    await fill(wrapper)
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(bodies).toHaveLength(1)
    expect(bodies[0]).toEqual({
      location_id: 1,
      family: 'ipv4',
      bridge: 'vmbr0',
      cidr: '203.0.113.0/24',
      gateway: '203.0.113.1',
      dns: ['1.1.1.1', '9.9.9.9'],
      range_start: '203.0.113.10',
      range_end: '203.0.113.200',
    })
    expect('vlan_tag' in bodies[0]!).toBe(false)
  })

  it('ip pool rejects gateway outside CIDR locally', async () => {
    const { wrapper } = mountApp(IpPoolForm)
    await fill(wrapper, '198.51.100.1')
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(bodies).toHaveLength(0)
    expect(wrapper.text()).toContain('Gateway must be inside the CIDR')
  })

  it('ip pool shows allocated count', async () => {
    const { wrapper } = mountApp(IpPoolsPage)
    await flushPromises()
    await flushPromises()
    expect(wrapper.text()).toContain('42 of 191')
  })
})

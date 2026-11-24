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
import type { AdminLocation } from '@/api/types'
import { json, server } from '@/test/server'
import { mountApp } from '@/test/mount'
import PrivateNetworksCard from './PrivateNetworksCard.vue'

type Pool = AdminLocation['private_networks']

let unmount: () => void
let puts: Record<string, unknown>[]

const pool = (over: Partial<Pool> = {}): Pool => ({
  state: 'ready',
  zone: 'nebula',
  available: 5,
  in_use: 2,
  cooling: 1,
  unusable: [],
  degraded: [],
  ...over,
})

const loc = (over: Partial<AdminLocation> = {}): AdminLocation => ({
  id: 1,
  name: 'home',
  description: 'rack',
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
  firewall_enabled: true,
  volume_storage: '',
  volume_storage_ok: true,
  backup_storage: '',
  backup_storage_ok: false,
  backup_window: { start: '02:00', end: '06:00' },
  backups_on_other_storage: 0,
  sdn_zone: 'nebula',
  vnet_prefix: 'nb',
  private_networks: pool(),
  nodes: [],
  created: '2026-01-01T00:00:00Z',
  updated: '2026-01-01T00:00:00Z',
  ...over,
})

beforeEach(() => {
  puts = []
  server.use(
    http.put('*/v1/admin/locations/1', async ({ request }) => {
      puts.push((await request.json()) as Record<string, unknown>)
      return json(200, loc())
    }),
  )
})

afterEach(() => {
  unmount()
  document.body.innerHTML = ''
})

async function mountCard(location: AdminLocation) {
  const m = mountApp(PrivateNetworksCard, { props: { location } })
  unmount = () => m.wrapper.unmount()
  await vi.waitFor(() => expect(document.body.textContent).toContain('Private networks'))
  return m
}

const input = (name: string) =>
  document.body.querySelector<HTMLInputElement>(`input[name="${name}"]`)!
const text = () => document.body.textContent ?? ''

describe('PrivateNetworksCard', () => {
  it('shows the pool counts when ready', async () => {
    await mountCard(loc())
    expect(text()).toContain('5 available')
    expect(text()).toContain('2 in use')
    expect(text()).toContain('1 cooling')
    expect(input('sdn_zone').value).toBe('nebula')
    expect(input('vnet_prefix').value).toBe('nb')
    expect(text()).not.toContain('SDN.Audit')
    expect(text()).not.toContain('Isolation off')
  })

  it('explains that private networks are off when not set up', async () => {
    await mountCard(
      loc({ sdn_zone: null, private_networks: pool({ state: 'not_set_up', zone: null }) }),
    )
    expect(text()).toContain('Private networks are off at this location.')
    expect(input('sdn_zone').value).toBe('')
    expect(text()).not.toContain('available')
  })

  it('shows the pveum command when the token cannot read the zone', async () => {
    await mountCard(loc({ private_networks: pool({ state: 'needs_setup', available: 0 }) }))
    expect(text()).toContain('pveum role modify Nebla --append 1 --privs SDN.Audit')
  })

  it('warns when the datacenter firewall is off', async () => {
    await mountCard(loc({ private_networks: pool({ state: 'firewall_off' }) }))
    expect(text()).toContain('Isolation off: turn the datacenter firewall back on.')
  })

  it('lists unusable and degraded VNets', async () => {
    await mountCard(
      loc({
        private_networks: pool({
          unusable: [{ vnet: 'nb7', reason: 'has a subnet' }],
          degraded: [{ vnet: 'nb2', network: 'backend', reason: 'missing in Proxmox' }],
        }),
      }),
    )
    expect(text()).toContain('nb7')
    expect(text()).toContain('has a subnet')
    expect(text()).toContain('nb2')
    expect(text()).toContain('backend')
    expect(text()).toContain('missing in Proxmox')
  })

  it('saves the zone and prefix with the rest of the location unchanged', async () => {
    await mountCard(loc())
    input('sdn_zone').value = 'zone2'
    input('sdn_zone').dispatchEvent(new Event('input'))
    input('vnet_prefix').value = 'pv'
    input('vnet_prefix').dispatchEvent(new Event('input'))
    document.body.querySelector<HTMLFormElement>('#private-networks')!.requestSubmit()
    await vi.waitFor(() => expect(puts).toHaveLength(1))
    expect(puts[0]).toEqual({
      name: 'home',
      description: 'rack',
      pve: {
        url: 'https://pve.example.test:8006',
        token_id: 'nebla@pve!panel',
        ca_fingerprint: '',
        insecure_tls: false,
      },
      disk_storage: 'local-lvm',
      iso_storage: 'local',
      import_storage: 'local',
      sdn_zone: 'zone2',
      vnet_prefix: 'pv',
    })
  })

  it('rejects a bad prefix before sending', async () => {
    await mountCard(loc())
    input('vnet_prefix').value = 'Bad-Prefix'
    input('vnet_prefix').dispatchEvent(new Event('input'))
    document.body.querySelector<HTMLFormElement>('#private-networks')!.requestSubmit()
    await vi.waitFor(() => expect(text()).toContain('1 to 4 lowercase letters or digits'))
    expect(puts).toHaveLength(0)
  })

  it('shows the API error when the zone is in use', async () => {
    server.use(
      http.put('*/v1/admin/locations/1', () =>
        json(409, {
          error: {
            code: 'conflict',
            message:
              "This location's private networks are in use; the zone and prefix can't change.",
          },
        }),
      ),
    )
    await mountCard(loc())
    input('sdn_zone').value = 'zone2'
    input('sdn_zone').dispatchEvent(new Event('input'))
    document.body.querySelector<HTMLFormElement>('#private-networks')!.requestSubmit()
    await vi.waitFor(() => expect(text()).toContain("the zone and prefix can't change"))
  })
})

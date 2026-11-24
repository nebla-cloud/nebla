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

import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { http } from 'msw'
import type { Network } from '@/api/types'
import { makeServer } from '@/features/servers/fixtures'
import { makeLocation } from '@/features/volumes/fixtures'
import { json, server } from '@/test/server'
import { mountApp, testRouter } from '@/test/mount'
import NetworkingTab from './NetworkingTab.vue'
import { makeNetwork, makeNetworkGuestConfig } from './fixtures'
import { button, fill, networkAction, serveLists, settle } from './testkit'

let calls: { path: string; body: unknown }[]
let unmount: () => void

const dialog = () =>
  Array.from(document.body.querySelectorAll('[role="dialog"]')).at(-1) as HTMLElement | undefined

const nets: Network[] = [
  makeNetwork({ id: 1, name: 'backend' }),
  makeNetwork({ id: 2, name: 'db', ip_range: '192.168.5.0/24' }),
  makeNetwork({ id: 3, name: 'far-net', location: { id: 2, name: 'far', description: '' } }),
]

beforeEach(() => {
  calls = []
  serveLists({ networks: nets, locations: [makeLocation({ private_networks_available: true })] })
  server.use(
    http.post('*/v1/servers/:id/actions/:verb', async ({ request, params }) => {
      calls.push({
        path: `/servers/${String(params.id)}/actions/${String(params.verb)}`,
        body: await request.json(),
      })
      return json(201, {
        ...networkAction(String(params.verb), 5),
        guest_config: makeNetworkGuestConfig(),
      })
    }),
    http.get('*/v1/actions/:id', () =>
      json(200, networkAction('attach_to_network', 5, 'success').action),
    ),
  )
})

afterEach(() => {
  unmount()
  document.body.innerHTML = ''
})

async function mountTab(over: Parameters<typeof makeServer>[0] = {}) {
  const router = testRouter([
    { path: '/p/:projectId/networks/:networkId', name: 'network', component: NetworkingTab },
    { path: '/p/:projectId/servers/:serverId/networking', component: NetworkingTab },
  ])
  await router.push('/p/7/servers/5/networking')
  const m = mountApp(NetworkingTab, { router, props: { server: makeServer({ id: 5, ...over }) } })
  unmount = () => m.wrapper.unmount()
  await settle()
  return m
}

const joined = {
  private_net: [
    { network: 1, name: 'backend', ip: '10.0.0.2', guest_config: makeNetworkGuestConfig() },
  ],
}

describe('NetworkingTab', () => {
  it('shows the public address and the private addresses', async () => {
    await mountTab(joined)
    const text = document.body.textContent!
    expect(text).toContain('203.0.113.10')
    expect(text).toContain('backend')
    expect(text).toContain('10.0.0.2')
    expect(document.body.querySelector('a[href="/p/7/networks/1"]')).not.toBeNull()
    expect(text).not.toMatch(/vnet|vmid|bridge|\bnic\b|zone|sdn/i)
  })

  it('shows a prompt when the server has no private networks', async () => {
    await mountTab()
    expect(document.body.textContent).toContain('This server is not on a private network.')
    expect(() => button('Show commands')).toThrow()
  })

  it('reopens the commands from the stored guest config', async () => {
    await mountTab(joined)
    button('Show commands').click()
    await settle()
    expect(dialog()!.textContent).toContain('sudo ip addr add 10.0.0.2/16 dev eth1')
  })

  it('hides Show commands when there are none', async () => {
    await mountTab({ private_net: [{ ...joined.private_net[0]!, guest_config: null }] })
    expect(() => button('Show commands')).toThrow()
  })

  it('attaches to a network in its own location and opens the commands', async () => {
    await mountTab(joined)
    button('Attach to network').click()
    await settle()
    const options = Array.from(
      dialog()!.querySelectorAll<HTMLOptionElement>('select[name="network"] option'),
    ).map((o) => o.textContent!.trim())
    expect(options).toEqual([
      'Pick a network',
      'backend (10.0.0.0/16) (Already on this network)',
      'db (192.168.5.0/24)',
    ])
    await fill('select[name="network"]', '2')
    document.body.querySelector<HTMLFormElement>('#attach-network')!.requestSubmit()
    await settle()
    await settle()
    expect(calls).toEqual([
      { path: '/servers/5/actions/attach_to_network', body: { network: '2' } },
    ])
    expect(dialog()!.textContent).toContain('Run this on the server')
  })

  it('detaches after confirming', async () => {
    await mountTab(joined)
    button('Detach').click()
    await settle()
    Array.from(dialog()!.querySelectorAll('button'))
      .find((b) => b.textContent?.trim() === 'Detach')!
      .click()
    await settle()
    expect(calls).toEqual([
      { path: '/servers/5/actions/detach_from_network', body: { network: '1' } },
    ])
  })

  it('disables Attach at three networks', async () => {
    const one = joined.private_net[0]!
    await mountTab({ private_net: [one, { ...one, network: 2 }, { ...one, network: 3 }] })
    expect(button('Attach to network').disabled).toBe(true)
  })
})

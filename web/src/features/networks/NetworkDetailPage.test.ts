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
import { json, server } from '@/test/server'
import { mountApp, testRouter } from '@/test/mount'
import NetworkDetailPage from './NetworkDetailPage.vue'
import { makeNetwork, makeNetworkGuestConfig } from './fixtures'
import { button, fill, networkAction, serveLists, settle } from './testkit'

let net: Network
let calls: { path: string; body: unknown }[]
let deleted: boolean
let unmount: () => void

const dialog = () =>
  Array.from(document.body.querySelectorAll('[role="dialog"]')).at(-1) as HTMLElement | undefined

beforeEach(() => {
  net = makeNetwork()
  calls = []
  deleted = false
  serveLists({
    servers: [
      makeServer({ id: 5, name: 'web1' }),
      makeServer({ id: 6, name: 'far1', location: { id: 2, name: 'far', description: '' } }),
      makeServer({ id: 8, name: 'web2' }),
    ],
  })
  server.use(
    http.get('*/v1/networks/:id', () => json(200, net)),
    http.post('*/v1/servers/:id/actions/:verb', async ({ request, params }) => {
      calls.push({
        path: `/servers/${String(params.id)}/actions/${String(params.verb)}`,
        body: await request.json(),
      })
      return json(201, {
        ...networkAction(`${String(params.verb)}`, Number(params.id)),
        guest_config:
          params.verb === 'attach_to_network'
            ? makeNetworkGuestConfig()
            : makeNetworkGuestConfig({
                apply: null,
                persist: null,
                remove: 'sudo ip link set eth1 down',
              }),
      })
    }),
    http.delete('*/v1/networks/:id', () => {
      deleted = true
      return new Response(null, { status: 204 })
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

async function mountPage() {
  const router = testRouter([
    { path: '/p/:projectId/networks', name: 'networks', component: NetworkDetailPage },
    { path: '/p/:projectId/networks/:networkId', name: 'network', component: NetworkDetailPage },
    { path: '/p/:projectId/servers/:serverId', name: 'server', component: NetworkDetailPage },
  ])
  await router.push('/p/7/networks/1')
  const m = mountApp(NetworkDetailPage, { router })
  unmount = () => m.wrapper.unmount()
  await settle()
  return m
}

describe('NetworkDetailPage', () => {
  it('shows the range, location and an empty servers table', async () => {
    await mountPage()
    const text = document.body.textContent!
    expect(text).toContain('backend')
    expect(text).toContain('10.0.0.0/16')
    expect(text).toContain('home')
    expect(text).toContain('No servers on this network yet.')
    expect(button('Delete').disabled).toBe(false)
  })

  it('attaches a server from the same location and opens the commands', async () => {
    await mountPage()
    button('Attach server').click()
    await settle()
    const options = Array.from(
      dialog()!.querySelectorAll<HTMLOptionElement>('select[name="server"] option'),
    ).map((o) => o.textContent!.trim())
    expect(options).toEqual(['Pick a server', 'web1', 'web2'])
    await fill('select[name="server"]', '5')
    await fill('input[name="ip"]', '10.0.0.9')
    document.body.querySelector<HTMLFormElement>('#attach-network')!.requestSubmit()
    await settle()
    await settle()
    expect(calls).toEqual([
      { path: '/servers/5/actions/attach_to_network', body: { network: '1', ip: '10.0.0.9' } },
    ])
    expect(dialog()!.textContent).toContain('Run this on the server')
    expect(dialog()!.textContent).toContain('sudo ip addr add 10.0.0.2/16 dev eth1')
    expect(dialog()!.textContent).toContain('web1')
  })

  it('leaves the address out when it is empty', async () => {
    await mountPage()
    button('Attach server').click()
    await settle()
    await fill('select[name="server"]', '8')
    document.body.querySelector<HTMLFormElement>('#attach-network')!.requestSubmit()
    await settle()
    expect(calls).toEqual([
      { path: '/servers/8/actions/attach_to_network', body: { network: '1' } },
    ])
  })

  it('detaches an attached server and shows the remove command', async () => {
    net = makeNetwork({ servers: [{ server: 5, name: 'web1', ip: '10.0.0.2' }] })
    await mountPage()
    expect(document.body.textContent).toContain('10.0.0.2')
    button('Detach').click()
    await settle()
    const confirm = Array.from(dialog()!.querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === 'Detach',
    )!
    confirm.click()
    await settle()
    await settle()
    expect(calls).toEqual([
      { path: '/servers/5/actions/detach_from_network', body: { network: '1' } },
    ])
    expect(dialog()!.textContent).toContain('sudo ip link set eth1 down')
  })

  it('disables Delete with a hint while servers are attached', async () => {
    net = makeNetwork({ servers: [{ server: 5, name: 'web1', ip: '10.0.0.2' }] })
    await mountPage()
    expect(button('Delete').disabled).toBe(true)
    expect(document.body.querySelector('[title="Detach all servers first."]')).not.toBeNull()
  })

  it('deletes after typing the name', async () => {
    await mountPage()
    button('Delete').click()
    await settle()
    await fill('input', 'backend')
    button('Delete network').click()
    await settle()
    expect(deleted).toBe(true)
  })
})

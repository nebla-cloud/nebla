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
import type { PrimaryIp, Server } from '@/api/types'
import { makeServer } from '@/features/servers/fixtures'
import { json, server } from '@/test/server'
import { mountApp, testRouter } from '@/test/mount'
import PrimaryIPDetailPage from './PrimaryIPDetailPage.vue'
import { makePrimaryIP } from './fixtures'
import { action, button, fill, serveLists, settle } from './testkit'

const OLD =
  'This server was created before IP changes were supported. Changing its IP re-runs its cloud-init user data and gives it new SSH host keys.'

let pip: PrimaryIp
let assignedServer: Server
let calls: { path: string; body: unknown }[]
let warnings: string[]
let unmount: () => void

const dialog = () =>
  Array.from(document.body.querySelectorAll('[role="dialog"]')).at(-1) as HTMLElement | undefined

beforeEach(() => {
  pip = makePrimaryIP()
  assignedServer = makeServer({ id: 5, name: 'web1', status: 'off' })
  calls = []
  warnings = []
  serveLists({
    servers: [
      makeServer({
        id: 5,
        name: 'web1',
        status: 'off',
        public_net: { ipv4: null, ipv6: null, floating_ips: [] },
      }),
      makeServer({
        id: 6,
        name: 'far1',
        status: 'off',
        location: { id: 2, name: 'far', description: '' },
      }),
      makeServer({ id: 8, name: 'busy1', status: 'running' }),
      makeServer({ id: 9, name: 'full1', status: 'off' }),
    ],
  })
  server.use(
    http.get('*/v1/primary_ips/:id', () => json(200, pip)),
    http.get('*/v1/servers/:id', () => json(200, assignedServer)),
    http.put('*/v1/primary_ips/:id', async ({ request, params }) => {
      calls.push({ path: `PUT /primary_ips/${params.id}`, body: await request.json() })
      return json(200, pip)
    }),
    http.post('*/v1/primary_ips/:id/actions/assign', async ({ request, params }) => {
      calls.push({ path: `/primary_ips/${params.id}/actions/assign`, body: await request.json() })
      return json(201, { action: action('assign_primary_ip'), warnings })
    }),
    http.post('*/v1/primary_ips/:id/actions/unassign', ({ params }) => {
      calls.push({ path: `/primary_ips/${params.id}/actions/unassign`, body: null })
      return json(201, { action: action('unassign_primary_ip'), warnings })
    }),
    http.delete('*/v1/primary_ips/:id', ({ params }) => {
      calls.push({ path: `DELETE /primary_ips/${params.id}`, body: null })
      return new Response(null, { status: 204 })
    }),
    http.get('*/v1/actions/:id', () => json(200, action('assign_primary_ip', 'running'))),
  )
})

afterEach(() => {
  unmount()
  document.body.innerHTML = ''
})

async function mountPage() {
  const router = testRouter([
    { path: '/p/:projectId/primary-ips', name: 'primary-ips', component: PrimaryIPDetailPage },
    {
      path: '/p/:projectId/primary-ips/:primaryIpId',
      name: 'primary-ip',
      component: PrimaryIPDetailPage,
    },
    { path: '/p/:projectId/servers/:serverId', name: 'server', component: PrimaryIPDetailPage },
  ])
  await router.push('/p/7/primary-ips/1')
  const m = mountApp(PrimaryIPDetailPage, { router })
  unmount = () => m.wrapper.unmount()
  await settle()
  return m
}

describe('PrimaryIPDetailPage', () => {
  it('shows an unassigned IP with Assign and Delete enabled', async () => {
    await mountPage()
    expect(document.body.textContent).toContain('203.0.113.60')
    expect(document.body.textContent).toContain('Not assigned')
    expect(button('Assign').disabled).toBe(false)
    expect(button('Delete').disabled).toBe(false)
    expect(() => button('Unassign')).toThrow()
  })

  it('lists only stopped servers of the location without an IP of that family', async () => {
    await mountPage()
    button('Assign').click()
    await settle()
    const options = Array.from(
      dialog()!.querySelectorAll<HTMLOptionElement>('select[name="server"] option'),
    ).map((o) => [o.textContent!.trim(), o.disabled])
    expect(options).toEqual([
      ['Pick a server', true],
      ['web1', false],
      ['busy1 (Stop the server first.)', true],
      ['full1 (Already has a primary IPv4)', true],
    ])
    expect(dialog()!.textContent).toContain('The new address takes effect when the server starts.')
  })

  it('shows the old-server warning when assigning', async () => {
    warnings = [OLD]
    await mountPage()
    button('Assign').click()
    await settle()
    await fill('select[name="server"]', '5')
    document.body.querySelector<HTMLFormElement>('#assign-primary-ip')!.requestSubmit()
    await settle()
    expect(calls).toEqual([{ path: '/primary_ips/1/actions/assign', body: { server: '5' } }])
    expect(dialog()!.textContent).toContain(OLD)
  })

  it('warns about an old server before the assign is sent', async () => {
    serveLists({
      servers: [
        makeServer({
          id: 5,
          name: 'web1',
          status: 'off',
          ip_change_reruns_cloud_init: true,
          public_net: { ipv4: null, ipv6: null, floating_ips: [] },
        }),
      ],
    })
    await mountPage()
    button('Assign').click()
    await settle()
    await fill('select[name="server"]', '5')
    expect(dialog()!.textContent).toContain(OLD)
    expect(calls).toEqual([])
  })

  it('shows the old-server warning in the unassign confirm', async () => {
    pip = makePrimaryIP({ server: { id: 5, name: 'web1' } })
    assignedServer = makeServer({
      id: 5,
      name: 'web1',
      status: 'off',
      ip_change_reruns_cloud_init: true,
    })
    await mountPage()
    button('Unassign').click()
    await settle()
    expect(dialog()!.textContent).toContain(OLD)
    expect(calls).toEqual([])
  })

  it('disables Unassign and Delete with the reason while the server runs', async () => {
    pip = makePrimaryIP({ server: { id: 5, name: 'web1' } })
    assignedServer = makeServer({ id: 5, name: 'web1', status: 'running' })
    await mountPage()
    expect(button('Unassign').disabled).toBe(true)
    expect(button('Delete').disabled).toBe(true)
    expect(document.body.textContent).toContain('Stop the server first.')
    expect(() => button('Assign')).toThrow()
  })

  it('unassigns from a stopped server after confirming, warning of no public IPv4', async () => {
    pip = makePrimaryIP({ server: { id: 5, name: 'web1' } })
    warnings = [OLD]
    await mountPage()
    expect(button('Unassign').disabled).toBe(false)
    expect(button('Delete').disabled).toBe(true)
    button('Unassign').click()
    await settle()
    expect(dialog()!.textContent).toContain('This server will have no public IPv4.')
    const confirm = Array.from(dialog()!.querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === 'Unassign',
    ) as HTMLButtonElement
    confirm.click()
    await settle()
    expect(calls.map((c) => c.path)).toEqual(['/primary_ips/1/actions/unassign'])
    expect(document.body.textContent).toContain(OLD)
  })

  it('toggles auto-delete', async () => {
    await mountPage()
    document.body.querySelector<HTMLButtonElement>('button[role="switch"]')!.click()
    await settle()
    expect(calls).toEqual([{ path: 'PUT /primary_ips/1', body: { auto_delete: false } }])
  })

  it('deletes by name', async () => {
    await mountPage()
    button('Delete').click()
    await settle()
    expect(button('Delete primary IP').disabled).toBe(true)
    await fill('input[name="confirm_name"]', 'web-ip')
    button('Delete primary IP').click()
    await settle()
    expect(calls.map((c) => c.path)).toEqual(['DELETE /primary_ips/1'])
  })
})

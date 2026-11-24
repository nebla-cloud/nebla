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

import { afterEach, describe, expect, it } from 'vitest'
import { http } from 'msw'
import type { PrimaryIp } from '@/api/types'
import { makeServer } from '@/features/servers/fixtures'
import { json, server } from '@/test/server'
import { mountApp, testRouter } from '@/test/mount'
import ServerPrimaryIPs from './ServerPrimaryIPs.vue'
import { OLD_SERVER } from './queries'
import { makePrimaryIP } from './fixtures'
import { action, fill, settle } from './testkit'

let unmount: () => void
let posts: { path: string; body: unknown }[]

afterEach(() => {
  unmount()
  document.body.innerHTML = ''
})

const dialog = () =>
  Array.from(document.body.querySelectorAll('[role="dialog"]')).at(-1) as HTMLElement

async function mountPanel(
  over: Parameters<typeof makeServer>[0],
  ips: PrimaryIp[],
  warnings: string[] = [],
) {
  posts = []
  server.use(
    http.get('*/v1/projects/:id', () => json(200, { id: 7, name: 'Home', role: 'owner' })),
    http.get('*/v1/primary_ips', () =>
      json(200, {
        primary_ips: ips,
        meta: { pagination: { page: 1, per_page: 50, total_entries: ips.length } },
      }),
    ),
    http.post('*/v1/primary_ips/:id/actions/:verb', async ({ request, params }) => {
      posts.push({
        path: `/primary_ips/${params.id}/actions/${params.verb}`,
        body: request.headers.get('content-length') === '0' ? null : await request.json(),
      })
      return json(201, { action: action(`${params.verb}_primary_ip`), warnings })
    }),
  )
  const router = testRouter([
    {
      path: '/p/:projectId/primary-ips/:primaryIpId',
      name: 'primary-ip',
      component: ServerPrimaryIPs,
    },
  ])
  await router.push('/p/7/primary-ips/1')
  const m = mountApp(ServerPrimaryIPs, {
    router,
    props: { server: makeServer({ id: 5, status: 'off', ...over }), busy: false },
  })
  unmount = () => m.wrapper.unmount()
  await settle()
  return m
}

const noV6 = { public_net: { ipv4: { id: 1, ip: '203.0.113.10' }, ipv6: null, floating_ips: [] } }

describe('ServerPrimaryIPs', () => {
  it('assigns an unassigned IP of the family and shows the action warnings', async () => {
    const ips = [
      makePrimaryIP({ id: 1, name: 'v4', ip: '203.0.113.10', server: { id: 5, name: 'web1' } }),
      makePrimaryIP({ id: 4, name: 'spare-v6', ip: '2001:db8::4', type: 'ipv6' }),
      makePrimaryIP({ id: 5, name: 'spare-v4', ip: '203.0.113.70' }),
    ]
    const { wrapper } = await mountPanel(noV6, ips, [OLD_SERVER])
    await wrapper.find('button[aria-label="Assign IPv6"]').trigger('click')
    await settle()
    const options = Array.from(
      dialog().querySelectorAll<HTMLOptionElement>('select[name="primary_ip"] option'),
    ).map((o) => o.textContent!.trim())
    expect(options).toEqual(['Pick an IP', 'spare-v6 (2001:db8::4)'])
    await fill('select[name="primary_ip"]', '4')
    document.body.querySelector<HTMLFormElement>('#assign-server-primary-ip')!.requestSubmit()
    await settle()
    expect(posts).toEqual([{ path: '/primary_ips/4/actions/assign', body: { server: '5' } }])
    expect(wrapper.text()).toContain(OLD_SERVER)
  })

  it('warns about an old server before the assign is sent', async () => {
    const ips = [makePrimaryIP({ id: 4, name: 'spare-v6', ip: '2001:db8::4', type: 'ipv6' })]
    const { wrapper } = await mountPanel({ ...noV6, ip_change_reruns_cloud_init: true }, ips)
    await wrapper.find('button[aria-label="Assign IPv6"]').trigger('click')
    await settle()
    expect(dialog().textContent).toContain(OLD_SERVER)
    expect(posts).toEqual([])
  })

  it('confirms an unassign with the no-public-IP copy, and the old-server warning when it applies', async () => {
    const ips = [
      makePrimaryIP({ id: 1, name: 'v4', ip: '203.0.113.10', server: { id: 5, name: 'web1' } }),
    ]
    const { wrapper } = await mountPanel({ ip_change_reruns_cloud_init: true }, ips)
    await wrapper.find('button[aria-label="Unassign IPv4"]').trigger('click')
    await settle()
    expect(dialog().textContent).toContain(
      `This server will have no public IPv4. The new address takes effect when the server starts. ${OLD_SERVER}`,
    )
    expect(posts).toEqual([])
    const confirm = Array.from(dialog().querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === 'Unassign',
    )!
    confirm.click()
    await settle()
    expect(posts).toEqual([{ path: '/primary_ips/1/actions/unassign', body: null }])
  })

  it('leaves the old-server warning out for a new server', async () => {
    const ips = [
      makePrimaryIP({ id: 1, name: 'v4', ip: '203.0.113.10', server: { id: 5, name: 'web1' } }),
    ]
    const { wrapper } = await mountPanel({}, ips)
    await wrapper.find('button[aria-label="Unassign IPv4"]').trigger('click')
    await settle()
    expect(dialog().textContent).not.toContain(OLD_SERVER)
  })
})

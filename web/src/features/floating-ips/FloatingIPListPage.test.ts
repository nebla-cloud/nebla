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
import { makeLocation } from '@/features/volumes/fixtures'
import { makeServer } from '@/features/servers/fixtures'
import { json, server } from '@/test/server'
import { mountApp, testRouter } from '@/test/mount'
import FloatingIPListPage from './FloatingIPListPage.vue'
import { makeFloatingIP } from './fixtures'
import { button, fill, serveLists, settle } from './testkit'

let unmount: () => void

afterEach(() => {
  unmount()
  document.body.innerHTML = ''
})

async function mountPage() {
  const router = testRouter([
    { path: '/p/:projectId/floating-ips', name: 'floating-ips', component: FloatingIPListPage },
    {
      path: '/p/:projectId/floating-ips/:floatingIpId',
      name: 'floating-ip',
      component: FloatingIPListPage,
    },
    { path: '/p/:projectId/servers/:serverId', name: 'server', component: FloatingIPListPage },
  ])
  await router.push('/p/7/floating-ips')
  const m = mountApp(FloatingIPListPage, { router })
  unmount = () => m.wrapper.unmount()
  await settle()
  return m
}

describe('FloatingIPListPage', () => {
  it('shows an empty state with a create button', async () => {
    serveLists({ locations: [makeLocation()], floatingIps: [] })
    await mountPage()
    expect(document.body.textContent).toContain('No floating IPs yet')
    expect(button('Create floating IP')).toBeTruthy()
  })

  it('lists name, address, location, server and status', async () => {
    serveLists({
      locations: [makeLocation()],
      servers: [makeServer({ id: 5, name: 'web1' })],
      floatingIps: [
        makeFloatingIP({ id: 1, name: 'web-vip', ip: '203.0.113.50', server: 5 }),
        makeFloatingIP({
          id: 2,
          name: 'spare',
          ip: '2001:db8::5',
          type: 'ipv6',
          status: 'releasing',
        }),
      ],
    })
    const { wrapper } = await mountPage()
    const rows = wrapper.findAll('li')
    expect(rows).toHaveLength(2)
    expect(rows[0]!.text()).toContain('web-vip')
    expect(rows[0]!.text()).toContain('203.0.113.50')
    expect(rows[0]!.text()).toContain('home')
    expect(rows[0]!.text()).toContain('web1')
    expect(rows[0]!.find('[role="img"]').attributes('aria-label')).toBe('Active')
    expect(rows[0]!.find('a').attributes('href')).toBe('/p/7/floating-ips/1')
    expect(rows[1]!.text()).toContain('2001:db8::5')
    expect(rows[1]!.text()).toContain('Not assigned')
    expect(rows[1]!.find('[role="img"]').attributes('aria-label')).toBe('Releasing')
  })

  it('creates a floating IP in a location', async () => {
    serveLists({ locations: [makeLocation()], floatingIps: [] })
    let body: unknown = null
    server.use(
      http.post('*/v1/floating_ips', async ({ request }) => {
        body = await request.json()
        return json(201, { floating_ip: makeFloatingIP({ name: 'vip' }), action: null })
      }),
    )
    const { router } = await mountPage()
    button('Create floating IP').click()
    await settle()
    await fill('input[name="name"]', 'vip')
    document.body.querySelector<HTMLFormElement>('#create-floating-ip')!.requestSubmit()
    await settle()
    expect(body).toEqual({ name: 'vip', type: 'ipv4', location: '1' })
    expect(document.body.textContent).toContain('Created vip')
    expect(router.currentRoute.value.path).toBe('/p/7/floating-ips/1')
  })
})

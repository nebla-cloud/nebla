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
import { json, server } from '@/test/server'
import { mountApp, testRouter } from '@/test/mount'
import PrimaryIPListPage from './PrimaryIPListPage.vue'
import { makePrimaryIP } from './fixtures'
import { button, fill, serveLists, settle } from './testkit'

let unmount: () => void

afterEach(() => {
  unmount()
  document.body.innerHTML = ''
})

async function mountPage() {
  const router = testRouter([
    { path: '/p/:projectId/primary-ips', name: 'primary-ips', component: PrimaryIPListPage },
    {
      path: '/p/:projectId/primary-ips/:primaryIpId',
      name: 'primary-ip',
      component: PrimaryIPListPage,
    },
  ])
  await router.push('/p/7/primary-ips')
  const m = mountApp(PrimaryIPListPage, { router })
  unmount = () => m.wrapper.unmount()
  await settle()
  return m
}

describe('PrimaryIPListPage', () => {
  it('shows an empty state with a create button', async () => {
    serveLists({ locations: [makeLocation()], primaryIps: [] })
    await mountPage()
    expect(document.body.textContent).toContain('No primary IPs yet')
    expect(button('Create primary IP')).toBeTruthy()
  })

  it('lists name, address, type, location, server and auto-delete', async () => {
    serveLists({
      locations: [makeLocation()],
      primaryIps: [
        makePrimaryIP({ id: 1, name: 'web-ip', server: { id: 5, name: 'web1' } }),
        makePrimaryIP({
          id: 2,
          name: 'spare',
          ip: '2001:db8::5',
          type: 'ipv6',
          auto_delete: false,
          status: 'releasing',
        }),
      ],
    })
    const { wrapper } = await mountPage()
    const rows = wrapper.findAll('li')
    expect(rows).toHaveLength(2)
    expect(rows[0]!.text()).toContain('web-ip')
    expect(rows[0]!.text()).toContain('203.0.113.60')
    expect(rows[0]!.text()).toContain('IPv4')
    expect(rows[0]!.text()).toContain('home')
    expect(rows[0]!.text()).toContain('web1')
    expect(rows[0]!.text()).toContain('Deleted with server')
    expect(rows[0]!.find('a').attributes('href')).toBe('/p/7/primary-ips/1')
    expect(rows[1]!.text()).toContain('2001:db8::5')
    expect(rows[1]!.text()).toContain('IPv6')
    expect(rows[1]!.text()).toContain('Not assigned')
    expect(rows[1]!.text()).toContain('Kept')
    expect(rows[1]!.find('[role="img"]').attributes('aria-label')).toBe('Releasing')
  })

  it('reserves an IP that survives its server', async () => {
    serveLists({ locations: [makeLocation()], primaryIps: [] })
    let body: unknown = null
    server.use(
      http.post('*/v1/primary_ips', async ({ request }) => {
        body = await request.json()
        return json(201, { primary_ip: makePrimaryIP({ name: 'keeper' }) })
      }),
    )
    const { router } = await mountPage()
    button('Create primary IP').click()
    await settle()
    await fill('input[name="name"]', 'keeper')
    const keep = document.body.querySelector<HTMLInputElement>('input[name="keep"]')!
    keep.click()
    await settle()
    document.body.querySelector<HTMLFormElement>('#create-primary-ip')!.requestSubmit()
    await settle()
    expect(body).toEqual({ name: 'keeper', type: 'ipv4', location: '1', auto_delete: false })
    expect(document.body.textContent).toContain('Created keeper')
    expect(router.currentRoute.value.path).toBe('/p/7/primary-ips/1')
  })
})

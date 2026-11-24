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
import { json, server } from '@/test/server'
import { mountApp, testRouter } from '@/test/mount'
import { makeLocation } from '@/features/volumes/fixtures'
import NetworkListPage from './NetworkListPage.vue'
import { makeNetwork } from './fixtures'
import { button, fill, serveLists, settle } from './testkit'

let unmount: () => void
let posted: unknown[]

beforeEach(() => {
  posted = []
  server.use(
    http.post('*/v1/networks', async ({ request }) => {
      posted.push(await request.json())
      return json(201, { network: makeNetwork({ id: 9, name: 'db' }) })
    }),
  )
})

afterEach(() => {
  unmount()
  document.body.innerHTML = ''
})

async function mountPage() {
  const router = testRouter([
    { path: '/p/:projectId/networks', name: 'networks', component: NetworkListPage },
    { path: '/p/:projectId/networks/:networkId', name: 'network', component: NetworkListPage },
  ])
  await router.push('/p/7/networks')
  const m = mountApp(NetworkListPage, { router })
  unmount = () => m.wrapper.unmount()
  await settle()
  return m
}

const dialog = () => document.body.querySelector('[role="dialog"]') as HTMLElement

describe('NetworkListPage', () => {
  it('lists name, location, range and server count', async () => {
    serveLists({
      networks: [
        makeNetwork({
          servers: [
            { server: 5, name: 'web1', ip: '10.0.0.2' },
            { server: 6, name: 'web2', ip: '10.0.0.3' },
          ],
        }),
        makeNetwork({ id: 2, name: 'empty', ip_range: '192.168.5.0/24' }),
      ],
    })
    await mountPage()
    const text = document.body.textContent!
    expect(text).toContain('backend')
    expect(text).toContain('10.0.0.0/16')
    expect(text).toContain('home')
    expect(text).toContain('2 servers')
    expect(text).toContain('192.168.5.0/24')
    expect(text).toContain('0 servers')
  })

  it('shows the empty state with Create', async () => {
    serveLists({ locations: [makeLocation({ private_networks_available: true })] })
    await mountPage()
    expect(document.body.textContent).toContain('No networks yet')
    button('Create network').click()
    await settle()
    expect(dialog().textContent).toContain('Create network')
  })

  it('creates with a name and an offered location, leaving the range out', async () => {
    serveLists({
      locations: [
        makeLocation({ id: 1, name: 'plain', private_networks_available: false }),
        makeLocation({ id: 2, name: 'rack', private_networks_available: true }),
      ],
    })
    await mountPage()
    button('Create network').click()
    await settle()
    const options = Array.from(dialog().querySelectorAll('select[name="location"] option')).map(
      (o) => o.textContent!.trim(),
    )
    expect(options).toEqual(['rack'])
    expect(dialog().querySelector<HTMLInputElement>('input[name="ip_range"]')!.placeholder).toBe(
      '10.0.0.0/16 (automatic)',
    )
    await fill('input[name="name"]', 'backend')
    document.body.querySelector<HTMLFormElement>('#create-network')!.requestSubmit()
    await settle()
    expect(posted).toEqual([{ name: 'backend', location: '2' }])
  })

  it('sends the range when given and shows the API error', async () => {
    serveLists({ locations: [makeLocation({ private_networks_available: true })] })
    server.use(
      http.post('*/v1/networks', async ({ request }) => {
        posted.push(await request.json())
        return json(422, {
          error: { code: 'invalid_input', message: 'That range overlaps another network.' },
        })
      }),
    )
    await mountPage()
    button('Create network').click()
    await settle()
    await fill('input[name="name"]', 'db')
    await fill('input[name="ip_range"]', '192.168.7.0/24')
    document.body.querySelector<HTMLFormElement>('#create-network')!.requestSubmit()
    await settle()
    expect(posted).toEqual([{ name: 'db', location: '1', ip_range: '192.168.7.0/24' }])
    expect(dialog().textContent).toContain('That range overlaps another network.')
  })
})

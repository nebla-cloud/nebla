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
import { flushPromises } from '@vue/test-utils'
import { http } from 'msw'
import { json, server } from '@/test/server'
import { mountApp, testRouter } from '@/test/mount'
import type { Role } from '@/api/types'
import { makeFirewall, listMeta } from './fixtures'
import type { Firewall } from './queries'
import FirewallsPage from './FirewallsPage.vue'

let firewalls: Firewall[]
let posted: unknown[]

function serve(role: Role = 'owner') {
  server.use(
    http.get('*/v1/projects/7', () => json(200, { id: 7, name: 'Home', role })),
    http.get('*/v1/firewalls', () => json(200, { firewalls, meta: listMeta(firewalls.length) })),
    http.post('*/v1/firewalls', async ({ request }) => {
      const b = (await request.json()) as { name: string; labels: Record<string, string> }
      posted.push(b)
      const f = makeFirewall({ id: 9, name: b.name, labels: b.labels })
      firewalls = [...firewalls, f]
      return json(201, { firewall: f, actions: [] })
    }),
    http.get('*/v1/firewalls/9', () => json(200, firewalls.find((f) => f.id === 9))),
  )
}

async function settle() {
  for (let i = 0; i < 5; i++) {
    await new Promise((r) => setTimeout(r, 10))
    await flushPromises()
  }
}

async function mountPage() {
  const router = testRouter([
    { path: '/p/:projectId/firewalls', name: 'firewalls', component: FirewallsPage },
    { path: '/p/:projectId/firewalls/:firewallId', name: 'firewall', component: FirewallsPage },
  ])
  await router.push('/p/7/firewalls')
  const m = mountApp(FirewallsPage, { router })
  await settle()
  return m
}

const text = () => document.body.textContent ?? ''
const button = (t: string) =>
  Array.from(document.body.querySelectorAll('button')).find((b) => b.textContent?.trim() === t)

async function fill(sel: string, value: string) {
  const el = document.body.querySelector(sel) as HTMLInputElement
  el.value = value
  el.dispatchEvent(new Event('input'))
  await flushPromises()
}

beforeEach(() => {
  posted = []
  firewalls = [
    makeFirewall({
      id: 3,
      name: 'web',
      rules: [
        { direction: 'in', protocol: 'tcp', port: '22', source_ips: ['0.0.0.0/0'] },
        { direction: 'in', protocol: 'tcp', port: '443', source_ips: ['0.0.0.0/0'] },
        { direction: 'out', protocol: 'udp', port: '53', destination_ips: ['0.0.0.0/0'] },
      ],
      applied_to: [{ type: 'label_selector', label_selector: { selector: 'role=web' } }],
    }),
  ]
})

let unmount: (() => void) | null = null
afterEach(() => {
  unmount?.()
  unmount = null
  document.body.innerHTML = ''
})

describe('FirewallsPage', () => {
  it('lists firewalls with rule counts', async () => {
    serve()
    const { wrapper } = await mountPage()
    unmount = () => wrapper.unmount()
    expect(text()).toContain('web')
    expect(text()).toContain('2 inbound')
    expect(text()).toContain('1 outbound')
    expect(document.body.querySelector('a[href="/p/7/firewalls/3"]')).not.toBeNull()
  })

  it('counts the distinct servers a firewall is applied to', async () => {
    firewalls[0].applied_to = [
      {
        type: 'label_selector',
        label_selector: { selector: 'role=web' },
        applied_to_resources: [
          { type: 'server', server: { id: 1 } },
          { type: 'server', server: { id: 2 } },
        ],
      },
      { type: 'server', server: { id: 2 } },
      { type: 'server', server: { id: 5 } },
    ]
    serve()
    const { wrapper } = await mountPage()
    unmount = () => wrapper.unmount()
    expect(text()).toContain('Applied to 3 servers')
    expect(text()).not.toContain('target')
  })

  it('shows an empty state', async () => {
    firewalls = []
    serve()
    const { wrapper } = await mountPage()
    unmount = () => wrapper.unmount()
    expect(text()).toContain('No firewalls yet')
  })

  it('creating posts the name and labels', async () => {
    serve()
    const { wrapper, router } = await mountPage()
    unmount = () => wrapper.unmount()
    button('Create firewall')!.click()
    await flushPromises()
    await fill('input[name="name"]', 'db')
    await fill('input[name="labels"]', 'tier=db, env=prod')
    document.body.querySelector<HTMLFormElement>('#create-firewall')!.requestSubmit()
    await settle()
    expect(posted).toEqual([{ name: 'db', labels: { tier: 'db', env: 'prod' } }])
    await vi.waitFor(() => expect(router.currentRoute.value.path).toBe('/p/7/firewalls/9'))
  })

  it('rejects a bad label before sending', async () => {
    serve()
    const { wrapper } = await mountPage()
    unmount = () => wrapper.unmount()
    button('Create firewall')!.click()
    await flushPromises()
    await fill('input[name="name"]', 'db')
    await fill('input[name="labels"]', '-bad=x')
    document.body.querySelector<HTMLFormElement>('#create-firewall')!.requestSubmit()
    await settle()
    expect(posted).toEqual([])
    expect(text()).toContain('Keys use letters')
  })

  it('a read-only role sees no create button', async () => {
    serve('read_only')
    const { wrapper } = await mountPage()
    unmount = () => wrapper.unmount()
    expect(text()).toContain('web')
    expect(button('Create firewall')).toBeUndefined()
  })
})

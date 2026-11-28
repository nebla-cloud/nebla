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
import type { FloatingIp } from '@/api/types'
import { makeServer } from '@/features/servers/fixtures'
import { json, server } from '@/test/server'
import { mountApp, testRouter } from '@/test/mount'
import FloatingIPDetailPage from './FloatingIPDetailPage.vue'
import { makeFloatingIP, makeGuestConfig } from './fixtures'
import { action, button, fill, serveLists, settle } from './testkit'
import UTooltip from '@nuxt/ui/components/Tooltip.vue'

let fip: FloatingIp
let calls: { path: string; body: unknown }[]
let actionStatus: string
let unmount: () => void

const dialog = () =>
  Array.from(document.body.querySelectorAll('[role="dialog"]')).at(-1) as HTMLElement | undefined

beforeEach(() => {
  fip = makeFloatingIP()
  calls = []
  actionStatus = 'success'
  serveLists({
    servers: [
      makeServer({ id: 5, name: 'web1' }),
      makeServer({ id: 6, name: 'far1', location: { id: 2, name: 'far', description: '' } }),
      makeServer({ id: 8, name: 'web2' }),
    ],
  })
  server.use(
    http.get('*/v1/floating_ips/:id', () => json(200, fip)),
    http.post('*/v1/floating_ips/:id/actions/assign', async ({ request, params }) => {
      calls.push({ path: `/floating_ips/${params.id}/actions/assign`, body: await request.json() })
      fip = makeFloatingIP({ server: 5, guest_config: makeGuestConfig() })
      return json(201, { action: action('assign_floating_ip') })
    }),
    http.post('*/v1/floating_ips/:id/actions/unassign', ({ params }) => {
      calls.push({ path: `/floating_ips/${params.id}/actions/unassign`, body: null })
      return json(201, { action: action('unassign_floating_ip') })
    }),
    http.delete('*/v1/floating_ips/:id', ({ params }) => {
      calls.push({ path: `/floating_ips/${params.id}`, body: null })
      return json(200, { action: action('delete_floating_ip') })
    }),
    http.get('*/v1/actions/:id', () => json(200, action('assign_floating_ip', actionStatus))),
  )
})

afterEach(() => {
  unmount()
  document.body.innerHTML = ''
})

async function mountPage() {
  const router = testRouter([
    { path: '/p/:projectId/floating-ips', name: 'floating-ips', component: FloatingIPDetailPage },
    {
      path: '/p/:projectId/floating-ips/:floatingIpId',
      name: 'floating-ip',
      component: FloatingIPDetailPage,
    },
    { path: '/p/:projectId/servers/:serverId', name: 'server', component: FloatingIPDetailPage },
  ])
  await router.push('/p/7/floating-ips/1')
  const m = mountApp(FloatingIPDetailPage, { router })
  unmount = () => m.wrapper.unmount()
  await settle()
  return m
}

const deleteButton = () =>
  Array.from(document.body.querySelectorAll('button')).find(
    (b) => b.textContent?.trim() === 'Delete' && !b.closest('[role="dialog"]'),
  ) as HTMLButtonElement

describe('FloatingIPDetailPage', () => {
  it('shows the address and location of an unassigned floating IP', async () => {
    await mountPage()
    expect(document.body.textContent).toContain('203.0.113.50')
    expect(document.body.textContent).toContain('home')
    expect(document.body.textContent).toContain('Not assigned')
    expect(button('Assign').disabled).toBe(false)
    expect(() => button('Move')).toThrow()
    expect(() => button('Unassign')).toThrow()
    expect(() => button('Show commands')).toThrow()
  })

  it('assigns to a server in the same location and opens the commands', async () => {
    await mountPage()
    button('Assign').click()
    await settle()
    const options = Array.from(
      dialog()!.querySelectorAll<HTMLOptionElement>('select[name="server"] option'),
    ).map((o) => o.textContent!.trim())
    expect(options).toEqual(['Pick a server', 'web1', 'web2'])
    await fill('select[name="server"]', '5')
    document.body.querySelector<HTMLFormElement>('#assign-floating-ip')!.requestSubmit()
    await settle()
    await settle()
    expect(calls).toEqual([{ path: '/floating_ips/1/actions/assign', body: { server: '5' } }])
    const d = dialog()!
    expect(d.textContent).toContain('Run this on the server')
    expect(d.textContent).toContain(
      'New and rebuilt servers set this up automatically. For a running server, run these commands.',
    )
    expect(d.querySelector('[data-testid="guest-apply"]')!.textContent).toContain(
      'sudo ip addr add 203.0.113.50/32 dev eth0',
    )
    expect(d.querySelector('[data-testid="guest-persist"]')).not.toBeNull()
    expect(d.textContent).not.toContain('to stop using the address')
  })

  it('shows the progress in the modal while the assign runs', async () => {
    actionStatus = 'running'
    await mountPage()
    button('Assign').click()
    await settle()
    await fill('select[name="server"]', '5')
    document.body.querySelector<HTMLFormElement>('#assign-floating-ip')!.requestSubmit()
    await settle()
    const d = dialog()!
    expect(d.querySelector('[role="status"]')!.textContent).toContain('Assigning floating IP')
    expect(d.textContent).not.toContain('Run this on the server')
  })

  it('disables every button while releasing and says why', async () => {
    fip = makeFloatingIP({ status: 'releasing' })
    await mountPage()
    expect(document.body.textContent).toContain(
      'The address is still being released from the previous server.',
    )
    expect(button('Assign').disabled).toBe(true)
    expect(button('Delete').disabled).toBe(true)
  })

  it('offers Move, Unassign and Show commands once assigned', async () => {
    fip = makeFloatingIP({ server: 5, guest_config: makeGuestConfig() })
    await mountPage()
    expect(document.body.textContent).toContain('web1')
    expect(() => button('Assign')).toThrow()
    expect(button('Move').disabled).toBe(false)
    expect(button('Unassign').disabled).toBe(false)
    expect(document.body.textContent).not.toContain('The address is still being released')
    button('Show commands').click()
    await settle()
    expect(dialog()!.textContent).toContain('Run this on the server')
  })

  it('moves to another server, never offering the current one', async () => {
    fip = makeFloatingIP({ server: 5, guest_config: makeGuestConfig() })
    await mountPage()
    button('Move').click()
    await settle()
    const options = Array.from(
      dialog()!.querySelectorAll<HTMLOptionElement>('select[name="server"] option'),
    ).map((o) => [o.textContent!.trim(), o.disabled])
    expect(options).toEqual([
      ['Pick a server', true],
      ['web1 (Assigned here now)', true],
      ['web2', false],
    ])
  })

  it('unassigns after confirming', async () => {
    fip = makeFloatingIP({ server: 5, guest_config: makeGuestConfig() })
    await mountPage()
    button('Unassign').click()
    await settle()
    const confirm = Array.from(dialog()!.querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === 'Unassign',
    ) as HTMLButtonElement
    confirm.click()
    await settle()
    expect(calls.map((c) => c.path)).toEqual(['/floating_ips/1/actions/unassign'])
  })

  it('deletes by name', async () => {
    await mountPage()
    button('Delete').click()
    await settle()
    expect(button('Delete floating IP').disabled).toBe(true)
    await fill('input[name="confirm_name"]', 'web-vip')
    button('Delete floating IP').click()
    await settle()
    expect(calls.map((c) => c.path)).toEqual(['/floating_ips/1'])
  })

  it('disables Delete with a lock and tooltip while delete protection is on', async () => {
    fip = makeFloatingIP({ protection: { delete: true } })
    const { wrapper } = await mountPage()
    const del = deleteButton()
    expect(del.disabled).toBe(true)
    expect(del.innerHTML).toContain('M7 11V7a5 5 0 0 1 10 0v4')
    expect(wrapper.findAllComponents(UTooltip).map((t) => t.props('text'))).toContain(
      'Turn off delete protection first.',
    )
  })

  it('leaves Delete alone while delete protection is off', async () => {
    fip = makeFloatingIP({ protection: { delete: false } })
    const { wrapper } = await mountPage()
    expect(deleteButton().disabled).toBe(false)
    expect(wrapper.findAllComponents(UTooltip).map((t) => t.props('text'))).not.toContain(
      'Turn off delete protection first.',
    )
  })
})

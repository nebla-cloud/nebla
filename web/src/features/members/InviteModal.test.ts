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
import { flushPromises } from '@vue/test-utils'
import { http } from 'msw'
import { defineComponent, h, ref } from 'vue'
import { json, server } from '@/test/server'
import { mountApp, testRouter } from '@/test/mount'
import InviteModal from './InviteModal.vue'

let mounted: { unmount: () => void } | null = null
let posts: unknown[]
const open = ref(false)

const Host = defineComponent({
  render: () =>
    h(InviteModal, { open: open.value, 'onUpdate:open': (v: boolean) => (open.value = v) }),
})

async function settle() {
  for (let i = 0; i < 5; i++) {
    await new Promise((r) => setTimeout(r, 10))
    await flushPromises()
  }
}

async function mountModal() {
  const router = testRouter([{ path: '/p/:projectId/members', component: Host }])
  await router.push('/p/7/members')
  open.value = true
  const m = mountApp(Host, { router })
  mounted = m.wrapper
  await settle()
}

const body = () => document.body
const emailInput = () => body().querySelector('input[name="email"]') as HTMLInputElement
const roleSelect = () => body().querySelector('select[name="role"]') as HTMLSelectElement
function button(text: string): HTMLButtonElement {
  const b = Array.from(body().querySelectorAll('button')).find((x) => x.textContent?.includes(text))
  if (!b) throw new Error(`button ${text} not found`)
  return b as HTMLButtonElement
}
async function type(value: string) {
  emailInput().value = value
  emailInput().dispatchEvent(new Event('input'))
  await flushPromises()
}

beforeEach(() => {
  posts = []
  open.value = false
  server.use(
    http.get('*/v1/projects/:id', () => json(200, { id: 7, name: 'Home', role: 'owner' })),
    http.post('*/v1/projects/:id/invites', async ({ request }) => {
      posts.push(await request.json())
      return json(201, { invite_url: 'https://nebla.test/invite/abc123' })
    }),
  )
})

afterEach(() => {
  mounted?.unmount()
  mounted = null
  document.body.innerHTML = ''
})

describe('InviteModal', () => {
  it('requires an email with @', async () => {
    await mountModal()
    await type('nope')
    button('Create invite').click()
    await settle()
    expect(posts).toHaveLength(0)
    expect(body().textContent).toContain('Enter an email address')
  })

  it('shows the API error when creating fails', async () => {
    server.use(
      http.post('*/v1/projects/:id/invites', () =>
        json(422, { error: { code: 'invalid', message: 'That email is not valid.' } }),
      ),
    )
    await mountModal()
    await type('sam@x.io')
    button('Create invite').click()
    await settle()
    const alert = body().querySelector('[role="alert"]')!
    expect(alert.textContent).toContain('That email is not valid.')
    expect(roleSelect().getAttribute('aria-invalid')).toBe('true')
    expect(roleSelect().getAttribute('aria-describedby')).toBe(alert.id)
    expect(body().querySelector('input[readonly]')).toBeNull()
  })

  it('labels the role select', async () => {
    await mountModal()
    expect(body().querySelector(`label[for="${roleSelect().id}"]`)?.textContent).toContain('Role')
  })

  it('default role Member', async () => {
    await mountModal()
    expect(roleSelect().value).toBe('member')
  })

  it('success shows link once', async () => {
    await mountModal()
    await type('sam@x.io')
    button('Create invite').click()
    await settle()
    expect(posts).toEqual([{ email: 'sam@x.io', role: 'member' }])
    const field = body().querySelector('input[readonly]') as HTMLInputElement
    expect(field.value).toBe('https://nebla.test/invite/abc123')
    expect(body().textContent).toContain(
      'Send this link to them. If email is set up on this panel, Nebla emailed it too.',
    )
    open.value = false
    await settle()
    open.value = true
    await settle()
    expect(body().querySelector('input[readonly]')).toBeNull()
    expect(emailInput().value).toBe('')
  })
})

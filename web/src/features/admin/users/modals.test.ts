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
import { nextTick } from 'vue'
import { http } from 'msw'
import type { VueWrapper } from '@vue/test-utils'
import { json, server } from '@/test/server'
import { mountApp } from '@/test/mount'
import { resetSession, setMe } from '@/lib/session'
import type { components } from '@/api/schema'
import UsersPage from './UsersPage.vue'

type AdminUser = components['schemas']['AdminUser']
const ada: AdminUser = {
  id: 1,
  name: 'Ada',
  email: 'ada@example.com',
  is_admin: true,
  totp_enabled: false,
  passkey_count: 0,
}
const bo: AdminUser = {
  id: 2,
  name: 'Bo',
  email: 'bo@example.com',
  is_admin: false,
  totp_enabled: true,
  passkey_count: 2,
}
const cy: AdminUser = {
  id: 3,
  name: 'Cy',
  email: 'cy@example.com',
  is_admin: false,
  totp_enabled: false,
  passkey_count: 0,
}

let calls: { method: string; path: string; query: string; body: unknown }[] = []
let wrappers: VueWrapper[] = []
let handlers: ReturnType<typeof http.get>[] = []

function track(method: string, path: string, status: number, body?: unknown) {
  const fn = http[method as 'post']
  return fn(`*/v1${path}`, async ({ request }) => {
    const u = new URL(request.url)
    let b: unknown
    try {
      b = await request.json()
    } catch {
      b = undefined
    }
    calls.push({ method, path: u.pathname.replace('/v1', ''), query: u.search, body: b })
    return status === 204 ? new Response(null, { status }) : json(status, body)
  })
}

function serve(...extra: ReturnType<typeof http.get>[]) {
  server.use(
    http.get('*/v1/admin/users', () =>
      json(200, {
        users: [ada, bo, cy],
        meta: { pagination: { page: 1, per_page: 50, total_entries: 3 } },
      }),
    ),
    ...extra,
  )
}

async function ready() {
  const m = mountApp(UsersPage)
  wrappers.push(m.wrapper)
  await vi.waitFor(() => expect(m.wrapper.text()).toContain('bo@example.com'))
  return m.wrapper
}

async function pick(wrapper: VueWrapper, name: string, item: string) {
  await wrapper.find(`button[aria-label="Actions for ${name}"]`).trigger('click')
  await vi.waitFor(() => expect(document.body.querySelector('[role="menu"]')).not.toBeNull())
  const el = [...document.body.querySelectorAll('[role="menuitem"]')].find(
    (e) => e.textContent?.trim() === item,
  ) as HTMLElement
  el.click()
}

const field = (name: string) =>
  document.body.querySelector(`input[name="${name}"]`) as HTMLInputElement

function fill(name: string, v: string) {
  const el = field(name)
  el.value = v
  el.dispatchEvent(new Event('input'))
}

function check(name: string, v: boolean) {
  const el = field(name)
  el.checked = v
  el.dispatchEvent(new Event('change'))
}

function submit() {
  document.body.querySelector('form')!.dispatchEvent(new Event('submit', { cancelable: true }))
}

const button = (label: string) =>
  [...document.body.querySelectorAll('button')].find((b) => b.textContent?.trim() === label)

beforeEach(() => {
  calls = []
  handlers = []
  resetSession()
  setMe({ id: 1, name: 'Ada', email: 'ada@example.com', is_admin: true, totp_enabled: false })
})

afterEach(() => {
  wrappers.forEach((w) => w.unmount())
  wrappers = []
  document.body.innerHTML = ''
  void handlers
})

describe('user modals', () => {
  it('New user sends is_admin and needs 10 characters', async () => {
    serve(track('post', '/admin/users', 201, { ...cy, id: 9 }))
    const w = await ready()
    const btn = w.findAll('button').find((b) => b.text() === 'New user')!
    await btn.trigger('click')
    await vi.waitFor(() => expect(field('email')).not.toBeNull())
    fill('name', 'Dee')
    fill('email', 'dee@example.com')
    fill('password', 'short')
    submit()
    await vi.waitFor(() => expect(document.body.textContent).toContain('at least 10 characters'))
    expect(calls).toHaveLength(0)
    fill('password', 'long enough pw')
    check('is_admin', true)
    submit()
    await vi.waitFor(() => expect(calls).toHaveLength(1))
    expect(calls[0]!.body).toEqual({
      name: 'Dee',
      email: 'dee@example.com',
      password: 'long enough pw',
      is_admin: true,
    })
  })

  it('Edit warns when the email changes', async () => {
    serve(track('put', '/admin/users/3', 200, cy))
    const w = await ready()
    await pick(w, 'Cy', 'Edit')
    await vi.waitFor(() => expect(field('email')).not.toBeNull())
    expect(document.body.textContent).not.toContain('log in with the new email')
    fill('email', ' CY@Example.com ')
    await nextTick()
    expect(document.body.textContent).not.toContain('log in with the new email')
    fill('email', 'cy2@example.com')
    await vi.waitFor(() =>
      expect(document.body.textContent).toContain(
        "They'll be signed out and log in with the new email.",
      ),
    )
  })

  it('Reset password offers removing passkeys only when they have some', async () => {
    serve(
      track('post', '/admin/users/2/password', 204),
      track('post', '/admin/users/3/password', 204),
    )
    const w = await ready()
    await pick(w, 'Cy', 'Reset password')
    await vi.waitFor(() => expect(field('password')).not.toBeNull())
    expect(document.body.textContent).not.toContain('Also remove their passkeys')
    document.body.querySelector<HTMLElement>('[aria-label="Close"]')?.click()
    await vi.waitFor(() => expect(field('password')).toBeNull())

    await pick(w, 'Bo', 'Reset password')
    await vi.waitFor(() => expect(field('password')).not.toBeNull())
    expect(document.body.textContent).toContain('Also remove their passkeys')
    expect(field('remove_passkeys').checked).toBe(true)
    fill('password', 'a new password')
    fill('confirm', 'different one')
    submit()
    await vi.waitFor(() => expect(document.body.textContent).toContain("Passwords don't match"))
    fill('confirm', 'a new password')
    submit()
    await vi.waitFor(() => expect(calls).toHaveLength(1))
    expect(calls[0]!.body).toEqual({ password: 'a new password', remove_passkeys: true })
    await vi.waitFor(() =>
      expect(document.body.textContent).toContain('Password changed. They were signed out.'),
    )
  })

  it('Turn off two-factor confirm text and call', async () => {
    serve(track('delete', '/admin/users/2/totp', 204))
    const w = await ready()
    await pick(w, 'Bo', 'Turn off two-factor')
    await vi.waitFor(() =>
      expect(document.body.textContent).toContain(
        "They'll log in with just their password and can turn it back on from their account page.",
      ),
    )
    button('Turn off two-factor')!.click()
    await vi.waitFor(() => expect(calls).toHaveLength(1))
    expect(calls[0]!.path).toBe('/admin/users/2/totp')
  })

  it('Remove passkeys confirm text and call', async () => {
    serve(track('delete', '/admin/users/2/passkeys', 204))
    const w = await ready()
    await pick(w, 'Bo', 'Remove passkeys')
    await vi.waitFor(() =>
      expect(document.body.textContent).toContain(
        "They'll sign in with their password (and two-factor code, if it's on) until they add a new passkey.",
      ),
    )
    button('Remove passkeys')!.click()
    await vi.waitFor(() => expect(calls).toHaveLength(1))
    expect(calls[0]!.path).toBe('/admin/users/2/passkeys')
  })

  it('Delete needs the email, and sole_owner offers a takeover', async () => {
    let n = 0
    serve(
      http.delete('*/v1/admin/users/3', ({ request }) => {
        const q = new URL(request.url).search
        calls.push({ method: 'delete', path: '/admin/users/3', query: q, body: undefined })
        if (n++ === 0) {
          return json(409, {
            error: { code: 'sole_owner', message: "They're the only owner of Homelab." },
          })
        }
        return new Response(null, { status: 204 })
      }),
    )
    const m = mountApp(UsersPage)
    wrappers.push(m.wrapper)
    await vi.waitFor(() => expect(m.wrapper.text()).toContain('bo@example.com'))
    const invalidate = vi.spyOn(m.queryClient, 'invalidateQueries')
    await pick(m.wrapper, 'Cy', 'Delete')
    await vi.waitFor(() => expect(field('confirm_name')).not.toBeNull())
    fill('confirm_name', 'cy@example.com')
    await vi.waitFor(() => expect(button('Delete user')!.disabled).toBe(false))
    button('Delete user')!.click()
    await vi.waitFor(() =>
      expect(document.body.textContent).toContain("They're the only owner of Homelab."),
    )
    expect(calls[0]!.query).toBe('')
    expect(field('confirm_name')).not.toBeNull()
    expect(button('Delete user')).toBeUndefined()
    fill('confirm_name', 'someone@example.com')
    await vi.waitFor(() => expect(button('Make me the owner and delete')!.disabled).toBe(true))
    fill('confirm_name', 'cy@example.com')
    await vi.waitFor(() => expect(button('Make me the owner and delete')!.disabled).toBe(false))
    button('Make me the owner and delete')!.click()
    await vi.waitFor(() => expect(calls).toHaveLength(2))
    expect(calls[1]!.query).toBe('?take_over=true')
    await vi.waitFor(() => expect(field('confirm_name')).toBeNull())
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ['admin', 'projects'] })
  })

  it('last_admin shows inline', async () => {
    serve(
      http.delete('*/v1/admin/users/3', () =>
        json(409, {
          error: { code: 'last_admin', message: 'The panel needs at least one admin.' },
        }),
      ),
    )
    const w = await ready()
    await pick(w, 'Cy', 'Delete')
    await vi.waitFor(() => expect(field('confirm_name')).not.toBeNull())
    fill('confirm_name', 'cy@example.com')
    await vi.waitFor(() => expect(button('Delete user')!.disabled).toBe(false))
    button('Delete user')!.click()
    await vi.waitFor(() =>
      expect(document.body.textContent).toContain('The panel needs at least one admin.'),
    )
    expect(button('Make me the owner and delete')).toBeUndefined()
  })

  it('self_admin_action shows inline on edit', async () => {
    serve(
      http.put('*/v1/admin/users/3', () =>
        json(422, {
          error: {
            code: 'self_admin_action',
            message: 'Use your account page to change this for yourself.',
          },
        }),
      ),
    )
    const w = await ready()
    await pick(w, 'Cy', 'Edit')
    await vi.waitFor(() => expect(field('name')).not.toBeNull())
    submit()
    await vi.waitFor(() =>
      expect(document.body.textContent).toContain(
        'Use your account page to change this for yourself.',
      ),
    )
  })
})

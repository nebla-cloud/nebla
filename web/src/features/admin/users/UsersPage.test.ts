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
import { http } from 'msw'
import type { VueWrapper } from '@vue/test-utils'
import { json, server } from '@/test/server'
import { mountApp, testRouter } from '@/test/mount'
import { resetSession, setMe, useSession } from '@/lib/session'
import UsersPage from './UsersPage.vue'

const users = [
  {
    id: 1,
    name: 'Ada',
    email: 'ada@example.com',
    is_admin: true,
    totp_enabled: true,
    passkey_count: 2,
  },
  {
    id: 2,
    name: 'Bo',
    email: 'bo@example.com',
    is_admin: false,
    totp_enabled: false,
    passkey_count: 0,
  },
  {
    id: 3,
    name: 'Cy',
    email: 'cy@example.com',
    is_admin: false,
    totp_enabled: true,
    passkey_count: 3,
  },
]

let pages: string[] = []
let puts: unknown[] = []
let wrappers: VueWrapper[] = []

function serve(total = 3) {
  server.use(
    http.get('*/v1/admin/users', ({ request }) => {
      pages.push(new URL(request.url).searchParams.get('page') ?? '')
      return json(200, {
        users,
        meta: { pagination: { page: 1, per_page: 50, total_entries: total } },
      })
    }),
    http.put('*/v1/admin/users/1', async ({ request }) => {
      puts.push(await request.json())
      return json(200, { ...users[0], ...((await Promise.resolve(puts.at(-1))) as object) })
    }),
  )
}

function mount() {
  const router = testRouter()
  const m = mountApp(UsersPage, { router })
  wrappers.push(m.wrapper)
  return m
}

async function openMenu(wrapper: VueWrapper, name: string) {
  await wrapper.find(`button[aria-label="Actions for ${name}"]`).trigger('click')
  await vi.waitFor(() => expect(document.body.querySelector('[role="menu"]')).not.toBeNull())
  return [...document.body.querySelectorAll('[role="menuitem"]')].map((e) => e.textContent?.trim())
}

beforeEach(() => {
  pages = []
  puts = []
  resetSession()
  setMe({ id: 1, name: 'Ada', email: 'ada@example.com', is_admin: true, totp_enabled: true })
})

afterEach(() => {
  wrappers.forEach((w) => w.unmount())
  wrappers = []
  document.body.innerHTML = ''
})

describe('UsersPage', () => {
  it('shows the columns and marks your row', async () => {
    serve()
    const { wrapper } = mount()
    await vi.waitFor(() => expect(wrapper.text()).toContain('bo@example.com'))
    const rows = wrapper.findAll('tbody tr')
    expect(rows).toHaveLength(3)
    expect(rows[0]!.text()).toContain('Ada')
    expect(rows[0]!.text()).toContain('(you)')
    expect(rows[0]!.text()).toContain('Admin')
    expect(rows[0]!.text()).toContain('On')
    expect(rows[1]!.text()).toContain('Off')
    expect(rows[1]!.text()).not.toContain('(you)')
    const cells = rows[1]!.findAll('td')
    expect(cells.some((c) => c.attributes('data-col') === 'passkeys' && c.text() === '')).toBe(true)
    expect(rows[2]!.find('td[data-col="passkeys"]').text()).toContain('3')
    // The row-actions button sits in the last cell.
    const last = rows[0]!.findAll('td').at(-1)!
    expect(last.attributes('data-col')).toBe('actions')
    expect(last.find('button').exists()).toBe(true)
    expect(wrapper.text()).toContain(
      "There's no email from here. Send them their password yourself.",
    )
  })

  it('your own row menu has only Edit', async () => {
    serve()
    const { wrapper } = mount()
    await vi.waitFor(() => expect(wrapper.text()).toContain('bo@example.com'))
    expect(await openMenu(wrapper, 'Ada')).toEqual(['Edit'])
  })

  it('own edit form has no admin checkbox', async () => {
    serve()
    const { wrapper } = mount()
    await vi.waitFor(() => expect(wrapper.text()).toContain('bo@example.com'))
    await openMenu(wrapper, 'Ada')
    ;(document.body.querySelector('[role="menuitem"]') as HTMLElement).click()
    await vi.waitFor(() => expect(document.body.querySelector('input[name="name"]')).not.toBeNull())
    expect(document.body.querySelector('input[name="is_admin"]')).toBeNull()
  })

  it('other rows offer actions that apply', async () => {
    serve()
    const { wrapper } = mount()
    await vi.waitFor(() => expect(wrapper.text()).toContain('bo@example.com'))
    expect(await openMenu(wrapper, 'Bo')).toEqual(['Edit', 'Reset password', 'Delete'])
  })

  it('offers every action when two-factor and passkeys are on', async () => {
    serve()
    const { wrapper } = mount()
    await vi.waitFor(() => expect(wrapper.text()).toContain('bo@example.com'))
    expect(await openMenu(wrapper, 'Cy')).toEqual([
      'Edit',
      'Reset password',
      'Turn off two-factor',
      'Remove passkeys',
      'Delete',
    ])
  })

  it('editing yourself updates me', async () => {
    serve()
    const { wrapper } = mount()
    await vi.waitFor(() => expect(wrapper.text()).toContain('bo@example.com'))
    await openMenu(wrapper, 'Ada')
    ;(document.body.querySelector('[role="menuitem"]') as HTMLElement).click()
    await vi.waitFor(() => expect(document.body.querySelector('input[name="name"]')).not.toBeNull())
    const input = document.body.querySelector('input[name="name"]') as HTMLInputElement
    input.value = 'Ada L'
    input.dispatchEvent(new Event('input'))
    document.body.querySelector('form')!.dispatchEvent(new Event('submit', { cancelable: true }))
    await vi.waitFor(() => expect(useSession().me.value?.name).toBe('Ada L'))
    expect(puts[0]).toMatchObject({ name: 'Ada L', is_admin: true })
  })

  it('the pager changes the page query param', async () => {
    serve(120)
    const { wrapper, router } = mount()
    await vi.waitFor(() => expect(wrapper.text()).toContain('Showing 1'))
    const next = wrapper.findAll('button').find((b) => b.text() === 'Next')!
    await next.trigger('click')
    await vi.waitFor(() => expect(router.currentRoute.value.query.page).toBe('2'))
    await vi.waitFor(() => expect(pages).toContain('2'))
  })

  it('an empty page past the end moves back to the last page', async () => {
    server.use(
      http.get('*/v1/admin/users', ({ request }) => {
        const page = new URL(request.url).searchParams.get('page') ?? '1'
        pages.push(page)
        return json(200, {
          users: page === '3' ? [] : users,
          meta: { pagination: { page: Number(page), per_page: 50, total_entries: 100 } },
        })
      }),
    )
    const router = testRouter()
    await router.push('/admin/users?page=3')
    const m = mountApp(UsersPage, { router })
    wrappers.push(m.wrapper)
    await vi.waitFor(() => expect(router.currentRoute.value.query.page).toBe('2'))
    await vi.waitFor(() => expect(m.wrapper.text()).toContain('bo@example.com'))
    expect(m.wrapper.text()).not.toContain('Nobody has an account yet.')
  })
})

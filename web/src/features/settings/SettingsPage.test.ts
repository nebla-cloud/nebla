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
import { json, server } from '@/test/server'
import { mountApp, testQueryClient, testRouter } from '@/test/mount'
import { setMe } from '@/lib/session'
import { readLastProject, writeLastProject } from '@/lib/project'
import SettingsPage from './SettingsPage.vue'

type Role = 'owner' | 'admin' | 'member' | 'read_only'

let mounted: { unmount: () => void } | null = null
let role: Role
let members: { user_id: number; email: string; name: string; role: Role }[]
let calls: { method: string; url: string; body?: unknown }[]
let leaveReply: () => Response
let deleteReply: () => Response

const ME = { id: 3, email: 'me@x.io', name: 'Me', is_admin: false, totp_enabled: false }

async function mountPage() {
  const router = testRouter([{ path: '/p/:projectId/settings', component: SettingsPage }])
  await router.push('/p/7/settings')
  const queryClient = testQueryClient()
  const m = mountApp(SettingsPage, { router, queryClient })
  mounted = m.wrapper
  await settle()
  return { ...m, queryClient }
}

async function settle() {
  for (let i = 0; i < 5; i++) {
    await new Promise((r) => setTimeout(r, 10))
    await flushPromises()
  }
}

function body(): HTMLElement {
  return document.body
}

function button(text: string): HTMLButtonElement {
  const b = Array.from(body().querySelectorAll('button')).find((x) => x.textContent?.includes(text))
  if (!b) throw new Error(`button ${text} not found`)
  return b as HTMLButtonElement
}

async function fill(selector: string, value: string) {
  const el = body().querySelector(selector) as HTMLInputElement
  el.value = value
  el.dispatchEvent(new Event('input'))
  await flushPromises()
}

beforeEach(() => {
  role = 'owner'
  members = [
    { user_id: 3, email: 'me@x.io', name: 'Me', role: 'owner' },
    { user_id: 4, email: 'a@x.io', name: 'A', role: 'owner' },
  ]
  calls = []
  leaveReply = () => new Response(null, { status: 204 })
  deleteReply = () => new Response(null, { status: 204 })
  setMe(ME)
  window.localStorage.clear()
  server.use(
    http.get('*/v1/projects/:id', () =>
      json(200, { id: 7, name: 'Home', role, usage: { servers: 0 } }),
    ),
    http.get('*/v1/projects/:id/members', () => json(200, { members })),
    http.put('*/v1/projects/:id', async ({ request }) => {
      calls.push({ method: 'PUT', url: request.url, body: await request.json() })
      return json(200, { id: 7, name: 'Renamed', role, usage: { servers: 0 } })
    }),
    http.delete('*/v1/projects/:id/members/:uid', ({ request }) => {
      calls.push({ method: 'DELETE', url: request.url })
      return leaveReply()
    }),
    http.delete('*/v1/projects/:id', ({ request }) => {
      calls.push({ method: 'DELETE', url: request.url })
      return deleteReply()
    }),
  )
})

afterEach(() => {
  mounted?.unmount()
  mounted = null
  document.body.innerHTML = ''
  setMe(null)
})

describe('SettingsPage', () => {
  it('rename saves and updates switcher', async () => {
    const { queryClient } = await mountPage()
    const invalidated: unknown[] = []
    const orig = queryClient.invalidateQueries.bind(queryClient)
    queryClient.invalidateQueries = ((f: { queryKey?: unknown }) => {
      invalidated.push(f?.queryKey)
      return orig(f as never)
    }) as typeof queryClient.invalidateQueries
    await fill('input[name="name"]', 'Renamed')
    body().querySelector<HTMLFormElement>('#rename-project')!.requestSubmit()
    await settle()
    expect(calls).toEqual([expect.objectContaining({ method: 'PUT', body: { name: 'Renamed' } })])
    expect(calls[0]!.url).toMatch(/\/projects\/7$/)
    expect(invalidated).toContainEqual(['projects'])
    expect(invalidated).toContainEqual(['project', 7])
  })

  it('member sees name as text', async () => {
    role = 'member'
    await mountPage()
    expect(body().querySelector('input[name="name"]')).toBeNull()
    expect(body().textContent).toContain('Home')
  })

  it('last owner cannot leave', async () => {
    members = [{ user_id: 3, email: 'me@x.io', name: 'Me', role: 'owner' }]
    await mountPage()
    expect(button('Leave project').disabled).toBe(true)
    expect(body().textContent).toContain(
      "You're the only owner. Make someone else an owner first, or delete the project.",
    )
  })

  it('leave stays disabled while members load', async () => {
    server.use(
      http.get('*/v1/projects/:id/members', async () => {
        await new Promise((r) => setTimeout(r, 500))
        return json(200, { members })
      }),
    )
    await mountPage()
    expect(button('Leave project').disabled).toBe(true)
  })

  it('leave is enabled when the members list fails to load', async () => {
    server.use(
      http.get('*/v1/projects/:id/members', () =>
        json(500, { error: { code: 'internal', message: 'boom' } }),
      ),
    )
    await mountPage()
    expect(button('Leave project').disabled).toBe(false)
  })

  it('leave confirms then goes home', async () => {
    writeLastProject(7)
    const { router, queryClient } = await mountPage()
    button('Leave project').click()
    await settle()
    expect(body().textContent).toContain('API tokens you made in it are revoked.')
    expect(calls).toHaveLength(0)
    const confirm = Array.from(body().querySelectorAll('button')).filter((b) =>
      b.textContent?.includes('Leave project'),
    )
    confirm[confirm.length - 1]!.click()
    await settle()
    expect(calls).toEqual([expect.objectContaining({ method: 'DELETE' })])
    expect(calls[0]!.url).toMatch(/\/projects\/7\/members\/3$/)
    expect(readLastProject()).toBeNull()
    // only inert id-0 placeholders from the unmounting page may remain
    expect(
      queryClient
        .getQueryCache()
        .getAll()
        .filter((q) => q.state.data !== undefined),
    ).toEqual([])
    expect(router.currentRoute.value.fullPath).toBe('/')
  })

  it('last_owner race shows message', async () => {
    leaveReply = () =>
      json(409, { error: { code: 'last_owner', message: 'a project needs at least one owner' } })
    const { router } = await mountPage()
    button('Leave project').click()
    await settle()
    const confirm = Array.from(body().querySelectorAll('button')).filter((b) =>
      b.textContent?.includes('Leave project'),
    )
    confirm[confirm.length - 1]!.click()
    await settle()
    expect(body().textContent).toContain('a project needs at least one owner')
    expect(router.currentRoute.value.fullPath).toBe('/p/7/settings')
  })

  it('delete is owner only', async () => {
    role = 'admin'
    await mountPage()
    expect(body().textContent).not.toContain('Delete project')
  })

  it('delete with servers', async () => {
    deleteReply = () =>
      json(409, { error: { code: 'project_not_empty', message: 'delete the servers' } })
    await mountPage()
    button('Delete project').click()
    await settle()
    await fill('input[name="confirm_name"]', 'Home')
    const confirms = Array.from(body().querySelectorAll('button')).filter((b) =>
      b.textContent?.includes('Delete project'),
    )
    confirms[confirms.length - 1]!.click()
    await settle()
    expect(body().textContent).toContain(
      "Delete the project's servers, volumes, floating IPs and snapshots first.",
    )
    const link = Array.from(body().querySelectorAll('a')).find((a) =>
      a.textContent?.includes('Servers'),
    )
    expect(link?.getAttribute('href')).toBe('/p/7/servers')
  })
})

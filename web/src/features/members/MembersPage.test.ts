import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { http } from 'msw'
import { json, server } from '@/test/server'
import { mountApp, testRouter } from '@/test/mount'
import { setMe } from '@/lib/session'
import type { Role } from '@/api/types'
import MembersPage from './MembersPage.vue'

let mounted: { unmount: () => void } | null = null
let myRole: Role
let calls: { method: string; url: string; body?: unknown }[]
let putStatus: number
let projectGets: number

const members = [
  { user_id: 1, email: 'owner@x.io', name: 'Olive Owner', role: 'owner' },
  { user_id: 2, email: 'me@x.io', name: 'Me Self', role: 'admin' },
  { user_id: 3, email: 'sam@x.io', name: 'Sam Reed', role: 'member' },
]

async function settle() {
  for (let i = 0; i < 5; i++) {
    await new Promise((r) => setTimeout(r, 10))
    await flushPromises()
  }
}

async function mountPage(role: Role, meId = 2) {
  myRole = role
  setMe({ id: meId, email: 'me@x.io', name: 'Me Self', is_admin: false } as never)
  const router = testRouter([{ path: '/p/:projectId/members', component: MembersPage }])
  await router.push('/p/7/members')
  const m = mountApp(MembersPage, { router })
  mounted = m.wrapper
  await settle()
  return m
}

const body = () => document.body
function labelled(label: string): HTMLButtonElement {
  const b = body().querySelector(`button[aria-label="${label}"]`)
  if (!b) throw new Error(`button ${label} not found`)
  return b as HTMLButtonElement
}
function dialogButton(text: string): HTMLButtonElement {
  const d = body().querySelector('[role="dialog"]')!
  const b = Array.from(d.querySelectorAll('button')).find((x) => x.textContent?.trim() === text)
  if (!b) throw new Error(`dialog button ${text} not found`)
  return b as HTMLButtonElement
}
const selects = () => Array.from(body().querySelectorAll('select')) as HTMLSelectElement[]
function button(text: string): HTMLButtonElement {
  const b = Array.from(body().querySelectorAll('button')).find((x) => x.textContent?.includes(text))
  if (!b) throw new Error(`button ${text} not found`)
  return b as HTMLButtonElement
}
async function choose(sel: HTMLSelectElement, value: string) {
  sel.value = value
  sel.dispatchEvent(new Event('change'))
  await settle()
}

beforeEach(() => {
  calls = []
  putStatus = 200
  projectGets = 0
  server.use(
    http.get('*/v1/projects/:id', () => {
      projectGets++
      return json(200, { id: 7, name: 'Home', role: myRole, usage: { servers: 0 } })
    }),
    http.get('*/v1/projects/:id/members', () => json(200, { members })),
    http.get('*/v1/projects/:id/invites', ({ request }) => {
      calls.push({ method: 'GET', url: request.url })
      return json(200, {
        invites: [
          {
            id: 9,
            email: 'new@x.io',
            role: 'member',
            created_by: 1,
            expires: new Date(Date.now() + 5.5 * 86_400_000).toISOString(),
          },
        ],
      })
    }),
    http.put('*/v1/projects/:id/members/:uid', async ({ request }) => {
      calls.push({ method: 'PUT', url: request.url, body: await request.json() })
      if (putStatus === 409)
        return json(409, { error: { code: 'last_owner', message: 'A project needs an owner.' } })
      return json(200, members[2])
    }),
    http.delete('*/v1/projects/:id/invites/:iid', ({ request }) => {
      calls.push({ method: 'DELETE', url: request.url })
      return new Response(null, { status: 204 })
    }),
    http.delete('*/v1/projects/:id/members/:uid', ({ request }) => {
      calls.push({ method: 'DELETE', url: request.url })
      return new Response(null, { status: 204 })
    }),
  )
})

afterEach(() => {
  mounted?.unmount()
  mounted = null
  document.body.innerHTML = ''
})

describe('MembersPage', () => {
  it('your row says (you)', async () => {
    await mountPage('owner', 2)
    const rows = Array.from(body().querySelectorAll('li'))
    expect(rows.find((r) => r.textContent?.includes('me@x.io'))?.textContent).toContain('(you)')
    expect(rows.find((r) => r.textContent?.includes('sam@x.io'))?.textContent).not.toContain(
      '(you)',
    )
  })

  it('read_only sees list only', async () => {
    await mountPage('read_only')
    expect(body().textContent).toContain('Sam Reed')
    expect(selects()).toHaveLength(0)
    expect(body().textContent).not.toContain('Remove')
    expect(body().textContent).not.toContain('Invite')
    expect(body().textContent).not.toContain('Pending invites')
    expect(calls.filter((c) => c.url.includes('/invites'))).toHaveLength(0)
  })

  it('admin sees no controls on owner row', async () => {
    await mountPage('admin')
    const owner = Array.from(body().querySelectorAll('li')).find((r) =>
      r.textContent?.includes('owner@x.io'),
    )!
    expect(owner.querySelector('select')).toBeNull()
    expect(owner.textContent).not.toContain('Remove')
    expect(owner.textContent).toContain('Owner')
  })

  it('admin role options exclude Owner', async () => {
    await mountPage('admin')
    const opts = Array.from(selects()[0]!.options).map((o) => o.textContent)
    expect(opts).toEqual(['Admin', 'Member', 'Read-only'])
  })

  it('role change error reverts', async () => {
    await mountPage('owner')
    putStatus = 409
    const sel = selects().find((s) => s.value === 'member')!
    await choose(sel, 'admin')
    expect(calls.some((c) => c.method === 'PUT')).toBe(true)
    expect(body().textContent).toContain('A project needs an owner.')
    expect(sel.value).toBe('member')
  })

  it('remove confirms with token note', async () => {
    await mountPage('owner')
    labelled('Remove Sam Reed').click()
    await settle()
    expect(body().textContent).toContain('API tokens they made keep working')
    expect(calls.some((c) => c.method === 'DELETE')).toBe(false)
    dialogButton('Remove').click()
    await settle()
    const del = calls.filter((c) => c.method === 'DELETE')
    expect(del).toHaveLength(1)
    expect(del[0]!.url).toContain('/v1/projects/7/members/3')
    expect(body().textContent).toContain('Removed Sam Reed')
  })

  it('cancelling the remove confirm sends nothing', async () => {
    await mountPage('owner')
    labelled('Remove Sam Reed').click()
    await settle()
    dialogButton('Cancel').click()
    await settle()
    expect(calls.some((c) => c.method === 'DELETE')).toBe(false)
  })

  it('revoke confirms and deletes the invite', async () => {
    await mountPage('owner')
    labelled('Revoke invite for new@x.io').click()
    await settle()
    expect(calls.some((c) => c.method === 'DELETE')).toBe(false)
    dialogButton('Revoke').click()
    await settle()
    const del = calls.filter((c) => c.method === 'DELETE')
    expect(del).toHaveLength(1)
    expect(del[0]!.url).toContain('/v1/projects/7/invites/9')
    expect(body().textContent).toContain('Revoked the invite for new@x.io')
  })

  it('demoting yourself refetches the project and hides Invite', async () => {
    await mountPage('admin')
    expect(body().textContent).toContain('Invite')
    const before = projectGets
    const mine = selects().find((s) => s.getAttribute('aria-label')?.includes('Me Self'))!
    await choose(mine, 'member')
    button('Change role').click()
    myRole = 'member'
    await settle()
    expect(projectGets).toBeGreaterThan(before)
    expect(
      Array.from(body().querySelectorAll('button')).some((b) => b.textContent?.trim() === 'Invite'),
    ).toBe(false)
  })

  it('demoting confirms with the token note and promoting does not', async () => {
    await mountPage('owner')
    const adminSel = selects().find((s) => s.value === 'admin')!
    await choose(adminSel, 'read_only')
    expect(body().textContent).toContain('API tokens they made keep working')
    expect(calls.some((c) => c.method === 'PUT')).toBe(false)
    button('Change role').click()
    await settle()
    expect(calls.filter((c) => c.method === 'PUT')).toHaveLength(1)

    calls = []
    const memberSel = selects().find((s) => s.value === 'member')!
    await choose(memberSel, 'admin')
    expect(calls.filter((c) => c.method === 'PUT')).toHaveLength(1)
  })

  it('pending invites list', async () => {
    await mountPage('owner')
    expect(body().textContent).toContain('new@x.io')
    expect(body().textContent).toContain('expires in 6 days')
    expect(body().textContent).toContain('Revoke')
    expect(body().textContent).not.toContain('Copy link')
  })
})

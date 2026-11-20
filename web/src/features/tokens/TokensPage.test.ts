import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { http } from 'msw'
import { json, server } from '@/test/server'
import { mountApp, testQueryClient, testRouter } from '@/test/mount'
import { projectQuery } from '@/features/projects/queries'
import * as project from '@/lib/project'
import TokensPage from './TokensPage.vue'

vi.mock('@/lib/project', async (orig) => {
  const actual = await orig<typeof import('@/lib/project')>()
  return { ...actual, recheckAccess: vi.fn(actual.recheckAccess) }
})

let mounted: { unmount: () => void } | null = null
let tokens: unknown[]
let status: number
let role: string
let calls: string[]

async function settle() {
  for (let i = 0; i < 5; i++) {
    await new Promise((r) => setTimeout(r, 10))
    await flushPromises()
  }
}

const body = () => document.body

function button(text: string): HTMLButtonElement {
  const b = Array.from(body().querySelectorAll('button')).find((x) => x.textContent?.includes(text))
  if (!b) throw new Error(`button ${text} not found`)
  return b as HTMLButtonElement
}

async function mountPage() {
  const router = testRouter([{ path: '/p/:projectId/tokens', component: TokensPage }])
  await router.push('/p/7/tokens')
  const m = mountApp(TokensPage, { router })
  mounted = m.wrapper
  await settle()
  return m
}

beforeEach(() => {
  status = 200
  role = 'owner'
  calls = []
  tokens = [
    {
      id: 1,
      name: 'ci',
      permission: 'read_write',
      created: '2026-11-01T10:00:00Z',
      last_used: null,
    },
  ]
  vi.mocked(project.recheckAccess).mockClear()
  server.use(
    http.get('*/v1/projects/:id', () =>
      json(200, { id: 7, name: 'Home lab', role, usage: { servers: 0 } }),
    ),
    http.get('*/v1/projects/:id/tokens', () =>
      status === 403
        ? json(403, { error: { code: 'forbidden', message: 'No.' } })
        : json(200, { tokens }),
    ),
    http.delete('*/v1/projects/:id/tokens/:tid', ({ request }) => {
      calls.push(request.url)
      tokens = []
      return new Response(null, { status: 204 })
    }),
  )
})

afterEach(() => {
  mounted?.unmount()
  mounted = null
  document.body.innerHTML = ''
})

describe('TokensPage', () => {
  it('lists tokens', async () => {
    await mountPage()
    const t = body().textContent ?? ''
    for (const s of [
      'Name',
      'Permission',
      'Created',
      'Last used',
      'ci',
      'Read and write',
      'Never',
    ]) {
      expect(t).toContain(s)
    }
  })

  it('empty state', async () => {
    tokens = []
    await mountPage()
    expect(body().textContent).toContain('let the CLI and scripts use this project')
  })

  it('revoke confirms then deletes', async () => {
    await mountPage()
    button('Revoke').click()
    await settle()
    expect(body().textContent).toContain('stops working right away')
    expect(calls).toHaveLength(0)
    button('Revoke token').click()
    await settle()
    expect(calls).toHaveLength(1)
    expect(calls[0]).toMatch(/\/projects\/7\/tokens\/1$/)
  })

  it('403 rechecks access', async () => {
    status = 403
    role = 'member'
    const qc = testQueryClient()
    const router = testRouter([
      {
        path: '/p/:projectId/tokens',
        component: TokensPage,
        meta: { roles: ['owner', 'admin'] },
      },
      { path: '/nope', name: 'not-found', component: { render: () => null } },
    ])
    let guarded = false
    router.beforeEach(async (to) => {
      if (!guarded || !to.meta.roles) return true
      const p = await qc.fetchQuery({ ...projectQuery(7), staleTime: 0 })
      return to.meta.roles.includes(p.role) ? true : { name: 'not-found' }
    })
    role = 'owner'
    await router.push('/p/7/tokens')
    guarded = true
    role = 'member'
    const m = mountApp(TokensPage, { router, queryClient: qc })
    mounted = m.wrapper
    await settle()
    expect(project.recheckAccess).toHaveBeenCalled()
    expect(router.currentRoute.value.name).toBe('not-found')
  })
})

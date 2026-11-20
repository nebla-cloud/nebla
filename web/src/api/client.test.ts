import { describe, expect, it, vi } from 'vitest'
import { http } from 'msw'
import { json, server } from '@/test/server'
import { testRouter } from '@/test/mount'
import { parseProjectId } from '@/lib/project'
import {
  api,
  onForbidden,
  onUnauthorized,
  redirectOnUnauthorized,
  reportUnauthorized,
  setProjectSource,
} from './client'
import { ApiError, errorMessage, unwrap } from './errors'

describe('api client', () => {
  it('sends the Project-ID of the current route at request time', async () => {
    const seen: (string | null)[] = []
    server.use(
      http.get('*/v1/servers', ({ request }) => {
        seen.push(request.headers.get('Project-ID'))
        return json(200, { data: [], meta: {} })
      }),
    )
    const router = testRouter([
      { path: '/p/:projectId/servers', name: 'servers', component: { render: () => null } },
      { path: '/projects', name: 'projects', component: { render: () => null } },
    ])
    setProjectSource(() => parseProjectId(router.currentRoute.value.params.projectId))
    await router.push('/p/7/servers')
    await api.GET('/servers')
    await router.push('/p/8/servers')
    await api.GET('/servers')
    await router.push('/projects')
    await api.GET('/servers')
    setProjectSource(null)
    expect(seen).toEqual(['7', '8', null])
  })

  it('calls onUnauthorized on 401 except from /me', async () => {
    const fn = vi.fn()
    onUnauthorized(fn)
    const body = { error: { code: 'unauthorized', message: 'no' } }
    server.use(
      http.get('*/v1/servers', () => json(401, body)),
      http.get('*/v1/me', () => json(401, body)),
    )
    await api.GET('/me')
    expect(fn).not.toHaveBeenCalled()
    await api.GET('/servers')
    expect(fn).toHaveBeenCalledOnce()
  })

  it("quiet paths don't redirect", async () => {
    const fn = vi.fn()
    onUnauthorized(fn)
    const body = { error: { code: 'unauthorized', message: 'no' } }
    server.use(
      http.post('*/v1/me/password', () => json(401, body)),
      http.post('*/v1/invites/:token/accept', () => json(401, body)),
      http.get('*/v1/servers', () => json(401, body)),
    )
    await api.POST('/me/password', { body: { current_password: 'a', new_password: 'b' } })
    await api.POST('/invites/{token}/accept', { params: { path: { token: 't' } }, body: {} })
    expect(fn).not.toHaveBeenCalled()
    await api.GET('/servers')
    expect(fn).toHaveBeenCalledOnce()
  })

  it('a 403 under a project calls the forbidden handler with its id', async () => {
    const fn = vi.fn()
    onForbidden(fn)
    const body = { error: { code: 'forbidden', message: 'no' } }
    server.use(
      http.get('*/v1/projects/5/tokens', () => json(403, body)),
      http.get('*/v1/projects/5', () => json(403, body)),
      http.get('*/v1/servers', () => json(403, body)),
    )
    await api.GET('/projects/{id}/tokens', { params: { path: { id: 5 } } })
    expect(fn).toHaveBeenCalledWith(5)
    fn.mockClear()
    await api.GET('/projects/{id}', { params: { path: { id: 5 } } })
    await api.GET('/servers')
    expect(fn).not.toHaveBeenCalled()
    onForbidden(() => {})
  })

  it('a project get 403 does not loop with a Project-ID header', async () => {
    const fn = vi.fn()
    onForbidden(fn)
    server.use(
      http.get('*/v1/projects/5', () => json(403, { error: { code: 'forbidden', message: 'no' } })),
    )
    setProjectSource(() => 5)
    await api.GET('/projects/{id}', { params: { path: { id: 5 } } })
    setProjectSource(null)
    expect(fn).not.toHaveBeenCalled()
    onForbidden(() => {})
  })

  it('a server-scoped 403 refreshes the role', async () => {
    const fn = vi.fn()
    onForbidden(fn)
    server.use(
      http.get('*/v1/servers/3', () => json(403, { error: { code: 'forbidden', message: 'no' } })),
    )
    setProjectSource(() => 7)
    await api.GET('/servers/{id}', { params: { path: { id: 3 } } })
    setProjectSource(null)
    expect(fn).toHaveBeenCalledWith(7)
    onForbidden(() => {})
  })

  it('passkey paths keep a 401 quiet', async () => {
    const fn = vi.fn()
    onUnauthorized(fn)
    const body = { error: { code: 'unauthorized', message: 'no' } }
    server.use(
      http.post('*/v1/auth/passkey/finish', () => json(401, body)),
      http.post('*/v1/me/passkeys/begin', () => json(401, body)),
    )
    await api.POST('/auth/passkey/finish', { body: { challenge_id: 'c1', credential: {} } })
    await api.POST('/me/passkeys/begin', { body: { password: 'pw' } })
    expect(fn).not.toHaveBeenCalled()
  })

  it('reportUnauthorized calls the handler', () => {
    const fn = vi.fn()
    onUnauthorized(fn)
    reportUnauthorized()
    expect(fn).toHaveBeenCalledOnce()
  })

  it('unwrap throws ApiError with code', async () => {
    server.use(
      http.get('*/v1/servers', () => json(409, { error: { code: 'conflict', message: 'x' } })),
    )
    const r = await api.GET('/servers')
    let err: unknown
    try {
      unwrap(r)
    } catch (e) {
      err = e
    }
    expect(err).toBeInstanceOf(ApiError)
    expect(err).toMatchObject({ status: 409, code: 'conflict', message: 'x' })
  })

  it('errorMessage maps pve_unreachable', () => {
    expect(errorMessage(new ApiError(502, 'pve_unreachable', 'dial tcp'))).toBe(
      "The server's host didn't respond. Try again in a moment.",
    )
    expect(errorMessage(new ApiError(503, 'service_unavailable', 'x'))).toBe(
      "Nebla's background jobs aren't running. Check the server logs.",
    )
    expect(errorMessage(new ApiError(409, 'conflict', 'name taken'))).toBe('name taken')
  })

  it('401 redirects once with next', async () => {
    const router = testRouter([
      { path: '/login', name: 'login', component: { render: () => null } },
    ])
    await router.push('/p/1/servers?x=1')
    const replace = vi.spyOn(router, 'replace')
    const cleared = vi.fn()
    redirectOnUnauthorized(router, cleared)
    const body = { error: { code: 'unauthorized', message: 'no' } }
    server.use(http.get('*/v1/servers', () => json(401, body)))
    await Promise.all([api.GET('/servers'), api.GET('/servers')])
    await Promise.all(replace.mock.results.map((r) => r.value))
    await api.GET('/servers')
    expect(replace).toHaveBeenCalledOnce()
    expect(replace).toHaveBeenCalledWith({ name: 'login', query: { next: '/p/1/servers?x=1' } })
    expect(router.currentRoute.value.query.next).toBe('/p/1/servers?x=1')
    expect(cleared).toHaveBeenCalledOnce()
  })
})

import { describe, expect, it, vi } from 'vitest'
import { http } from 'msw'
import { json, server } from '@/test/server'
import { api, onUnauthorized, setProjectId } from './client'
import { ApiError, errorMessage, unwrap } from './errors'

describe('api client', () => {
  it('adds Project-ID when set', async () => {
    const seen: (string | null)[] = []
    server.use(
      http.get('*/v1/servers', ({ request }) => {
        seen.push(request.headers.get('Project-ID'))
        return json(200, { data: [], meta: {} })
      }),
    )
    setProjectId(7)
    await api.GET('/servers')
    setProjectId(null)
    await api.GET('/servers')
    expect(seen).toEqual(['7', null])
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
      "Proxmox didn't respond. Try again in a moment.",
    )
    expect(errorMessage(new ApiError(503, 'service_unavailable', 'x'))).toBe(
      "Nebla's background jobs aren't running. Check the server logs.",
    )
    expect(errorMessage(new ApiError(409, 'conflict', 'name taken'))).toBe('name taken')
  })
})

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

import createClient, { type Client, type Middleware } from 'openapi-fetch'
import type { Router } from 'vue-router'
import { parseProjectId } from '@/lib/projectId'
import type { paths } from './schema'

let projectSource: (() => number | null) | null = null
let unauthorized: (() => void) | null = null
let forbidden: ((projectId: number) => void) | null = null
let loginRedirect = false

// Read on every request for callers that use the current project's scope.
export function setProjectSource(fn: (() => number | null) | null): void {
  projectSource = fn
}

export function onUnauthorized(fn: () => void): void {
  unauthorized = fn
}

export function onForbidden(fn: (projectId: number) => void): void {
  forbidden = fn
}

// Sends the first 401 to login with the current path; later ones are ignored until you leave login.
export function redirectOnUnauthorized(router: Router, onRedirect?: () => void): void {
  let redirecting = false
  router.afterEach((to, _from, failure) => {
    if (!failure && to.name !== 'login') redirecting = false
  })
  onUnauthorized(() => {
    const current = router.currentRoute.value
    if (redirecting || current.name === 'login') return
    redirecting = true
    loginRedirect = true
    onRedirect?.()
    void router
      .replace({ name: 'login', query: { next: current.fullPath } })
      .finally(() => (loginRedirect = false))
  })
}

export function redirectingToLogin(): boolean {
  return loginRedirect
}

// Paths where a 401 is an answer for the page, not an expired session.
const quiet401 = new Set<string>([
  '/auth/login',
  '/auth/logout',
  '/me',
  '/auth/totp/enroll',
  '/auth/totp/verify',
  '/auth/totp/disable',
  '/me/password',
  '/auth/passkey/finish',
  '/me/passkeys/begin',
  '/invites/{token}',
  '/invites/{token}/accept',
])

// Pages call this when a quiet path answers with code unauthorized.
export function reportUnauthorized(): void {
  unauthorized?.()
}

const middleware: Middleware = {
  onRequest({ request }) {
    const projectId = projectSource?.() ?? null
    if (projectId !== null && !request.headers.has('Project-ID'))
      request.headers.set('Project-ID', String(projectId))
    return request
  },
  onResponse({ request, response, schemaPath }) {
    if (response.status === 401 && !quiet401.has(schemaPath)) unauthorized?.()
    if (response.status === 403) {
      const header = parseProjectId(request.headers.get('Project-ID'))
      // The project query itself answers 403 for a lost project; refetching it would loop.
      const isProjectGet = schemaPath === '/projects/{id}' && request.method === 'GET'
      const pathId = schemaPath.startsWith('/projects/{id}')
        ? parseProjectId(new URL(request.url).pathname.match(/\/projects\/(\d+)/)?.[1])
        : null
      const id = pathId ?? header
      if (!isProjectGet && id !== null) forbidden?.(id)
    }
    return response
  },
}

// Look fetch up per call so test interceptors installed after import still apply.
export const api: Client<paths> = createClient<paths>({
  baseUrl: '/v1',
  credentials: 'same-origin',
  fetch: (req) => globalThis.fetch(req),
})
api.use(middleware)

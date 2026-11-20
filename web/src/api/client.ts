import createClient, { type Client, type Middleware } from 'openapi-fetch'
import type { Router } from 'vue-router'
import type { paths } from './schema'

let projectSource: (() => number | null) | null = null
let unauthorized: (() => void) | null = null
let loginRedirect = false

// Read on every request so the header always matches the page you're on.
export function setProjectSource(fn: (() => number | null) | null): void {
  projectSource = fn
}

export function onUnauthorized(fn: () => void): void {
  unauthorized = fn
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

const quiet401 = new Set(['/auth/login', '/auth/logout', '/me'])

const middleware: Middleware = {
  onRequest({ request }) {
    const projectId = projectSource?.() ?? null
    if (projectId !== null) request.headers.set('Project-ID', String(projectId))
    return request
  },
  onResponse({ response, schemaPath }) {
    if (response.status === 401 && !quiet401.has(schemaPath)) unauthorized?.()
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

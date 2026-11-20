import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { http } from 'msw'
import { json, server } from '@/test/server'
import { mountApp, testRouter } from '@/test/mount'
import { onUnauthorized } from '@/api/client'
import { resetSession, setMe, useSession } from '@/lib/session'
import AccountPage from './AccountPage.vue'

const user = { id: 1, email: 'kai@example.com', name: 'Kai', is_admin: false, totp_enabled: false }

let mounted: { unmount: () => void } | null = null
let calls: { method: string; path: string; body: unknown }[]

async function settle() {
  for (let i = 0; i < 4; i++) {
    await new Promise((r) => setTimeout(r, 10))
    await flushPromises()
  }
}

async function mountPage() {
  setMe(user)
  const router = testRouter([{ path: '/account', name: 'account', component: AccountPage }])
  await router.push('/account')
  mounted = mountApp(AccountPage, { router }).wrapper
  await settle()
  return router
}

function input(name: string): HTMLInputElement {
  return document.body.querySelector(`input[name="${name}"]`) as HTMLInputElement
}

async function fill(name: string, value: string) {
  const el = input(name)
  el.value = value
  el.dispatchEvent(new Event('input'))
  await flushPromises()
}

async function submitFormWith(name: string) {
  input(name).closest('form')!.dispatchEvent(new Event('submit', { cancelable: true }))
  await settle()
}

beforeEach(() => {
  resetSession()
  calls = []
  document.body.innerHTML = ''
  server.use(
    http.put('*/v1/me', async ({ request }) => {
      const body = (await request.json()) as { name: string }
      calls.push({ method: 'PUT', path: '/me', body })
      return json(200, { ...user, name: body.name })
    }),
  )
})

afterEach(() => {
  mounted?.unmount()
  mounted = null
  onUnauthorized(() => {})
})

describe('AccountPage', () => {
  it('saves name', async () => {
    await mountPage()
    await fill('name', 'Kai D')
    await submitFormWith('name')
    expect(calls[0]).toEqual({ method: 'PUT', path: '/me', body: { name: 'Kai D' } })
    expect(useSession().me.value?.name).toBe('Kai D')
  })

  it('email read-only', async () => {
    await mountPage()
    expect(document.body.textContent).toContain('kai@example.com')
    expect(input('email')).toBeNull()
  })

  it('password confirm must match', async () => {
    server.use(
      http.post('*/v1/me/password', () => {
        calls.push({ method: 'POST', path: '/me/password', body: null })
        return new Response(null, { status: 204 })
      }),
    )
    await mountPage()
    await fill('current_password', 'old-password-1')
    await fill('new_password', 'new-password-1')
    await fill('confirm_password', 'different-1')
    await submitFormWith('new_password')
    expect(calls).toHaveLength(0)
    expect(document.body.textContent).toContain("Passwords don't match")
  })

  it('changes password', async () => {
    server.use(
      http.post('*/v1/me/password', async ({ request }) => {
        calls.push({ method: 'POST', path: '/me/password', body: await request.json() })
        return new Response(null, { status: 204 })
      }),
    )
    await mountPage()
    await fill('current_password', 'old-password-1')
    await fill('new_password', 'new-password-1')
    await fill('confirm_password', 'new-password-1')
    await submitFormWith('new_password')
    expect(calls[0].body).toEqual({
      current_password: 'old-password-1',
      new_password: 'new-password-1',
    })
    expect(document.body.textContent).toContain(
      'Password changed. Your other sessions were signed out.',
    )
    expect(input('new_password').value).toBe('')
  })

  it('wrong current password stays on page', async () => {
    server.use(
      http.post('*/v1/me/password', () =>
        json(401, { error: { code: 'invalid_credentials', message: 'wrong password' } }),
      ),
    )
    const lost = vi.fn()
    onUnauthorized(lost)
    const router = await mountPage()
    await fill('current_password', 'bad')
    await fill('new_password', 'new-password-1')
    await fill('confirm_password', 'new-password-1')
    await submitFormWith('new_password')
    expect(document.body.textContent).toContain('wrong password')
    expect(router.currentRoute.value.name).toBe('account')
    expect(lost).not.toHaveBeenCalled()
    expect(useSession().me.value).toMatchObject({ email: 'kai@example.com' })
  })

  it('reports a lost session on save name', async () => {
    const lost = vi.fn()
    onUnauthorized(lost)
    server.use(
      http.put('*/v1/me', () => json(401, { error: { code: 'unauthorized', message: 'no' } })),
    )
    await mountPage()
    await fill('name', 'Kai D')
    await submitFormWith('name')
    expect(lost).toHaveBeenCalledTimes(1)
  })

  it('reports a lost session on password change', async () => {
    const lost = vi.fn()
    onUnauthorized(lost)
    server.use(
      http.post('*/v1/me/password', () =>
        json(401, { error: { code: 'unauthorized', message: 'no' } }),
      ),
    )
    await mountPage()
    await fill('current_password', 'old-password-1')
    await fill('new_password', 'new-password-1')
    await fill('confirm_password', 'new-password-1')
    await submitFormWith('new_password')
    expect(lost).toHaveBeenCalledTimes(1)
  })
})

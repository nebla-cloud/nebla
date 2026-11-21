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

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, type VueWrapper } from '@vue/test-utils'
import { http } from 'msw'
import { onUnauthorized } from '@/api/client'
import { json, server } from '@/test/server'
import { mountApp, testRouter } from '@/test/mount'
import { resetSession, useSession } from '@/lib/session'
import LoginPage from './LoginPage.vue'

const wa = vi.hoisted(() => ({
  startAuthentication: vi.fn(),
  autofill: vi.fn(),
  cancelCeremony: vi.fn(),
}))
vi.mock('@simplewebauthn/browser', () => ({
  browserSupportsWebAuthn: () => true,
  browserSupportsWebAuthnAutofill: () => wa.autofill(),
  startAuthentication: wa.startAuthentication,
  WebAuthnAbortService: { cancelCeremony: wa.cancelCeremony },
}))

const user = { id: 1, email: 'kai@example.com', name: 'Kai', is_admin: false, totp_enabled: true }

async function loadSession(signup = false, passkeyOrigin: string | null = location.origin) {
  server.use(
    http.get('*/v1/ui/manifest', () =>
      json(200, {
        setup_required: false,
        signup_enabled: signup,
        branding: { product_name: 'Nebla', logo_url: '', primary_color: '' },
        admin_sections: [],
        passkey_origin: passkeyOrigin,
      }),
    ),
    http.get('*/v1/me', () => json(401, { error: { code: 'unauthorized', message: 'no' } })),
  )
  await useSession().refresh()
}

beforeEach(() => {
  resetSession()
  document.body.innerHTML = ''
  wa.startAuthentication.mockReset()
  wa.cancelCeremony.mockReset()
  wa.autofill.mockReset().mockReturnValue(Promise.resolve(false))
})

const failedText = "That passkey didn't work. Try again, or log in with your password."
const optionsJSON = { challenge: 'abc', rpId: 'localhost' }
const credential = { id: 'cred', type: 'public-key' }

function passkeyHandlers(finishStatus = 200) {
  const calls = { begin: 0, finish: [] as unknown[] }
  server.use(
    http.post('*/v1/auth/passkey/begin', () => {
      calls.begin++
      return json(200, { challenge_id: 'ch1', options: optionsJSON })
    }),
    http.post('*/v1/auth/passkey/finish', async ({ request }) => {
      calls.finish.push(await request.json())
      if (finishStatus === 200) return json(200, user)
      return json(finishStatus, { error: { code: 'passkey_failed', message: 'nope' } })
    }),
  )
  return calls
}

async function mountLogin(next = '/p/4/servers') {
  const router = testRouter([{ path: '/login', name: 'login', component: LoginPage }])
  await router.push(`/login?next=${next}`)
  const { wrapper } = mountApp(LoginPage, { router })
  await flushPromises()
  return { router, wrapper }
}

const passkeyButton = (w: VueWrapper) =>
  w.findAll('button').find((b) => b.text() === 'Log in with a passkey')

describe('LoginPage', () => {
  it('asks for TOTP on totp_required', async () => {
    await loadSession()
    const bodies: Record<string, unknown>[] = []
    server.use(
      http.post('*/v1/auth/login', async ({ request }) => {
        const body = (await request.json()) as Record<string, unknown>
        bodies.push(body)
        if (!body.totp_code) {
          return json(401, { error: { code: 'totp_required', message: 'a TOTP code is required' } })
        }
        return json(200, user)
      }),
    )
    const router = testRouter([{ path: '/login', name: 'login', component: LoginPage }])
    await router.push('/login?next=/p/4/servers')
    const { wrapper } = mountApp(LoginPage, { router })
    await wrapper.find('input[name="email"]').setValue('kai@example.com')
    await wrapper.find('input[name="password"]').setValue('hunter2hunter2')
    expect(wrapper.find('input[name="totp_code"]').exists()).toBe(false)
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    await flushPromises()
    expect(wrapper.find('input[name="totp_code"]').exists()).toBe(true)

    await wrapper.find('input[name="totp_code"]').setValue('123456')
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    await flushPromises()
    expect(bodies).toHaveLength(2)
    expect(bodies[1]).toEqual({
      email: 'kai@example.com',
      password: 'hunter2hunter2',
      totp_code: '123456',
    })
    expect(useSession().me.value).toMatchObject({ email: 'kai@example.com' })
    expect(router.currentRoute.value.fullPath).toBe('/p/4/servers')
    wrapper.unmount()
  })

  it('signup link only when enabled', async () => {
    const router = testRouter([
      { path: '/login', name: 'login', component: LoginPage },
      { path: '/signup', name: 'signup', component: LoginPage },
    ])
    await router.push('/login')
    await loadSession(false)
    const off = mountApp(LoginPage, { router })
    expect(off.wrapper.text()).not.toContain('New here? Create an account')
    off.wrapper.unmount()
    resetSession()
    await loadSession(true)
    const on = mountApp(LoginPage, { router })
    expect(on.wrapper.text()).toContain('New here? Create an account')
  })
  describe('passkeys', () => {
    it('passkey button logs in', async () => {
      await loadSession()
      const calls = passkeyHandlers()
      wa.startAuthentication.mockResolvedValue(credential)
      const { router, wrapper } = await mountLogin()
      await passkeyButton(wrapper)!.trigger('click')
      await vi.waitFor(() => expect(calls.finish).toHaveLength(1))
      await flushPromises()
      expect(calls.begin).toBe(1)
      expect(wa.startAuthentication).toHaveBeenCalledWith({ optionsJSON })
      expect(calls.finish).toEqual([{ challenge_id: 'ch1', credential }])
      expect(useSession().me.value).toMatchObject({ email: 'kai@example.com' })
      expect(router.currentRoute.value.fullPath).toBe('/p/4/servers')
      wrapper.unmount()
    })

    it('cancel shows nothing', async () => {
      await loadSession()
      const calls = passkeyHandlers()
      wa.startAuthentication.mockRejectedValue(
        Object.assign(new Error('x'), { name: 'NotAllowedError' }),
      )
      const { wrapper } = await mountLogin()
      await passkeyButton(wrapper)!.trigger('click')
      await flushPromises()
      expect(wrapper.text()).not.toContain(failedText)
      expect(wrapper.text()).not.toContain('NotAllowedError')
      expect(calls.finish).toHaveLength(0)
      wrapper.unmount()
    })

    it('passkey_failed shows message', async () => {
      await loadSession()
      passkeyHandlers(401)
      const unauthorizedSpy = vi.fn()
      onUnauthorized(unauthorizedSpy)
      wa.startAuthentication.mockResolvedValue(credential)
      const { wrapper } = await mountLogin()
      await passkeyButton(wrapper)!.trigger('click')
      await vi.waitFor(() => expect(wrapper.text()).toContain(failedText))
      expect(unauthorizedSpy).not.toHaveBeenCalled()
      onUnauthorized(() => {})
      wrapper.unmount()
    })

    it('starts conditional on mount', async () => {
      await loadSession()
      const calls = passkeyHandlers()
      wa.autofill.mockReturnValue(Promise.resolve(true))
      wa.startAuthentication.mockReturnValue(new Promise(() => {}))
      const { wrapper } = await mountLogin()
      await vi.waitFor(() => expect(wa.startAuthentication).toHaveBeenCalled())
      expect(calls.begin).toBe(1)
      expect(wa.startAuthentication).toHaveBeenCalledWith({ optionsJSON, useBrowserAutofill: true })
      expect(wrapper.find('input[name="email"]').attributes('autocomplete')).toBe(
        'username webauthn',
      )
      wrapper.unmount()
    })

    it('aborts conditional on unmount', async () => {
      await loadSession()
      const calls = passkeyHandlers()
      wa.autofill.mockReturnValue(Promise.resolve(true))
      let reject!: (e: unknown) => void
      wa.startAuthentication.mockReturnValue(new Promise((_, r) => (reject = r)))
      const { wrapper } = await mountLogin()
      await vi.waitFor(() => expect(wa.startAuthentication).toHaveBeenCalled())
      wrapper.unmount()
      expect(wa.cancelCeremony).toHaveBeenCalled()
      reject(Object.assign(new Error('aborted'), { name: 'AbortError' }))
      await flushPromises()
      expect(calls.finish).toHaveLength(0)
    })

    it('conditional passkey_failed shows the alert', async () => {
      await loadSession()
      const calls = passkeyHandlers(401)
      wa.autofill.mockReturnValue(Promise.resolve(true))
      wa.startAuthentication
        .mockResolvedValueOnce(credential)
        .mockReturnValue(new Promise(() => {}))
      const { wrapper } = await mountLogin()
      await vi.waitFor(() => expect(wrapper.text()).toContain(failedText))
      await vi.waitFor(() => expect(wa.startAuthentication).toHaveBeenCalledTimes(2))
      expect(calls.begin).toBe(2)
      expect(wa.startAuthentication).toHaveBeenLastCalledWith({
        optionsJSON,
        useBrowserAutofill: true,
      })
      wrapper.unmount()
    })

    it('button cancel re-arms autofill', async () => {
      await loadSession()
      const calls = passkeyHandlers()
      wa.autofill.mockReturnValue(Promise.resolve(true))
      wa.startAuthentication.mockImplementation((opts: { useBrowserAutofill?: boolean }) =>
        opts.useBrowserAutofill
          ? new Promise(() => {})
          : Promise.reject(Object.assign(new Error('x'), { name: 'NotAllowedError' })),
      )
      const { wrapper } = await mountLogin()
      await vi.waitFor(() => expect(wa.startAuthentication).toHaveBeenCalledTimes(1))
      await passkeyButton(wrapper)!.trigger('click')
      await vi.waitFor(() => expect(wa.startAuthentication).toHaveBeenCalledTimes(3))
      const modes = wa.startAuthentication.mock.calls.map(
        ([o]) => (o as { useBrowserAutofill?: boolean }).useBrowserAutofill === true,
      )
      expect(modes).toEqual([true, false, true])
      expect(calls.begin).toBe(3)
      expect(wrapper.text()).not.toContain('NotAllowedError')
      wrapper.unmount()
    })

    it('stale challenge re-arms on focus', async () => {
      await loadSession()
      const calls = passkeyHandlers()
      wa.autofill.mockReturnValue(Promise.resolve(true))
      wa.startAuthentication.mockReturnValue(new Promise(() => {}))
      const { wrapper } = await mountLogin()
      await vi.waitFor(() => expect(wa.startAuthentication).toHaveBeenCalledTimes(1))
      window.dispatchEvent(new Event('focus'))
      document.dispatchEvent(new Event('visibilitychange'))
      await new Promise((r) => setTimeout(r, 50))
      await flushPromises()
      expect(calls.begin).toBe(1)

      const now = Date.now()
      const clock = vi.spyOn(Date, 'now').mockReturnValue(now + 4 * 60 * 1000 + 1000)
      window.dispatchEvent(new Event('focus'))
      await vi.waitFor(() => expect(wa.startAuthentication).toHaveBeenCalledTimes(2))
      expect(calls.begin).toBe(2)
      expect(wa.cancelCeremony).toHaveBeenCalledTimes(1)

      clock.mockReturnValue(now + 9 * 60 * 1000)
      document.dispatchEvent(new Event('visibilitychange'))
      await vi.waitFor(() => expect(wa.startAuthentication).toHaveBeenCalledTimes(3))
      expect(calls.begin).toBe(3)
      clock.mockRestore()
      wrapper.unmount()
    })

    it('no re-arm after unmount', async () => {
      await loadSession()
      const calls = passkeyHandlers()
      wa.autofill.mockReturnValue(Promise.resolve(true))
      let reject!: (e: unknown) => void
      wa.startAuthentication.mockReturnValue(new Promise((_, r) => (reject = r)))
      const { wrapper } = await mountLogin()
      await vi.waitFor(() => expect(wa.startAuthentication).toHaveBeenCalledTimes(1))
      wrapper.unmount()
      reject(Object.assign(new Error('aborted'), { name: 'AbortError' }))
      const clock = vi.spyOn(Date, 'now').mockReturnValue(Date.now() + 10 * 60 * 1000)
      window.dispatchEvent(new Event('focus'))
      document.dispatchEvent(new Event('visibilitychange'))
      await new Promise((r) => setTimeout(r, 50))
      await flushPromises()
      clock.mockRestore()
      expect(calls.begin).toBe(1)
      expect(wa.startAuthentication).toHaveBeenCalledTimes(1)
    })

    it('slow conditional begin leaves the button sheet alone', async () => {
      await loadSession()
      let release!: () => void
      const gate = new Promise<void>((r) => (release = r))
      let begins = 0
      server.use(
        http.post('*/v1/auth/passkey/begin', async () => {
          if (++begins === 1) await gate
          return json(200, { challenge_id: `ch${begins}`, options: optionsJSON })
        }),
      )
      wa.autofill.mockReturnValue(Promise.resolve(true))
      wa.startAuthentication.mockReturnValue(new Promise(() => {}))
      const { wrapper } = await mountLogin()
      await vi.waitFor(() => expect(begins).toBe(1))
      await passkeyButton(wrapper)!.trigger('click')
      await vi.waitFor(() => expect(wa.startAuthentication).toHaveBeenCalledTimes(1))
      release()
      await new Promise((r) => setTimeout(r, 50))
      await flushPromises()
      expect(wa.startAuthentication).toHaveBeenCalledTimes(1)
      expect(wa.startAuthentication).toHaveBeenCalledWith({ optionsJSON })
      wrapper.unmount()
    })

    it('conditional begin error is silent', async () => {
      await loadSession()
      server.use(
        http.post('*/v1/auth/passkey/begin', () =>
          json(429, { error: { code: 'rate_limited', message: 'slow down' } }),
        ),
      )
      wa.autofill.mockReturnValue(Promise.resolve(true))
      const { wrapper } = await mountLogin()
      await new Promise((r) => setTimeout(r, 50))
      await flushPromises()
      expect(wa.startAuthentication).not.toHaveBeenCalled()
      expect(wrapper.text()).not.toContain('slow down')
      wrapper.unmount()
    })

    it('unmount during begin starts no ceremony', async () => {
      await loadSession()
      let release!: () => void
      const gate = new Promise<void>((r) => (release = r))
      let hit = false
      server.use(
        http.post('*/v1/auth/passkey/begin', async () => {
          hit = true
          await gate
          return json(200, { challenge_id: 'ch1', options: optionsJSON })
        }),
      )
      wa.autofill.mockReturnValue(Promise.resolve(true))
      const { wrapper } = await mountLogin()
      await vi.waitFor(() => expect(hit).toBe(true))
      wrapper.unmount()
      release()
      await new Promise((r) => setTimeout(r, 50))
      expect(wa.startAuthentication).not.toHaveBeenCalled()
    })

    it('unmount during finish does not log in', async () => {
      await loadSession()
      let release!: () => void
      const gate = new Promise<void>((r) => (release = r))
      let hit = false
      server.use(
        http.post('*/v1/auth/passkey/begin', () =>
          json(200, { challenge_id: 'ch1', options: optionsJSON }),
        ),
        http.post('*/v1/auth/passkey/finish', async () => {
          hit = true
          await gate
          return json(200, user)
        }),
      )
      wa.autofill.mockReturnValue(Promise.resolve(true))
      wa.startAuthentication.mockResolvedValue(credential)
      const { router, wrapper } = await mountLogin()
      const replace = vi.spyOn(router, 'replace')
      await vi.waitFor(() => expect(hit).toBe(true))
      wrapper.unmount()
      release()
      await new Promise((r) => setTimeout(r, 50))
      await flushPromises()
      expect(useSession().me.value).toBeNull()
      expect(replace).not.toHaveBeenCalled()
    })

    it('button during a pending conditional ceremony completes', async () => {
      await loadSession()
      const calls = passkeyHandlers()
      wa.autofill.mockReturnValue(Promise.resolve(true))
      let rejectConditional!: (e: unknown) => void
      wa.startAuthentication.mockImplementation((opts: { useBrowserAutofill?: boolean }) =>
        opts.useBrowserAutofill
          ? new Promise((_, r) => (rejectConditional = r))
          : Promise.resolve(credential),
      )
      const { router, wrapper } = await mountLogin()
      await vi.waitFor(() => expect(wa.startAuthentication).toHaveBeenCalledTimes(1))
      await passkeyButton(wrapper)!.trigger('click')
      rejectConditional(Object.assign(new Error('aborted'), { name: 'AbortError' }))
      await vi.waitFor(() => expect(calls.finish).toHaveLength(1))
      await flushPromises()
      expect(wrapper.text()).not.toContain('aborted')
      expect(router.currentRoute.value.fullPath).toBe('/p/4/servers')
      wrapper.unmount()
    })

    it('hidden in totp step', async () => {
      await loadSession()
      passkeyHandlers()
      server.use(
        http.post('*/v1/auth/login', () =>
          json(401, { error: { code: 'totp_required', message: 'a TOTP code is required' } }),
        ),
      )
      const { wrapper } = await mountLogin()
      expect(passkeyButton(wrapper)).toBeTruthy()
      expect(wrapper.find('[data-passkey-divider]').exists()).toBe(true)
      await wrapper.find('input[name="email"]').setValue('kai@example.com')
      await wrapper.find('input[name="password"]').setValue('hunter2hunter2')
      await wrapper.find('form').trigger('submit')
      await flushPromises()
      await flushPromises()
      expect(wrapper.find('input[name="totp_code"]').exists()).toBe(true)
      expect(passkeyButton(wrapper)).toBeUndefined()
      expect(wrapper.find('[data-passkey-divider]').exists()).toBe(false)
      wrapper.unmount()
    })

    it('other origin shows no passkey', async () => {
      await loadSession(false, 'https://panel.example.com')
      const calls = passkeyHandlers()
      wa.autofill.mockReturnValue(Promise.resolve(true))
      const { wrapper } = await mountLogin()
      expect(passkeyButton(wrapper)).toBeUndefined()
      expect(calls.begin).toBe(0)
      expect(wrapper.find('input[name="email"]').attributes('autocomplete')).toBe('username')
      wrapper.unmount()
    })

    it('null origin shows no passkey', async () => {
      await loadSession(false, null)
      const calls = passkeyHandlers()
      const { wrapper } = await mountLogin()
      expect(passkeyButton(wrapper)).toBeUndefined()
      expect(calls.begin).toBe(0)
      wrapper.unmount()
    })
  })
})

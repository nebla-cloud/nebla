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
import { flushPromises } from '@vue/test-utils'
import { http } from 'msw'
import { browserSupportsWebAuthn, startRegistration } from '@simplewebauthn/browser'
import { json, server } from '@/test/server'
import { mountApp } from '@/test/mount'
import { onUnauthorized } from '@/api/client'
import { ensureSession, resetSession } from '@/lib/session'
import PasskeysSection from './PasskeysSection.vue'

vi.mock('@simplewebauthn/browser', () => ({
  browserSupportsWebAuthn: vi.fn(() => true),
  startRegistration: vi.fn(),
}))

const user = { id: 1, email: 'kai@example.com', name: 'Kai', is_admin: false, totp_enabled: false }

let mounted: { unmount: () => void } | null = null
let calls: { method: string; path: string; body: unknown }[]
let list: unknown[]

function pk(id: number, name: string, last: string | null = null) {
  return { id, name, created_at: '2026-11-12T10:00:00Z', last_used_at: last }
}

async function settle() {
  for (let i = 0; i < 4; i++) {
    await new Promise((r) => setTimeout(r, 10))
    await flushPromises()
  }
}

function button(text: string): HTMLButtonElement {
  const b = Array.from(document.body.querySelectorAll('button')).find((x) =>
    x.textContent?.includes(text),
  )
  if (!b) throw new Error(`button ${text} not found`)
  return b as HTMLButtonElement
}

function hasButton(text: string): boolean {
  return Array.from(document.body.querySelectorAll('button')).some((x) =>
    x.textContent?.includes(text),
  )
}

async function fill(name: string, value: string) {
  const el = document.body.querySelector(`input[name="${name}"]`) as HTMLInputElement
  el.value = value
  el.dispatchEvent(new Event('input'))
  await flushPromises()
}

async function submit() {
  document.body.querySelector('form')!.dispatchEvent(new Event('submit', { cancelable: true }))
  await settle()
}

async function openMenu(rowText: string) {
  const row = Array.from(document.body.querySelectorAll('li')).find((x) =>
    x.textContent?.includes(rowText),
  )!
  const trigger = row.querySelector('button[aria-label]') as HTMLButtonElement
  trigger.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
  await settle()
}

async function pickItem(text: string) {
  const item = Array.from(document.body.querySelectorAll('[role="menuitem"]')).find((x) =>
    x.textContent?.includes(text),
  ) as HTMLElement
  item.click()
  await settle()
}

function record(method: 'post' | 'put' | 'delete', path: string, status: number, body?: unknown) {
  return http[method](`*/v1${path}`, async ({ request }) => {
    calls.push({ method, path, body: await request.json().catch(() => null) })
    return status === 204 ? new Response(null, { status }) : json(status, body)
  })
}

async function mountSection(
  opts: { origin?: string | null; totp?: boolean; items?: unknown[] } = {},
) {
  const origin = opts.origin === undefined ? location.origin : opts.origin
  list = opts.items ?? []
  server.use(
    http.get('*/v1/ui/manifest', () =>
      json(200, {
        passkey_origin: origin,
        setup_required: false,
        signup_enabled: false,
        branding: { product_name: 'Nebla' },
      }),
    ),
    http.get('*/v1/me', () => json(200, { ...user, totp_enabled: opts.totp === true })),
    http.get('*/v1/me/passkeys', () => {
      calls.push({ method: 'get', path: '/me/passkeys', body: null })
      return json(200, { passkeys: list })
    }),
  )
  await ensureSession()
  mounted = mountApp(PasskeysSection).wrapper
  await settle()
}

async function openAdd() {
  button('Add a passkey').click()
  await settle()
}

const begun = { challenge_id: 'c1', options: { challenge: 'abc' } }

beforeEach(() => {
  resetSession()
  calls = []
  document.body.innerHTML = ''
  vi.mocked(browserSupportsWebAuthn).mockReturnValue(true)
  vi.mocked(startRegistration).mockReset()
})

afterEach(() => {
  mounted?.unmount()
  mounted = null
  onUnauthorized(() => {})
})

describe('PasskeysSection', () => {
  it('null origin', async () => {
    await mountSection({ origin: null })
    expect(document.body.textContent).toContain(
      'Passkeys need this panel on https with a domain name.',
    )
    expect(hasButton('Add a passkey')).toBe(false)
    expect(calls.filter((c) => c.path === '/me/passkeys')).toHaveLength(0)
  })

  it('other origin', async () => {
    await mountSection({ origin: 'https://panel.example.com' })
    expect(document.body.textContent).toContain('Passkeys work at https://panel.example.com.')
    const a = document.body.querySelector('a[href="https://panel.example.com"]')
    expect(a).not.toBeNull()
    expect(hasButton('Add a passkey')).toBe(false)
    expect(calls).toHaveLength(0)
  })

  it('unsupported browser', async () => {
    vi.mocked(browserSupportsWebAuthn).mockReturnValue(false)
    await mountSection()
    expect(document.body.textContent).toContain("This browser can't use passkeys.")
    expect(hasButton('Add a passkey')).toBe(false)
    expect(calls).toHaveLength(0)
  })

  it('empty state', async () => {
    await mountSection()
    expect(document.body.textContent).toContain("You haven't added a passkey.")
    expect(hasButton('Add a passkey')).toBe(true)
  })

  it('lists passkeys', async () => {
    await mountSection({ items: [pk(1, 'MacBook', '2026-11-18T10:00:00Z'), pk(2, 'YubiKey')] })
    const text = document.body.textContent ?? ''
    expect(text).toContain('MacBook')
    expect(text).toContain('YubiKey')
    expect(text).toContain('Added 12 Nov 2026.')
    expect(text).toContain('Last used 18 Nov 2026.')
    expect(text).toContain('Never used.')
  })

  it('limit reached', async () => {
    const items = Array.from({ length: 10 }, (_, i) => pk(i + 1, `Key ${i + 1}`))
    await mountSection({ items })
    expect(document.body.textContent).toContain("You've added the most passkeys allowed (10).")
    expect(hasButton('Add a passkey')).toBe(false)
  })

  it('add without totp', async () => {
    server.use(
      record('post', '/me/passkeys/begin', 200, begun),
      record('post', '/me/passkeys/finish', 201, pk(3, 'Passkey')),
    )
    vi.mocked(startRegistration).mockResolvedValue({ id: 'cred' } as never)
    await mountSection()
    await openAdd()
    expect(document.body.querySelector('input[name="code"]')).toBeNull()
    await fill('password', 'hunter2hunter2')
    list = [pk(3, 'Passkey')]
    await submit()
    expect(calls.find((c) => c.path === '/me/passkeys/begin')?.body).toEqual({
      password: 'hunter2hunter2',
    })
    expect(startRegistration).toHaveBeenCalledWith({ optionsJSON: begun.options })
    expect(calls.find((c) => c.path === '/me/passkeys/finish')?.body).toEqual({
      challenge_id: 'c1',
      credential: { id: 'cred' },
    })
    expect(document.body.querySelector('input[name="password"]')).toBeNull()
    expect(document.body.textContent).toContain('Passkey added')
    expect(calls.filter((c) => c.path === '/me/passkeys')).toHaveLength(2)
  })

  it('add with totp', async () => {
    server.use(
      record('post', '/me/passkeys/begin', 200, begun),
      record('post', '/me/passkeys/finish', 201, pk(3, 'Passkey')),
    )
    vi.mocked(startRegistration).mockResolvedValue({ id: 'cred' } as never)
    await mountSection({ totp: true })
    await openAdd()
    expect(document.body.textContent).toContain('Authentication code')
    await fill('password', 'pw')
    await fill('code', '123456')
    await submit()
    expect(calls.find((c) => c.path === '/me/passkeys/begin')?.body).toEqual({
      password: 'pw',
      totp_code: '123456',
    })
  })

  it('cancel keeps modal open', async () => {
    server.use(record('post', '/me/passkeys/begin', 200, begun))
    vi.mocked(startRegistration).mockRejectedValue(
      Object.assign(new Error('prompt dismissed'), { name: 'NotAllowedError' }),
    )
    await mountSection()
    await openAdd()
    await fill('password', 'pw')
    await submit()
    expect(document.body.querySelector('input[name="password"]')).not.toBeNull()
    expect(document.body.textContent).not.toContain('prompt dismissed')
    expect(calls.some((c) => c.path === '/me/passkeys/finish')).toBe(false)
  })

  it.each([
    [
      '/me/passkeys/begin',
      401,
      'invalid_credentials',
      'invalid email or password',
      "That password isn't right.",
    ],
    [
      '/me/passkeys/begin',
      401,
      'totp_required',
      'a TOTP code is required',
      'Enter the code from your authenticator app.',
    ],
    [
      '/me/passkeys/begin',
      401,
      'invalid_code',
      'invalid verification code',
      "That code isn't right.",
    ],
    [
      '/me/passkeys/begin',
      409,
      'passkey_limit',
      'you can have at most 10 passkeys',
      "You've added the most passkeys allowed (10).",
    ],
    [
      '/me/passkeys/finish',
      409,
      'passkey_exists',
      'that passkey is already added',
      'This passkey is already on your account.',
    ],
    [
      '/me/passkeys/finish',
      422,
      'passkey_invalid',
      'that request expired, try again',
      "That didn't work. Try adding the passkey again.",
    ],
    [
      '/me/passkeys/finish',
      422,
      'passkey_invalid',
      "that passkey couldn't be verified, try again",
      "That didn't work. Try adding the passkey again.",
    ],
  ] as const)('add error %s %s %s', async (path, status, code, raw, shown) => {
    server.use(
      path === '/me/passkeys/begin'
        ? record('post', path, status, { error: { code, message: raw } })
        : record('post', '/me/passkeys/begin', 200, begun),
      path === '/me/passkeys/finish'
        ? record('post', path, status, { error: { code, message: raw } })
        : record('post', '/me/passkeys/finish', 201, pk(3, 'Passkey')),
    )
    vi.mocked(startRegistration).mockResolvedValue({ id: 'cred' } as never)
    await mountSection()
    await openAdd()
    await fill('password', 'pw')
    await submit()
    expect(document.body.textContent).toContain(shown)
    expect(document.body.textContent).not.toContain(raw)
    expect(document.body.querySelector('input[name="password"]')).not.toBeNull()
  })

  it('add error other codes show the server message', async () => {
    server.use(
      record('post', '/me/passkeys/begin', 429, {
        error: { code: 'rate_limited', message: 'Too many attempts.' },
      }),
    )
    await mountSection()
    await openAdd()
    await fill('password', 'pw')
    await submit()
    expect(document.body.textContent).toContain('Too many attempts.')
  })

  it('authenticator already registered', async () => {
    server.use(record('post', '/me/passkeys/begin', 200, begun))
    vi.mocked(startRegistration).mockRejectedValue(
      Object.assign(new Error('The authenticator was previously registered'), {
        name: 'InvalidStateError',
        code: 'ERROR_AUTHENTICATOR_PREVIOUSLY_REGISTERED',
      }),
    )
    await mountSection()
    await openAdd()
    await fill('password', 'pw')
    await submit()
    expect(document.body.textContent).toContain('This passkey is already on your account.')
    expect(document.body.textContent).not.toContain('previously registered')
    expect(calls.some((c) => c.path === '/me/passkeys/finish')).toBe(false)
    expect(document.body.querySelector('input[name="password"]')).not.toBeNull()
  })

  it('list error offers retry', async () => {
    let fail = true
    server.use(
      http.get('*/v1/ui/manifest', () =>
        json(200, {
          passkey_origin: location.origin,
          setup_required: false,
          signup_enabled: false,
          branding: { product_name: 'Nebla' },
        }),
      ),
      http.get('*/v1/me', () => json(200, user)),
      http.get('*/v1/me/passkeys', () => {
        calls.push({ method: 'get', path: '/me/passkeys', body: null })
        return fail
          ? json(500, { error: { code: 'internal', message: 'boom' } })
          : json(200, { passkeys: [pk(1, 'MacBook')] })
      }),
    )
    await ensureSession()
    mounted = mountApp(PasskeysSection).wrapper
    await settle()
    expect(document.body.textContent).toContain("Couldn't load your passkeys. Try again.")
    expect(document.body.textContent).not.toContain('boom')
    expect(hasButton('Add a passkey')).toBe(false)
    fail = false
    button('Retry').click()
    await settle()
    expect(calls.filter((c) => c.path === '/me/passkeys')).toHaveLength(2)
    expect(document.body.textContent).not.toContain("Couldn't load your passkeys.")
    expect(document.body.textContent).toContain('MacBook')
  })

  it('unauthorized reports the lost session', async () => {
    const lost = vi.fn()
    onUnauthorized(lost)
    server.use(
      record('post', '/me/passkeys/begin', 401, {
        error: { code: 'unauthorized', message: 'log in' },
      }),
    )
    await mountSection()
    await openAdd()
    await fill('password', 'pw')
    await submit()
    expect(lost).toHaveBeenCalled()
    expect(document.body.textContent).not.toContain('log in')
  })

  it('rename', async () => {
    server.use(record('put', '/me/passkeys/:id', 200, pk(1, 'Work laptop')))
    await mountSection({ items: [pk(1, 'MacBook')] })
    await openMenu('MacBook')
    await pickItem('Rename')
    await fill('name', '  Work laptop  ')
    list = [pk(1, 'Work laptop')]
    await submit()
    expect(calls.find((c) => c.method === 'put')).toEqual({
      method: 'put',
      path: '/me/passkeys/:id',
      body: { name: 'Work laptop' },
    })
    expect(document.body.textContent).toContain('Work laptop')
    expect(document.body.textContent).not.toContain('MacBook')
  })

  it('remove', async () => {
    server.use(record('delete', '/me/passkeys/:id', 204))
    await mountSection({ items: [pk(1, 'MacBook')] })
    await openMenu('MacBook')
    await pickItem('Remove')
    expect(document.body.textContent).toContain(
      "Remove this passkey? You won't be able to log in with it, and you'll be signed out on your other devices.",
    )
    list = []
    button('Remove passkey').click()
    await settle()
    expect(calls.some((c) => c.method === 'delete')).toBe(true)
    expect(document.body.textContent).toContain('Passkey removed')
    expect(document.body.textContent).toContain("You haven't added a passkey.")
  })
})

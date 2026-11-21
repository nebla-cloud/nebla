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
import { json, server } from '@/test/server'
import { mountApp } from '@/test/mount'
import { onUnauthorized } from '@/api/client'
import { resetSession, setMe, useSession } from '@/lib/session'
import TwoFactorSection from './TwoFactorSection.vue'

const user = { id: 1, email: 'kai@example.com', name: 'Kai', is_admin: false, totp_enabled: false }

let mounted: { unmount: () => void } | null = null
let calls: { path: string; body: unknown }[]

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

function record(path: string, status: number, body?: unknown) {
  return http.post(`*/v1${path}`, async ({ request }) => {
    calls.push({ path, body: await request.json().catch(() => null) })
    return status === 204 ? new Response(null, { status }) : json(status, body)
  })
}

function mountSection(totp: boolean) {
  setMe({ ...user, totp_enabled: totp })
  mounted = mountApp(TwoFactorSection).wrapper
  return settle()
}

beforeEach(() => {
  resetSession()
  calls = []
  document.body.innerHTML = ''
})

afterEach(() => {
  mounted?.unmount()
  mounted = null
  onUnauthorized(() => {})
})

describe('TwoFactorSection', () => {
  it('enroll flow', async () => {
    server.use(
      record('/auth/totp/enroll', 200, {
        secret: 'JBSWY3DPEHPK3PXP',
        otpauth_url: 'otpauth://totp/Nebla:kai?secret=JBSWY3DPEHPK3PXP',
      }),
      record('/auth/totp/verify', 204),
    )
    await mountSection(false)
    await fill('password', 'hunter2hunter2')
    await submit()
    expect(calls[0]).toEqual({ path: '/auth/totp/enroll', body: { password: 'hunter2hunter2' } })
    expect(document.body.querySelector('[role="img"] svg')).not.toBeNull()
    const shown = document.body.querySelector('input[readonly]') as HTMLInputElement
    expect(shown.value).toBe('JBSW Y3DP EHPK 3PXP')

    const write = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', {
      value: { writeText: write },
      configurable: true,
    })
    button('Copy').click()
    await settle()
    expect(write).toHaveBeenCalledWith('JBSWY3DPEHPK3PXP')

    await fill('code', '123456')
    await submit()
    expect(calls[1]).toEqual({ path: '/auth/totp/verify', body: { code: '123456' } })
    expect(document.body.textContent).toContain('Two-factor login is on.')
    expect(useSession().me.value?.totp_enabled).toBe(true)
  })

  it('wrong code stays on step 2', async () => {
    server.use(
      record('/auth/totp/enroll', 200, { secret: 'JBSWY3DPEHPK3PXP', otpauth_url: 'otpauth://x' }),
      record('/auth/totp/verify', 401, { error: { code: 'invalid_code', message: 'wrong code' } }),
    )
    await mountSection(false)
    await fill('password', 'pw')
    await submit()
    await fill('code', '000000')
    await submit()
    expect(document.body.querySelector('input[name="code"]')).not.toBeNull()
    expect(document.body.textContent).toContain('wrong code')
    expect(useSession().me.value?.totp_enabled).toBe(false)
  })

  it('cancel returns to step 1', async () => {
    server.use(
      record('/auth/totp/enroll', 200, { secret: 'JBSWY3DPEHPK3PXP', otpauth_url: 'otpauth://x' }),
    )
    await mountSection(false)
    await fill('password', 'pw')
    await submit()
    button('Cancel').click()
    await settle()
    expect(document.body.querySelector('input[name="code"]')).toBeNull()
    expect(document.body.querySelector('input[name="password"]')).not.toBeNull()
  })

  it('already enabled refetches me', async () => {
    server.use(
      record('/auth/totp/enroll', 409, {
        error: { code: 'totp_already_enabled', message: 'already on' },
      }),
      http.get('*/v1/me', () => json(200, { ...user, totp_enabled: true })),
    )
    await mountSection(false)
    await fill('password', 'pw')
    await submit()
    expect(document.body.textContent).toContain('Two-factor login is on.')
    expect(useSession().me.value?.totp_enabled).toBe(true)
  })

  it('disable needs code and password', async () => {
    server.use(record('/auth/totp/disable', 204))
    await mountSection(true)
    await fill('code', '123456')
    await submit()
    expect(calls).toHaveLength(0)
    await fill('password', 'pw')
    await submit()
    expect(calls[0]).toEqual({
      path: '/auth/totp/disable',
      body: { code: '123456', password: 'pw' },
    })
    expect(useSession().me.value?.totp_enabled).toBe(false)
    expect(document.body.textContent).not.toContain('Two-factor login is on.')
  })

  it('unauthorized reports', async () => {
    const lost = vi.fn()
    onUnauthorized(lost)
    server.use(record('/auth/totp/enroll', 401, { error: { code: 'unauthorized', message: 'no' } }))
    await mountSection(false)
    await fill('password', 'pw')
    await submit()
    expect(lost).toHaveBeenCalledTimes(1)
  })
})

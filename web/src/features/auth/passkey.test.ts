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

import { describe, expect, it, vi } from 'vitest'
import { browserSupportsWebAuthn } from '@simplewebauthn/browser'

vi.mock('@simplewebauthn/browser', () => ({ browserSupportsWebAuthn: vi.fn(() => true) }))

import { isCancel, passkeyOriginElsewhere, passkeysUsable } from './passkey'

describe('passkey helpers', () => {
  it('usable on matching origin', () => {
    expect(passkeysUsable(location.origin)).toBe(true)
    expect(passkeyOriginElsewhere(location.origin)).toBe(false)
  })

  it('unusable without WebAuthn support', () => {
    vi.mocked(browserSupportsWebAuthn).mockReturnValueOnce(false)
    expect(passkeysUsable(location.origin)).toBe(false)
  })

  it('unusable on another origin', () => {
    expect(passkeysUsable('https://panel.example.com')).toBe(false)
    expect(passkeyOriginElsewhere('https://panel.example.com')).toBe(true)
  })

  it('unusable when null', () => {
    expect(passkeysUsable(null)).toBe(false)
    expect(passkeysUsable(undefined)).toBe(false)
    expect(passkeyOriginElsewhere(null)).toBe(false)
  })

  it('isCancel', () => {
    expect(isCancel(new DOMException('x', 'NotAllowedError'))).toBe(true)
    expect(isCancel(new DOMException('x', 'AbortError'))).toBe(true)
    expect(isCancel(new Error('x'))).toBe(false)
    expect(isCancel(null)).toBe(false)
  })
})

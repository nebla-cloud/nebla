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

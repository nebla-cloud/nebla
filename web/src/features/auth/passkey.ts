import { browserSupportsWebAuthn } from '@simplewebauthn/browser'

export function passkeysUsable(origin: string | null | undefined): boolean {
  return !!origin && origin === location.origin && browserSupportsWebAuthn()
}

export function passkeyOriginElsewhere(origin: string | null | undefined): boolean {
  return !!origin && origin !== location.origin
}

// The user dismissed the browser prompt or it timed out.
export function isCancel(e: unknown): boolean {
  const name = (e as { name?: unknown } | null)?.name
  return name === 'NotAllowedError' || name === 'AbortError'
}

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

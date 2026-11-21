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

import { me } from '@/lib/me'

export class ApiError extends Error {
  status: number
  code: string

  constructor(status: number, code: string, message: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
  }
}

const friendly: Record<string, string> = {
  pve_unreachable: "The server's host didn't respond. Try again in a moment.",
  service_unavailable: "Nebla's background jobs aren't running. Check the server logs.",
}

function isErrorBody(v: unknown): v is { error: { code: string; message: string } } {
  if (typeof v !== 'object' || v === null || !('error' in v)) return false
  const e = (v as { error: unknown }).error
  return typeof e === 'object' && e !== null && 'code' in e && 'message' in e
}

export function unwrap<T>(r: { data?: T; error?: unknown; response: Response }): T {
  if (r.error !== undefined || !r.response.ok) {
    if (isErrorBody(r.error)) {
      throw new ApiError(
        r.response.status,
        String(r.error.error.code),
        String(r.error.error.message),
      )
    }
    throw new ApiError(r.response.status, 'unknown', r.response.statusText || 'Request failed')
  }
  return r.data as T
}

const adminFriendly: Record<string, string> = {
  pve_unreachable:
    "Proxmox didn't respond. Check that the host is up and the location's API URL is right, then try again.",
}

export function errorMessage(e: unknown): string {
  if (e instanceof ApiError) {
    return (me.value?.is_admin ? adminFriendly[e.code] : undefined) ?? friendly[e.code] ?? e.message
  }
  if (e instanceof Error) return e.message
  return 'Something went wrong.'
}

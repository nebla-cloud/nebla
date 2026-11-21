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

import { shallowRef, type ShallowRef } from 'vue'
import { useRouter, type Router } from 'vue-router'
import { redirectingToLogin } from '@/api/client'
import { ApiError, errorMessage } from '@/api/errors'

export interface LoadFailure {
  message: string
  retry: () => Promise<unknown>
}

const RELOADED = 'nebla.chunkReload'
const failures = new WeakMap<Router, ShallowRef<LoadFailure | null>>()

export function isChunkError(e: unknown): boolean {
  return (
    e instanceof Error &&
    /dynamically imported module|Importing a module script failed|Unable to preload CSS/i.test(
      e.message,
    )
  )
}

// True only the first time, so a missing chunk can't reload forever; no storage means no reload.
function firstReload(): boolean {
  try {
    if (sessionStorage.getItem(RELOADED)) return false
    sessionStorage.setItem(RELOADED, '1')
    return true
  } catch {
    return false
  }
}

function clearReload(): void {
  try {
    sessionStorage.removeItem(RELOADED)
  } catch {
    // nothing to clear when storage is off
  }
}

export function handleLoadErrors(router: Router, reload: (url: string) => void): void {
  const failure = shallowRef<LoadFailure | null>(null)
  failures.set(router, failure)
  // vue-router reports errors from navigations a newer one replaced; only the latest counts.
  let latest: unknown = null
  router.beforeEach((to) => {
    latest = to
  })
  router.afterEach((_to, _from, navFailure) => {
    if (navFailure) return
    failure.value = null
    clearReload()
  })
  router.onError((e, to) => {
    if (to !== latest || redirectingToLogin()) return
    if (e instanceof ApiError && e.status === 401) return
    if (isChunkError(e) && firstReload()) return reload(router.resolve(to).href)
    failure.value = { message: errorMessage(e), retry: () => router.push(to.fullPath) }
  })
}

export function useLoadFailure(): ShallowRef<LoadFailure | null> {
  return failures.get(useRouter()) ?? shallowRef(null)
}

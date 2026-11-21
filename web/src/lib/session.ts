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

import { ref, type Ref } from 'vue'
import { api } from '@/api/client'
import { ApiError, unwrap } from '@/api/errors'
import type { UiManifest, User } from '@/api/types'
import { me } from './me'

const manifest = ref<UiManifest>()
let loading: Promise<void> | null = null

async function load(): Promise<void> {
  manifest.value = unwrap(await api.GET('/ui/manifest'))
  if (manifest.value.setup_required) {
    me.value = null
    return
  }
  const r = await api.GET('/me')
  if (r.response.status === 401) {
    me.value = null
    return
  }
  me.value = unwrap(r)
}

export function ensureSession(): Promise<void> {
  if (!loading) {
    loading = load().catch((e: unknown) => {
      loading = null
      throw e
    })
  }
  return loading
}

export function setMe(user: User | null): void {
  me.value = user
}

export function resetSession(): void {
  manifest.value = undefined
  me.value = undefined
  loading = null
}

export function useSession(): {
  manifest: Ref<UiManifest | undefined>
  me: Ref<User | null | undefined>
  refresh(): Promise<void>
  logout(): Promise<void>
} {
  return {
    manifest,
    me,
    refresh() {
      loading = null
      return ensureSession()
    },
    async logout() {
      try {
        const r = await api.POST('/auth/logout')
        if (r.response.status !== 401) unwrap(r)
      } catch (e) {
        if (!(e instanceof ApiError)) throw e
      }
      me.value = null
    },
  }
}

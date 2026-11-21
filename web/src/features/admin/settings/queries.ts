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

import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { api } from '@/api/client'
import { unwrap } from '@/api/errors'
import type { components } from '@/api/schema'
import { useSession } from '@/lib/session'

export type AdminSettings = components['schemas']['AdminSettings']

const key = ['admin', 'settings']

export function useAdminSettings() {
  return useQuery({
    queryKey: key,
    queryFn: async (): Promise<AdminSettings> => unwrap(await api.GET('/admin/settings')),
  })
}

export function useSaveSettings() {
  const qc = useQueryClient()
  const session = useSession()
  return useMutation({
    mutationFn: async (body: AdminSettings): Promise<AdminSettings> =>
      unwrap(await api.PUT('/admin/settings', { body })),
    onSuccess: async (data) => {
      qc.setQueryData(key, data)
      // The login page reads signup_enabled from the manifest; the save already worked.
      void session.refresh().catch(() => {})
    },
  })
}

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
import type { ServerType } from '@/api/types'

export type ServerTypeInput = components['schemas']['ServerTypeInput']

const key = ['admin', 'server-types']

export function useAdminServerTypes() {
  return useQuery({
    queryKey: key,
    queryFn: async (): Promise<ServerType[]> =>
      unwrap(await api.GET('/admin/server_types', { params: { query: { per_page: 50 } } }))
        .server_types,
  })
}

export function useSaveServerType() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (v: { id?: number; body: ServerTypeInput }): Promise<ServerType> =>
      v.id === undefined
        ? unwrap(await api.POST('/admin/server_types', { body: v.body }))
        : unwrap(
            await api.PUT('/admin/server_types/{id}', {
              params: { path: { id: v.id } },
              body: v.body,
            }),
          ),
    onSuccess: () => qc.invalidateQueries({ queryKey: key }),
  })
}

export function useDeleteServerType() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (id: number) =>
      unwrap(await api.DELETE('/admin/server_types/{id}', { params: { path: { id } } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: key }),
  })
}

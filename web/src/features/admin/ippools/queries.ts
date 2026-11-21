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

export type IpPool = components['schemas']['IpPool']
export type IpPoolInput = components['schemas']['IpPoolInput']

const key = ['admin', 'ip-pools']

export function useAdminIpPools() {
  return useQuery({
    queryKey: key,
    queryFn: async (): Promise<IpPool[]> =>
      unwrap(await api.GET('/admin/ip_pools', { params: { query: { per_page: 50 } } })).ip_pools,
  })
}

export function useSaveIpPool() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (v: { id?: number; body: IpPoolInput }): Promise<IpPool> =>
      v.id === undefined
        ? unwrap(await api.POST('/admin/ip_pools', { body: v.body }))
        : unwrap(
            await api.PUT('/admin/ip_pools/{id}', { params: { path: { id: v.id } }, body: v.body }),
          ),
    onSuccess: () => qc.invalidateQueries({ queryKey: key }),
  })
}

export function useDeleteIpPool() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (id: number) =>
      unwrap(await api.DELETE('/admin/ip_pools/{id}', { params: { path: { id } } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: key }),
  })
}

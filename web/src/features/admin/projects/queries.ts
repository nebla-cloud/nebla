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
import type { Ref } from 'vue'
import { api } from '@/api/client'
import { unwrap } from '@/api/errors'
import type { components } from '@/api/schema'

export type AdminProject = components['schemas']['AdminProject']
export type AdminProjectDetail = components['schemas']['AdminProjectDetail']
export type Quota = components['schemas']['Quota']
export const PER_PAGE = 50

export function useAdminProjects(page: Ref<number>) {
  return useQuery({
    queryKey: ['admin', 'projects', page],
    queryFn: async () =>
      unwrap(
        await api.GET('/admin/projects', {
          params: { query: { page: page.value, per_page: PER_PAGE } },
        }),
      ),
  })
}

export function useAdminProject(id: Ref<number>) {
  return useQuery({
    queryKey: ['admin', 'project', id],
    queryFn: async () =>
      unwrap(await api.GET('/admin/projects/{id}', { params: { path: { id: id.value } } })),
    retry: false,
  })
}

export function useSetQuota(id: Ref<number>) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (body: Quota) =>
      unwrap(
        await api.PUT('/admin/projects/{id}/quota', { params: { path: { id: id.value } }, body }),
      ),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ['admin', 'project', id.value] })
      await qc.invalidateQueries({ queryKey: ['admin', 'projects'] })
    },
  })
}

export function useDeleteProject() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (id: number) =>
      unwrap(await api.DELETE('/admin/projects/{id}', { params: { path: { id } } })),
    onSuccess: (_d, id) => {
      qc.removeQueries({ queryKey: ['admin', 'project', id] })
      return qc.invalidateQueries({ queryKey: ['admin', 'projects'] })
    },
  })
}

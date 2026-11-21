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

import { queryOptions, useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { api } from '@/api/client'
import { unwrap } from '@/api/errors'
import type { Project } from '@/api/types'

export const projectsQuery = () =>
  queryOptions({
    queryKey: ['projects'],
    queryFn: async (): Promise<Project[]> => unwrap(await api.GET('/projects')).projects,
  })

export const projectQuery = (id: number) =>
  queryOptions({
    queryKey: ['project', id],
    queryFn: async (): Promise<Project> =>
      unwrap(await api.GET('/projects/{id}', { params: { path: { id } } })),
  })

export function useProjects() {
  return useQuery(projectsQuery())
}

export function useCreateProject() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (name: string): Promise<Project> =>
      unwrap(await api.POST('/projects', { body: { name } })),
    onSuccess: (p) => {
      qc.setQueryData(['project', p.id], p)
      return qc.invalidateQueries({ queryKey: ['projects'] })
    },
  })
}

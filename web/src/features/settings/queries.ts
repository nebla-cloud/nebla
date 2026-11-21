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

import { useMutation, useQueryClient } from '@tanstack/vue-query'
import { api } from '@/api/client'
import { unwrap } from '@/api/errors'
import type { Project } from '@/api/types'
import { useCurrentProject } from '@/lib/project'

export function useRenameProject() {
  const qc = useQueryClient()
  const { projectId } = useCurrentProject()
  return useMutation({
    mutationFn: async (name: string): Promise<Project> =>
      unwrap(
        await api.PUT('/projects/{id}', {
          params: { path: { id: projectId.value } },
          body: { name },
        }),
      ),
    onSuccess: () =>
      Promise.all([
        qc.invalidateQueries({ queryKey: ['projects'] }),
        qc.invalidateQueries({ queryKey: ['project', projectId.value] }),
      ]),
  })
}

export function useLeaveProject() {
  const { projectId } = useCurrentProject()
  return useMutation({
    mutationFn: async (userId: number): Promise<void> => {
      unwrap(
        await api.DELETE('/projects/{id}/members/{user_id}', {
          params: { path: { id: projectId.value, user_id: userId } },
        }),
      )
    },
  })
}

export function useDeleteProject() {
  const { projectId } = useCurrentProject()
  return useMutation({
    mutationFn: async (): Promise<void> => {
      unwrap(await api.DELETE('/projects/{id}', { params: { path: { id: projectId.value } } }))
    },
  })
}

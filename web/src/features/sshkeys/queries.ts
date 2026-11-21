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

import { computed, type Ref } from 'vue'
import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { api } from '@/api/client'
import { unwrap } from '@/api/errors'
import type { components } from '@/api/schema'
import { useCurrentProject } from '@/lib/project'

export type SshKey = components['schemas']['SshKey']

export function useSshKeys(enabled?: Ref<boolean>) {
  const { projectId } = useCurrentProject()
  return useQuery({
    queryKey: computed(() => ['ssh-keys', projectId.value]),
    queryFn: async (): Promise<SshKey[]> =>
      unwrap(await api.GET('/ssh_keys', { params: { query: { per_page: 50 } } })).ssh_keys,
    enabled: computed(() => projectId.value > 0 && (enabled?.value ?? true)),
  })
}

export function useCreateSshKey() {
  const qc = useQueryClient()
  const { projectId } = useCurrentProject()
  return useMutation({
    mutationFn: async (body: { name: string; public_key: string }): Promise<SshKey> =>
      unwrap(await api.POST('/ssh_keys', { body })),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['ssh-keys', projectId.value] }),
  })
}

export function useRenameSshKey() {
  const qc = useQueryClient()
  const { projectId } = useCurrentProject()
  return useMutation({
    mutationFn: async (v: { id: number; name: string }): Promise<SshKey> =>
      unwrap(
        await api.PUT('/ssh_keys/{id}', {
          params: { path: { id: v.id } },
          body: { name: v.name },
        }),
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['ssh-keys', projectId.value] }),
  })
}

export function useDeleteSshKey() {
  const qc = useQueryClient()
  const { projectId } = useCurrentProject()
  return useMutation({
    mutationFn: async (id: number): Promise<void> => {
      unwrap(await api.DELETE('/ssh_keys/{id}', { params: { path: { id } } }))
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: ['ssh-keys', projectId.value] }),
  })
}

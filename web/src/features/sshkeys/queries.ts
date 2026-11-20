import { computed } from 'vue'
import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { api } from '@/api/client'
import { unwrap } from '@/api/errors'
import type { components } from '@/api/schema'
import { useCurrentProject } from '@/lib/project'

export type SshKey = components['schemas']['SshKey']

export function useSshKeys() {
  const { projectId } = useCurrentProject()
  return useQuery({
    queryKey: computed(() => ['ssh-keys', projectId.value]),
    queryFn: async (): Promise<SshKey[]> =>
      unwrap(await api.GET('/ssh_keys', { params: { query: { per_page: 50 } } })).ssh_keys,
    enabled: computed(() => projectId.value > 0),
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

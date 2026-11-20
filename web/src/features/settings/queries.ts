import { computed } from 'vue'
import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { api } from '@/api/client'
import { unwrap } from '@/api/errors'
import type { Member, Project } from '@/api/types'
import { useCurrentProject } from '@/lib/project'

export function useMembers() {
  const { projectId } = useCurrentProject()
  return useQuery({
    queryKey: computed(() => ['members', projectId.value]),
    queryFn: async (): Promise<Member[]> =>
      unwrap(await api.GET('/projects/{id}/members', { params: { path: { id: projectId.value } } }))
        .members,
    enabled: computed(() => projectId.value > 0),
  })
}

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

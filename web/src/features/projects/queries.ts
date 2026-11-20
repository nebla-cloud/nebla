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

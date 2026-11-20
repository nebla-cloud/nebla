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

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

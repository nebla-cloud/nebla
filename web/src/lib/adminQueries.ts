import { useQuery } from '@tanstack/vue-query'
import { api } from '@/api/client'
import { unwrap } from '@/api/errors'
import type { AdminLocation } from '@/api/types'

export function useAdminLocations() {
  return useQuery({
    queryKey: ['admin', 'locations'],
    queryFn: async (): Promise<AdminLocation[]> =>
      unwrap(await api.GET('/admin/locations', { params: { query: { per_page: 50 } } })).locations,
  })
}

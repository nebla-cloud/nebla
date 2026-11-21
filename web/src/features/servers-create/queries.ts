import { computed } from 'vue'
import { useQuery } from '@tanstack/vue-query'
import { api } from '@/api/client'
import { unwrap } from '@/api/errors'
import type { Location, ServerType } from '@/api/types'
import { useCurrentProject } from '@/lib/project'
import type { Image } from './createServer'

const PAGE = { params: { query: { per_page: 50 } } }

export const locationsKey = (projectId: number) => ['locations', projectId]
export const fetchLocations = async (): Promise<Location[]> =>
  unwrap(await api.GET('/locations', PAGE)).locations

export function useLocations() {
  const { projectId } = useCurrentProject()
  return useQuery({
    queryKey: computed(() => locationsKey(projectId.value)),
    queryFn: fetchLocations,
    enabled: computed(() => projectId.value > 0),
  })
}

export function useImages() {
  const { projectId } = useCurrentProject()
  return useQuery({
    queryKey: computed(() => ['images', projectId.value]),
    queryFn: async (): Promise<Image[]> => unwrap(await api.GET('/images', PAGE)).images,
    enabled: computed(() => projectId.value > 0),
  })
}

export function useServerTypes() {
  const { projectId } = useCurrentProject()
  return useQuery({
    queryKey: computed(() => ['server-types', projectId.value]),
    queryFn: async (): Promise<ServerType[]> =>
      unwrap(await api.GET('/server_types', PAGE)).server_types,
    enabled: computed(() => projectId.value > 0),
  })
}

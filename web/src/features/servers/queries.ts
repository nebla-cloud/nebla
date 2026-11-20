import { computed, type Ref } from 'vue'
import { useQuery } from '@tanstack/vue-query'
import { api } from '@/api/client'
import { unwrap } from '@/api/errors'
import type { Server } from '@/api/types'
import { useCurrentProject } from '@/lib/project'
import { isTransitional } from '@/lib/status'

const POLL_MS = 3000

export function useServers() {
  const { projectId } = useCurrentProject()
  return useQuery({
    queryKey: computed(() => ['servers', projectId.value]),
    queryFn: async (): Promise<Server[]> =>
      unwrap(await api.GET('/servers', { params: { query: { per_page: 50 } } })).servers,
    enabled: computed(() => projectId.value > 0),
    refetchInterval: (q) => (q.state.data?.some((s) => isTransitional(s.status)) ? POLL_MS : false),
  })
}

export function useServer(id: Ref<number>) {
  const { projectId } = useCurrentProject()
  return useQuery({
    queryKey: computed(() => ['server', id.value]),
    queryFn: async (): Promise<Server> =>
      unwrap(await api.GET('/servers/{id}', { params: { path: { id: id.value } } })),
    enabled: computed(() => projectId.value > 0 && id.value > 0),
    refetchInterval: (q) => (q.state.data && isTransitional(q.state.data.status) ? POLL_MS : false),
  })
}

import { computed, type Ref } from 'vue'
import { useQuery } from '@tanstack/vue-query'
import { api } from '@/api/client'
import { unwrap } from '@/api/errors'
import type { Server } from '@/api/types'
import { useCurrentProject } from '@/lib/project'
import { isTransitional } from '@/lib/status'

const POLL_MS = 3000

type ServerPage = { servers: Server[]; total: number }

function serverListQuery(selector: Ref<string> | undefined, enabled: Ref<boolean> | undefined) {
  const { projectId } = useCurrentProject()
  return {
    queryKey: computed(() => ['servers', projectId.value, selector?.value ?? '']),
    queryFn: async (): Promise<ServerPage> => {
      const label_selector = selector?.value.trim()
      const query = label_selector ? { per_page: 50, label_selector } : { per_page: 50 }
      const body = unwrap(await api.GET('/servers', { params: { query } }))
      return { servers: body.servers, total: body.meta.pagination.total_entries }
    },
    enabled: computed(() => projectId.value > 0 && (enabled?.value ?? true)),
    refetchInterval: (q: { state: { data?: ServerPage } }) =>
      q.state.data?.servers.some((s) => isTransitional(s.status)) ? POLL_MS : false,
  }
}

export function useServers(selector?: Ref<string>) {
  return useQuery({
    ...serverListQuery(selector, undefined),
    select: (d: ServerPage) => d.servers,
  })
}

// Names by id, and whether every server in the project was loaded.
export function useServerNames(enabled: Ref<boolean>) {
  return useQuery({
    ...serverListQuery(undefined, enabled),
    select: (d: ServerPage) => ({
      names: new Map(d.servers.map((s) => [s.id, s.name])),
      complete: d.total <= d.servers.length,
    }),
  })
}

export function useServer(id: Ref<number>, paused?: Ref<boolean>) {
  const { projectId } = useCurrentProject()
  return useQuery(
    computed(() => ({
      queryKey: ['server', projectId.value, id.value],
      queryFn: async (): Promise<Server> =>
        unwrap(await api.GET('/servers/{id}', { params: { path: { id: id.value } } })),
      enabled: projectId.value > 0 && id.value > 0,
      // A running action is polled instead; its end refetches the server.
      refetchInterval: paused?.value
        ? (false as const)
        : (q: { state: { data?: Server } }) =>
            q.state.data && isTransitional(q.state.data.status) ? POLL_MS : false,
    })),
  )
}

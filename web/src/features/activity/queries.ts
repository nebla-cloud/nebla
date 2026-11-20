import { computed, type Ref } from 'vue'
import { useInfiniteQuery } from '@tanstack/vue-query'
import { api } from '@/api/client'
import { unwrap } from '@/api/errors'
import type { Action } from '@/api/types'
import { useCurrentProject } from '@/lib/project'

const PER_PAGE = 25
export const POLL_MS = 3000

export type ActionFilter = 'all' | 'running' | 'error'

type Page = { actions: Action[]; page: number; last: boolean }

export function useActions(opts: { serverId?: Ref<number>; status?: Ref<ActionFilter> } = {}) {
  const { projectId } = useCurrentProject()
  const status = computed(() => opts.status?.value ?? 'all')
  return useInfiniteQuery({
    queryKey: computed(() => ['actions', projectId.value, opts.serverId?.value ?? 'all', status.value]),
    initialPageParam: 1,
    queryFn: async ({ pageParam }): Promise<Page> => {
      const query = {
        page: pageParam,
        per_page: PER_PAGE,
        sort: 'id:desc',
        ...(status.value === 'all' ? {} : { status: [status.value] }),
      }
      const body =
        opts.serverId === undefined
          ? unwrap(await api.GET('/actions', { params: { query } }))
          : unwrap(
              await api.GET('/servers/{id}/actions', {
                params: { path: { id: opts.serverId.value }, query },
              }),
            )
      const p = body.meta.pagination
      return { actions: body.actions, page: p.page, last: p.page * p.per_page >= p.total_entries }
    },
    getNextPageParam: (last) => (last.last ? undefined : last.page + 1),
    enabled: computed(() => projectId.value > 0 && (opts.serverId ? opts.serverId.value > 0 : true)),
    refetchInterval: (q) =>
      q.state.data?.pages.some((p) => p.actions.some((a) => a.status === 'running'))
        ? POLL_MS
        : false,
  })
}

import type { Ref } from 'vue'
import { useQuery } from '@tanstack/vue-query'
import { api } from '@/api/client'
import { ApiError, unwrap } from '@/api/errors'
import type { components } from '@/api/schema'
import { POLL_MS } from '@/features/activity/queries'

export type AdminAction = components['schemas']['AdminAction']
export type AdminStatus = 'running' | 'error' | 'success'
export const PER_PAGE = 50

export function useAdminActions(f: {
  page: Ref<number>
  projectId: Ref<number | null>
  status: Ref<AdminStatus | null>
}) {
  return useQuery({
    queryKey: ['admin', 'actions', f.page, f.projectId, f.status],
    queryFn: async () =>
      unwrap(
        await api.GET('/admin/actions', {
          params: {
            query: {
              page: f.page.value,
              per_page: PER_PAGE,
              sort: 'id:desc',
              ...(f.projectId.value ? { project_id: f.projectId.value } : {}),
              ...(f.status.value ? { status: [f.status.value] } : {}),
            },
          },
        }),
      ),
    refetchInterval: (q) =>
      q.state.data?.actions.some((a) => a.status === 'running') ? POLL_MS : false,
  })
}

export function useAdminAction(id: Ref<number>, enabled: Ref<boolean>) {
  return useQuery({
    queryKey: ['admin', 'action', id],
    queryFn: async (): Promise<AdminAction> =>
      unwrap(await api.GET('/admin/actions/{id}', { params: { path: { id: id.value } } })),
    enabled,
    staleTime: Infinity,
  })
}

export function unavailable(e: unknown): boolean {
  return e instanceof ApiError && (e.status === 403 || e.status === 404)
}

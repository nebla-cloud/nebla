import { computed, ref, watch, type ComputedRef, type Ref } from 'vue'
import { useQuery, useQueryClient } from '@tanstack/vue-query'
import { api } from '@/api/client'
import { ApiError, errorMessage, unwrap } from '@/api/errors'
import type { Action } from '@/api/types'
import { useCurrentProject } from './project'

const limited = (e: unknown) => e instanceof ApiError && e.status === 429

export function useActionPoll(
  actionId: Ref<number | null>,
  opts: { onDone?: (a: Action) => void; intervalMs?: number } = {},
): {
  action: Ref<Action | undefined>
  running: ComputedRef<boolean>
  error: ComputedRef<string | null>
} {
  const qc = useQueryClient()
  const { projectId } = useCurrentProject()
  const interval = opts.intervalMs ?? 2000
  const query = useQuery(
    computed(() => ({
      queryKey: ['action', actionId.value],
      queryFn: async (): Promise<Action> =>
        unwrap(await api.GET('/actions/{id}', { params: { path: { id: actionId.value! } } })),
      enabled: actionId.value !== null,
      staleTime: Infinity,
      refetchOnWindowFocus: false,
      retry: false,
      refetchInterval: (q: { state: { data?: Action; error: unknown } }) => {
        const e = q.state.error
        // A rate limit is transient, so back off and keep going.
        if (limited(e)) return interval * 5
        if (e instanceof ApiError && e.status >= 400 && e.status < 500) return false
        return q.state.data?.status === 'running' ? interval : false
      },
    })),
  )

  const action = ref<Action>()
  watch(
    () => [actionId.value, query.data.value] as const,
    ([id, a]) => {
      action.value = a && a.id === id ? a : undefined
    },
    { immediate: true },
  )

  const finished = new Set<number>()
  watch(
    () => action.value,
    async (a) => {
      if (!a || a.status !== 'success' || finished.has(a.id)) return
      finished.add(a.id)
      const invalidations = a.resources
        .filter((r) => r.type === 'server')
        .flatMap((r) => [
          qc.invalidateQueries({ queryKey: ['server', projectId.value, r.id] }),
          qc.invalidateQueries({ queryKey: ['servers', projectId.value] }),
        ])
      await Promise.all([
        ...invalidations,
        qc.invalidateQueries({ queryKey: ['actions', projectId.value] }),
      ])
      opts.onDone?.(a)
    },
  )

  return {
    action,
    running: computed(() => action.value?.status === 'running'),
    error: computed(() => {
      if (action.value?.status === 'error')
        return action.value.error?.message ?? 'The action failed.'
      const e = query.error.value
      return e && !limited(e) ? errorMessage(e) : null
    }),
  }
}

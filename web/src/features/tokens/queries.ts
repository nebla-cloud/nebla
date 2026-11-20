import { computed, type MaybeRefOrGetter, toValue } from 'vue'
import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { api } from '@/api/client'
import { unwrap } from '@/api/errors'
import type { ApiToken, TokenPermission } from '@/api/types'

export type CreatedToken = ApiToken & { token: string }

export function useTokens(projectId: MaybeRefOrGetter<number>) {
  return useQuery({
    queryKey: computed(() => ['tokens', toValue(projectId)]),
    queryFn: async (): Promise<ApiToken[]> =>
      unwrap(
        await api.GET('/projects/{id}/tokens', { params: { path: { id: toValue(projectId) } } }),
      ).tokens,
    enabled: computed(() => toValue(projectId) > 0),
  })
}

export function useCreateToken(projectId: MaybeRefOrGetter<number>) {
  const qc = useQueryClient()
  return useMutation({
    gcTime: 0,
    mutationFn: async (body: {
      name: string
      permission: TokenPermission
    }): Promise<CreatedToken> =>
      unwrap(
        await api.POST('/projects/{id}/tokens', {
          params: { path: { id: toValue(projectId) } },
          body,
        }),
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['tokens', toValue(projectId)] }),
  })
}

export function useRevokeToken(projectId: MaybeRefOrGetter<number>) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (tokenId: number): Promise<void> => {
      unwrap(
        await api.DELETE('/projects/{id}/tokens/{token_id}', {
          params: { path: { id: toValue(projectId), token_id: tokenId } },
        }),
      )
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: ['tokens', toValue(projectId)] }),
  })
}

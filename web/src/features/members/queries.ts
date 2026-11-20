import { computed, type MaybeRefOrGetter, toValue } from 'vue'
import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { api } from '@/api/client'
import { unwrap } from '@/api/errors'
import type { Invite, Member, Role } from '@/api/types'

export function useMembers(projectId: MaybeRefOrGetter<number>) {
  return useQuery({
    queryKey: computed(() => ['members', toValue(projectId)]),
    queryFn: async (): Promise<Member[]> =>
      unwrap(
        await api.GET('/projects/{id}/members', { params: { path: { id: toValue(projectId) } } }),
      ).members,
    enabled: computed(() => toValue(projectId) > 0),
  })
}

export function useInvites(
  projectId: MaybeRefOrGetter<number>,
  enabled: MaybeRefOrGetter<boolean>,
) {
  return useQuery({
    queryKey: computed(() => ['invites', toValue(projectId)]),
    queryFn: async (): Promise<Invite[]> =>
      unwrap(
        await api.GET('/projects/{id}/invites', { params: { path: { id: toValue(projectId) } } }),
      ).invites,
    enabled: computed(() => toValue(projectId) > 0 && toValue(enabled)),
  })
}

export function useSetRole(
  projectId: MaybeRefOrGetter<number>,
  myId: MaybeRefOrGetter<number | undefined>,
) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (v: { userId: number; role: Role }): Promise<Member> =>
      unwrap(
        await api.PUT('/projects/{id}/members/{user_id}', {
          params: { path: { id: toValue(projectId), user_id: v.userId } },
          body: { role: v.role },
        }),
      ),
    onSuccess: async (_d, v) => {
      const id = toValue(projectId)
      const keys = [
        ['members', id],
        ['invites', id],
      ]
      if (v.userId === toValue(myId)) keys.push(['project', id])
      await Promise.all(keys.map((queryKey) => qc.invalidateQueries({ queryKey })))
    },
  })
}

export function useRemoveMember(
  projectId: MaybeRefOrGetter<number>,
  myId: MaybeRefOrGetter<number | undefined>,
) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (userId: number): Promise<void> => {
      unwrap(
        await api.DELETE('/projects/{id}/members/{user_id}', {
          params: { path: { id: toValue(projectId), user_id: userId } },
        }),
      )
    },
    onSuccess: async (_d, userId) => {
      const id = toValue(projectId)
      const keys = [
        ['members', id],
        ['invites', id],
      ]
      if (userId === toValue(myId)) keys.push(['project', id])
      await Promise.all(keys.map((queryKey) => qc.invalidateQueries({ queryKey })))
    },
  })
}

export function useCreateInvite(projectId: MaybeRefOrGetter<number>) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (body: { email: string; role: Role }): Promise<string> =>
      unwrap(
        await api.POST('/projects/{id}/invites', {
          params: { path: { id: toValue(projectId) } },
          body,
        }),
      ).invite_url,
    onSuccess: () => qc.invalidateQueries({ queryKey: ['invites', toValue(projectId)] }),
  })
}

export function useRevokeInvite(projectId: MaybeRefOrGetter<number>) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (inviteId: number): Promise<void> => {
      unwrap(
        await api.DELETE('/projects/{id}/invites/{invite_id}', {
          params: { path: { id: toValue(projectId), invite_id: inviteId } },
        }),
      )
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: ['invites', toValue(projectId)] }),
  })
}

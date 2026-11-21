// Copyright (C) 2026 The Nebla contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

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

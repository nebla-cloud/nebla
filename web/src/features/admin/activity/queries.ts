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

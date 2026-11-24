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

import { computed, type Ref } from 'vue'
import { useQuery, useQueryClient } from '@tanstack/vue-query'
import { api } from '@/api/client'
import { unwrap } from '@/api/errors'
import type { components } from '@/api/schema'
import type { Action, Snapshot } from '@/api/types'
import { useCurrentProject } from '@/lib/project'
import { isSnapshotTransitional } from '@/lib/status'

const POLL_MS = 3000

export type SnapshotInput = components['schemas']['SnapshotInput']
export type SnapshotUpdate = components['schemas']['SnapshotUpdate']

const poll =
  (paused?: Ref<boolean>) =>
  (q: { state: { data?: Snapshot[] | Snapshot } }): number | false => {
    if (paused?.value) return false
    const d = q.state.data
    const list = d === undefined ? [] : Array.isArray(d) ? d : [d]
    return list.some((s) => isSnapshotTransitional(s.status)) ? POLL_MS : false
  }

const PER_PAGE = 50

// fetchSnapshots reads every page of the project's snapshots, or of those of one server.
export async function fetchSnapshots(
  project: number,
  server?: number,
  signal?: AbortSignal,
): Promise<Snapshot[]> {
  const out: Snapshot[] = []
  for (let page = 1; ; page++) {
    const query = server
      ? { page, per_page: PER_PAGE, server: String(server) }
      : { page, per_page: PER_PAGE }
    const body = unwrap(
      await api.GET('/snapshots', {
        headers: { 'Project-ID': String(project) },
        params: { query },
        signal,
      }),
    )
    out.push(...body.snapshots)
    if (body.snapshots.length < PER_PAGE || out.length >= body.meta.pagination.total_entries)
      return out
  }
}

// The project's snapshots, or only those of one server.
export function useSnapshots(
  serverId?: Ref<number>,
  paused?: Ref<boolean>,
  enabled?: Ref<boolean>,
) {
  const { projectId } = useCurrentProject()
  return useQuery(
    computed(() => {
      const project = projectId.value
      const server = serverId?.value ?? 0
      return {
        queryKey: ['snapshots', project, server],
        queryFn: ({ signal }: { signal: AbortSignal }) => fetchSnapshots(project, server, signal),
        enabled: project > 0 && (!serverId || server > 0) && (enabled?.value ?? true),
        refetchInterval: poll(paused),
      }
    }),
  )
}

export function useSnapshot(id: Ref<number>, paused?: Ref<boolean>) {
  const { projectId } = useCurrentProject()
  return useQuery(
    computed(() => ({
      queryKey: ['snapshot', projectId.value, id.value],
      queryFn: async (): Promise<Snapshot> =>
        unwrap(await api.GET('/snapshots/{id}', { params: { path: { id: id.value } } })),
      enabled: projectId.value > 0 && id.value > 0,
      refetchInterval: poll(paused),
    })),
  )
}

// Refreshes everything a snapshot change can touch.
export function useRefreshSnapshots() {
  const qc = useQueryClient()
  const { projectId } = useCurrentProject()
  return () =>
    Promise.all(
      ['snapshots', 'snapshot', 'servers', 'server', 'actions', 'project'].map((k) =>
        qc.invalidateQueries({ queryKey: [k, projectId.value] }),
      ),
    )
}

const path = (id: number) => ({ params: { path: { id } } })

export async function takeSnapshot(
  body: SnapshotInput,
): Promise<{ snapshot: Snapshot; action: Action }> {
  return unwrap(await api.POST('/snapshots', { body }))
}

export async function renameSnapshot(id: number, name: string): Promise<Snapshot> {
  return unwrap(await api.PUT('/snapshots/{id}', { ...path(id), body: { name } }))
}

export async function deleteSnapshot(id: number): Promise<Action> {
  return unwrap(await api.DELETE('/snapshots/{id}', path(id))).action
}

// Restoring rebuilds the source server from the snapshot.
export async function restoreSnapshot(snapshot: Snapshot): Promise<Action> {
  if (snapshot.server === null) throw new Error('The source server is gone.')
  return unwrap(
    await api.POST('/servers/{id}/actions/rebuild', {
      ...path(snapshot.server),
      body: { snapshot: String(snapshot.id) },
    }),
  ).action
}

export const snapshotDone: Record<string, string> = {
  create_snapshot: 'saved',
  delete_snapshot: 'deleted',
  rebuild_server: 'restored',
}

export function useSnapshotActions(id: Ref<number>, paused?: Ref<boolean>) {
  const { projectId } = useCurrentProject()
  return useQuery(
    computed(() => ({
      queryKey: ['actions', projectId.value, 'snapshot', id.value],
      queryFn: async (): Promise<Action[]> =>
        unwrap(
          await api.GET('/snapshots/{id}/actions', {
            params: { path: { id: id.value }, query: { per_page: 25, sort: 'id:desc' } },
          }),
        ).actions,
      enabled: projectId.value > 0 && id.value > 0,
      refetchInterval: (q: { state: { data?: Action[] } }) =>
        !paused?.value && q.state.data?.some((a) => a.status === 'running') ? POLL_MS : false,
    })),
  )
}

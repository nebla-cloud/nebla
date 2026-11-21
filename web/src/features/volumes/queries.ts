import { computed, type Ref } from 'vue'
import { useQuery, useQueryClient } from '@tanstack/vue-query'
import { api } from '@/api/client'
import { unwrap } from '@/api/errors'
import type { components } from '@/api/schema'
import type { Action, Volume } from '@/api/types'
import { useCurrentProject } from '@/lib/project'
import { isVolumeTransitional } from '@/lib/status'

const POLL_MS = 3000

export type VolumeInput = components['schemas']['VolumeInput']

const poll =
  (paused?: Ref<boolean>) =>
  (q: { state: { data?: Volume[] | Volume } }): number | false => {
    if (paused?.value) return false
    const d = q.state.data
    const list = d === undefined ? [] : Array.isArray(d) ? d : [d]
    return list.some((v) => isVolumeTransitional(v.status)) ? POLL_MS : false
  }

const PER_PAGE = 50

// fetchVolumes reads every page of the project's volumes, or of those on one server.
export async function fetchVolumes(
  project: number,
  server?: number,
  signal?: AbortSignal,
): Promise<Volume[]> {
  const out: Volume[] = []
  for (let page = 1; ; page++) {
    const query = server
      ? { page, per_page: PER_PAGE, server: String(server) }
      : { page, per_page: PER_PAGE }
    const body = unwrap(
      await api.GET('/volumes', {
        headers: { 'Project-ID': String(project) },
        params: { query },
        signal,
      }),
    )
    out.push(...body.volumes)
    if (body.volumes.length < PER_PAGE || out.length >= body.meta.pagination.total_entries)
      return out
  }
}

// The project's volumes, or only those on one server.
export function useVolumes(serverId?: Ref<number>, paused?: Ref<boolean>) {
  const { projectId } = useCurrentProject()
  return useQuery(
    computed(() => {
      const project = projectId.value
      const server = serverId?.value ?? 0
      return {
        queryKey: ['volumes', project, server],
        queryFn: ({ signal }: { signal: AbortSignal }) => fetchVolumes(project, server, signal),
        enabled: project > 0 && (!serverId || server > 0),
        refetchInterval: poll(paused),
      }
    }),
  )
}

export function useVolume(id: Ref<number>, paused?: Ref<boolean>) {
  const { projectId } = useCurrentProject()
  return useQuery(
    computed(() => ({
      queryKey: ['volume', projectId.value, id.value],
      queryFn: async (): Promise<Volume> =>
        unwrap(await api.GET('/volumes/{id}', { params: { path: { id: id.value } } })),
      enabled: projectId.value > 0 && id.value > 0,
      refetchInterval: poll(paused),
    })),
  )
}

// Refreshes everything a volume change can touch.
export function useRefreshVolumes() {
  const qc = useQueryClient()
  const { projectId } = useCurrentProject()
  return () =>
    Promise.all(
      ['volumes', 'volume', 'servers', 'server', 'actions', 'project'].map((k) =>
        qc.invalidateQueries({ queryKey: [k, projectId.value] }),
      ),
    )
}

const path = (id: number) => ({ params: { path: { id } } })

export async function createVolume(body: VolumeInput): Promise<{ volume: Volume; action: Action }> {
  return unwrap(await api.POST('/volumes', { body }))
}

export async function attachVolume(id: number, server: number): Promise<Action> {
  return unwrap(
    await api.POST('/volumes/{id}/actions/attach', {
      ...path(id),
      body: { server: String(server) },
    }),
  ).action
}

export async function detachVolume(id: number): Promise<Action> {
  return unwrap(await api.POST('/volumes/{id}/actions/detach', path(id))).action
}

export async function resizeVolume(id: number, size: number): Promise<Action> {
  return unwrap(await api.POST('/volumes/{id}/actions/resize', { ...path(id), body: { size } }))
    .action
}

export async function deleteVolume(id: number): Promise<Action> {
  return unwrap(await api.DELETE('/volumes/{id}', path(id))).action
}

export const volumeDone: Record<string, string> = {
  create_volume: 'created',
  attach_volume: 'attached',
  detach_volume: 'detached',
  resize_volume: 'resized',
  delete_volume: 'deleted',
}

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
import type { Action, FloatingIp } from '@/api/types'
import { useCurrentProject } from '@/lib/project'
import { isFloatingIpTransitional } from '@/lib/status'

const POLL_MS = 3000
const PER_PAGE = 50

export type FloatingIPInput = components['schemas']['FloatingIpInput']
export type FloatingIPUpdate = components['schemas']['FloatingIpUpdate']

const poll =
  (paused?: Ref<boolean>) =>
  (q: { state: { data?: FloatingIp[] | FloatingIp } }): number | false => {
    if (paused?.value) return false
    const d = q.state.data
    const list = d === undefined ? [] : Array.isArray(d) ? d : [d]
    return list.some((f) => isFloatingIpTransitional(f.status)) ? POLL_MS : false
  }

// fetchFloatingIPs reads every page of the project's floating IPs.
export async function fetchFloatingIPs(
  project: number,
  signal?: AbortSignal,
): Promise<FloatingIp[]> {
  const out: FloatingIp[] = []
  for (let page = 1; ; page++) {
    const body = unwrap(
      await api.GET('/floating_ips', {
        headers: { 'Project-ID': String(project) },
        params: { query: { page, per_page: PER_PAGE } },
        signal,
      }),
    )
    out.push(...body.floating_ips)
    if (body.floating_ips.length < PER_PAGE || out.length >= body.meta.pagination.total_entries)
      return out
  }
}

export function useFloatingIPs() {
  const { projectId } = useCurrentProject()
  return useQuery(
    computed(() => {
      const project = projectId.value
      return {
        queryKey: ['floating-ips', project],
        queryFn: ({ signal }: { signal: AbortSignal }) => fetchFloatingIPs(project, signal),
        enabled: project > 0,
        refetchInterval: poll(),
      }
    }),
  )
}

export function useFloatingIP(id: Ref<number>, paused?: Ref<boolean>) {
  const { projectId } = useCurrentProject()
  return useQuery(
    computed(() => ({
      queryKey: ['floating-ip', projectId.value, id.value],
      queryFn: async (): Promise<FloatingIp> =>
        unwrap(await api.GET('/floating_ips/{id}', { params: { path: { id: id.value } } })),
      enabled: projectId.value > 0 && id.value > 0,
      refetchInterval: poll(paused),
    })),
  )
}

// Refreshes everything a floating IP change can touch.
export function useRefreshFloatingIPs() {
  const qc = useQueryClient()
  const { projectId } = useCurrentProject()
  return () =>
    Promise.all(
      ['floating-ips', 'floating-ip', 'servers', 'server', 'actions', 'project'].map((k) =>
        qc.invalidateQueries({ queryKey: [k, projectId.value] }),
      ),
    )
}

const path = (id: number) => ({ params: { path: { id } } })

export async function createFloatingIP(
  body: FloatingIPInput,
): Promise<{ floating_ip: FloatingIp; action: Action | null }> {
  return unwrap(await api.POST('/floating_ips', { body }))
}

export async function updateFloatingIP(id: number, body: FloatingIPUpdate): Promise<FloatingIp> {
  return unwrap(await api.PUT('/floating_ips/{id}', { ...path(id), body }))
}

export async function assignFloatingIP(id: number, server: number): Promise<Action> {
  return unwrap(
    await api.POST('/floating_ips/{id}/actions/assign', {
      ...path(id),
      body: { server: String(server) },
    }),
  ).action
}

export async function unassignFloatingIP(id: number): Promise<Action> {
  return unwrap(await api.POST('/floating_ips/{id}/actions/unassign', path(id))).action
}

export async function deleteFloatingIP(id: number): Promise<Action> {
  return unwrap(await api.DELETE('/floating_ips/{id}', path(id))).action
}

export const floatingIPDone: Record<string, string> = {
  assign_floating_ip: 'assigned',
  unassign_floating_ip: 'unassigned',
  delete_floating_ip: 'deleted',
}

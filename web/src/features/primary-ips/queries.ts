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
import type { Action, PrimaryIp } from '@/api/types'
import { useCurrentProject } from '@/lib/project'
import { isPrimaryIpTransitional } from '@/lib/status'

const POLL_MS = 3000
const PER_PAGE = 50

export type PrimaryIPInput = components['schemas']['PrimaryIpInput']
export type PrimaryIPUpdate = components['schemas']['PrimaryIpUpdate']
export type PrimaryIPActionResult = { action: Action; warnings: string[] }

// Panel copy.
export const TAKES_EFFECT = 'The new address takes effect when the server starts.'
export const STOP_FIRST = 'Stop the server first.'
export const OLD_SERVER =
  'This server was created before IP changes were supported. Changing its IP re-runs its cloud-init user data and gives it new SSH host keys.'
export const KEPT_NOTE = 'Kept IPs stay unassigned and count toward quota.'
export const noPublic = (type: PrimaryIp['type']) =>
  `This server will have no public ${type === 'ipv4' ? 'IPv4' : 'IPv6'}.`

const poll =
  (paused?: Ref<boolean>) =>
  (q: { state: { data?: PrimaryIp[] | PrimaryIp } }): number | false => {
    if (paused?.value) return false
    const d = q.state.data
    const list = d === undefined ? [] : Array.isArray(d) ? d : [d]
    return list.some((p) => isPrimaryIpTransitional(p.status)) ? POLL_MS : false
  }

// fetchPrimaryIPs reads every page of the project's primary IPs.
export async function fetchPrimaryIPs(project: number, signal?: AbortSignal): Promise<PrimaryIp[]> {
  const out: PrimaryIp[] = []
  for (let page = 1; ; page++) {
    const body = unwrap(
      await api.GET('/primary_ips', {
        headers: { 'Project-ID': String(project) },
        params: { query: { page, per_page: PER_PAGE } },
        signal,
      }),
    )
    out.push(...body.primary_ips)
    if (body.primary_ips.length < PER_PAGE || out.length >= body.meta.pagination.total_entries)
      return out
  }
}

export function usePrimaryIPs() {
  const { projectId } = useCurrentProject()
  return useQuery(
    computed(() => {
      const project = projectId.value
      return {
        queryKey: ['primary-ips', project],
        queryFn: ({ signal }: { signal: AbortSignal }) => fetchPrimaryIPs(project, signal),
        enabled: project > 0,
        refetchInterval: poll(),
      }
    }),
  )
}

export function usePrimaryIP(id: Ref<number>, paused?: Ref<boolean>) {
  const { projectId } = useCurrentProject()
  return useQuery(
    computed(() => ({
      queryKey: ['primary-ip', projectId.value, id.value],
      queryFn: async (): Promise<PrimaryIp> =>
        unwrap(await api.GET('/primary_ips/{id}', { params: { path: { id: id.value } } })),
      enabled: projectId.value > 0 && id.value > 0,
      refetchInterval: poll(paused),
    })),
  )
}

// Refreshes everything a primary IP change can touch.
export function useRefreshPrimaryIPs() {
  const qc = useQueryClient()
  const { projectId } = useCurrentProject()
  return () =>
    Promise.all(
      ['primary-ips', 'primary-ip', 'servers', 'server', 'actions', 'project'].map((k) =>
        qc.invalidateQueries({ queryKey: [k, projectId.value] }),
      ),
    )
}

const path = (id: number) => ({ params: { path: { id } } })

export async function createPrimaryIP(body: PrimaryIPInput): Promise<PrimaryIp> {
  return unwrap(await api.POST('/primary_ips', { body })).primary_ip
}

export async function updatePrimaryIP(id: number, body: PrimaryIPUpdate): Promise<PrimaryIp> {
  return unwrap(await api.PUT('/primary_ips/{id}', { ...path(id), body }))
}

export async function assignPrimaryIP(id: number, server: number): Promise<PrimaryIPActionResult> {
  return unwrap(
    await api.POST('/primary_ips/{id}/actions/assign', {
      ...path(id),
      body: { server: String(server) },
    }),
  )
}

export async function unassignPrimaryIP(id: number): Promise<PrimaryIPActionResult> {
  return unwrap(await api.POST('/primary_ips/{id}/actions/unassign', path(id)))
}

export async function deletePrimaryIP(id: number): Promise<void> {
  unwrap(await api.DELETE('/primary_ips/{id}', path(id)))
}

export const primaryIPDone: Record<string, string> = {
  assign_primary_ip: 'assigned',
  unassign_primary_ip: 'unassigned',
}

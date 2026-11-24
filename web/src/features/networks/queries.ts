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
import type { Action, Network, NetworkGuestConfig } from '@/api/types'
import { useCurrentProject } from '@/lib/project'

const PER_PAGE = 50

export type NetworkInput = components['schemas']['NetworkInput']
export type NetworkUpdate = components['schemas']['NetworkUpdate']

export interface NetworkActionResult {
  action: Action
  guest_config: NetworkGuestConfig | null
}

// fetchNetworks reads every page of the project's networks.
export async function fetchNetworks(project: number, signal?: AbortSignal): Promise<Network[]> {
  const out: Network[] = []
  for (let page = 1; ; page++) {
    const body = unwrap(
      await api.GET('/networks', {
        headers: { 'Project-ID': String(project) },
        params: { query: { page, per_page: PER_PAGE } },
        signal,
      }),
    )
    out.push(...body.networks)
    if (body.networks.length < PER_PAGE || out.length >= body.meta.pagination.total_entries)
      return out
  }
}

export function useNetworks() {
  const { projectId } = useCurrentProject()
  return useQuery(
    computed(() => {
      const project = projectId.value
      return {
        queryKey: ['networks', project],
        queryFn: ({ signal }: { signal: AbortSignal }) => fetchNetworks(project, signal),
        enabled: project > 0,
      }
    }),
  )
}

export function useNetwork(id: Ref<number>) {
  const { projectId } = useCurrentProject()
  return useQuery(
    computed(() => ({
      queryKey: ['network', projectId.value, id.value],
      queryFn: async (): Promise<Network> =>
        unwrap(await api.GET('/networks/{id}', { params: { path: { id: id.value } } })),
      enabled: projectId.value > 0 && id.value > 0,
    })),
  )
}

// Refreshes everything a network change can touch.
export function useRefreshNetworks() {
  const qc = useQueryClient()
  const { projectId } = useCurrentProject()
  return () =>
    Promise.all(
      ['networks', 'network', 'servers', 'server', 'actions', 'project'].map((k) =>
        qc.invalidateQueries({ queryKey: [k, projectId.value] }),
      ),
    )
}

const path = (id: number) => ({ params: { path: { id } } })

export async function createNetwork(body: NetworkInput): Promise<Network> {
  return unwrap(await api.POST('/networks', { body })).network
}

export async function updateNetwork(id: number, body: NetworkUpdate): Promise<Network> {
  return unwrap(await api.PUT('/networks/{id}', { ...path(id), body }))
}

export async function deleteNetwork(id: number): Promise<void> {
  unwrap(await api.DELETE('/networks/{id}', path(id)))
}

export async function attachToNetwork(
  serverId: number,
  network: number,
  ip?: string,
): Promise<NetworkActionResult> {
  const body = { network: String(network), ...(ip ? { ip } : {}) }
  return unwrap(
    await api.POST('/servers/{id}/actions/attach_to_network', { ...path(serverId), body }),
  )
}

export async function detachFromNetwork(
  serverId: number,
  network: number,
): Promise<NetworkActionResult> {
  return unwrap(
    await api.POST('/servers/{id}/actions/detach_from_network', {
      ...path(serverId),
      body: { network: String(network) },
    }),
  )
}

import { useMutation, useQueryClient, type QueryClient } from '@tanstack/vue-query'
import { api } from '@/api/client'
import { unwrap } from '@/api/errors'
import type { AdminLocation } from '@/api/types'
import type { components } from '@/api/schema'
import { catalogKey } from '../catalog/queries'

export type LocationInput = components['schemas']['LocationInput']
export type NodeInput = components['schemas']['NodeInput']

const key = ['admin', 'locations']

// Catalog cells are per location and node, so they go stale with the locations.
function refresh(qc: QueryClient) {
  return Promise.all([
    qc.invalidateQueries({ queryKey: key }),
    qc.invalidateQueries({ queryKey: catalogKey }),
  ])
}

export function useSaveLocation(id?: number) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (body: LocationInput): Promise<AdminLocation> =>
      id === undefined
        ? unwrap(await api.POST('/admin/locations', { body }))
        : unwrap(await api.PUT('/admin/locations/{id}', { params: { path: { id } }, body })),
    onSuccess: () => refresh(qc),
  })
}

export function useDeleteLocation() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (id: number): Promise<void> => {
      unwrap(await api.DELETE('/admin/locations/{id}', { params: { path: { id } } }))
    },
    onSuccess: () => refresh(qc),
  })
}

export function useSyncNodes() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (id: number): Promise<AdminLocation> =>
      unwrap(await api.POST('/admin/locations/{id}/sync_nodes', { params: { path: { id } } })),
    onSuccess: () => refresh(qc),
  })
}

export function useUpdateNode(locationId: number) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (v: { nodeId: number; body: NodeInput }) =>
      unwrap(
        await api.PUT('/admin/locations/{id}/nodes/{node_id}', {
          params: { path: { id: locationId, node_id: v.nodeId } },
          body: v.body,
        }),
      ),
    onSuccess: () => refresh(qc),
  })
}

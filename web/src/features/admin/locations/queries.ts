import { ref, toValue, type Ref } from 'vue'
import { useMutation, useQuery, useQueryClient, type QueryClient } from '@tanstack/vue-query'
import { api } from '@/api/client'
import { unwrap } from '@/api/errors'
import type { AdminLocation, LocationEnrollment } from '@/api/types'
import type { components } from '@/api/schema'
import { catalogKey } from '../catalog/queries'

export type LocationInput = components['schemas']['LocationInput']
export type NodeInput = components['schemas']['NodeInput']

export type EnrollmentInput = components['schemas']['LocationEnrollmentInput']
export type EnrollmentCreated = components['schemas']['LocationEnrollmentCreated']
export type EnrollmentFinish = components['schemas']['LocationEnrollmentFinish']
export type EnrollmentFinished = components['schemas']['LocationEnrollmentFinished']

const key = ['admin', 'locations']
const enrollKey = ['admin', 'location_enrollments']

// Catalog cells are per location and node, so they go stale with the locations.
function refresh(qc: QueryClient) {
  return Promise.all([
    qc.invalidateQueries({ queryKey: key }),
    qc.invalidateQueries({ queryKey: catalogKey }),
    qc.invalidateQueries({ queryKey: enrollKey }),
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

// The token only comes back once, so the start response is handed to the enroll page here.
export const lastCreated = ref<Record<number, EnrollmentCreated>>({})

export const forgetCreated = (id: number) => {
  const rest = { ...lastCreated.value }
  delete rest[id]
  lastCreated.value = rest
}

const remember = (c: EnrollmentCreated) => {
  lastCreated.value = { ...lastCreated.value, [c.enrollment.id]: c }
}

export function useEnrollments() {
  return useQuery({
    queryKey: enrollKey,
    queryFn: async (): Promise<LocationEnrollment[]> =>
      unwrap(await api.GET('/admin/location_enrollments')).enrollments,
  })
}

export function useEnrollment(id: Ref<number>) {
  return useQuery({
    queryKey: [...enrollKey, id],
    queryFn: async (): Promise<LocationEnrollment> =>
      unwrap(
        await api.GET('/admin/location_enrollments/{id}', {
          params: { path: { id: toValue(id) } },
        }),
      ),
    refetchInterval: (q) =>
      q.state.data?.state === 'waiting' && !q.state.data.expired ? 3000 : false,
  })
}

export function useStartEnrollment() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (body: EnrollmentInput): Promise<EnrollmentCreated> =>
      unwrap(await api.POST('/admin/location_enrollments', { body })),
    onSuccess: (c) => {
      remember(c)
      return refresh(qc)
    },
  })
}

export function useNewEnrollmentToken(id: Ref<number>) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (): Promise<EnrollmentCreated> =>
      unwrap(
        await api.POST('/admin/location_enrollments/{id}/token', {
          params: { path: { id: toValue(id) } },
        }),
      ),
    onSuccess: (c) => {
      remember(c)
      return refresh(qc)
    },
  })
}

export function usePasteEnrollment(id: Ref<number>) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (blob: string): Promise<LocationEnrollment> =>
      unwrap(
        await api.POST('/admin/location_enrollments/{id}/report', {
          params: { path: { id: toValue(id) } },
          body: { blob },
        }),
      ),
    onSuccess: () => refresh(qc),
  })
}

export function useFinishEnrollment(id: Ref<number>) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (body: EnrollmentFinish): Promise<EnrollmentFinished> =>
      unwrap(
        await api.POST('/admin/location_enrollments/{id}/finish', {
          params: { path: { id: toValue(id) } },
          body,
        }),
      ),
    onSuccess: () => {
      forgetCreated(toValue(id))
      return refresh(qc)
    },
  })
}

export function useCancelEnrollment() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (id: number): Promise<void> => {
      unwrap(await api.DELETE('/admin/location_enrollments/{id}', { params: { path: { id } } }))
    },
    onSuccess: (_, id) => {
      forgetCreated(id)
      return refresh(qc)
    },
  })
}

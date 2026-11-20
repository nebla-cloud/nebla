import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { api } from '@/api/client'
import { unwrap } from '@/api/errors'
import type { components } from '@/api/schema'

type S = components['schemas']
export type CatalogEntry = S['CatalogEntry']
export type CatalogLocationState = S['CatalogLocationState']
export type CatalogNode = S['CatalogNode']
export type AdminImage = S['AdminImage']
export type CatalogAction = 'enable' | 'refresh' | 'disable'

export const catalogKey = ['admin', 'catalog'] as const
export const catalogPollMs = 3000

export function anyBuilding(entries: CatalogEntry[] | undefined): boolean {
  return !!entries?.some((e) => e.locations.some((l) => l.state === 'building'))
}

export function useCatalog() {
  return useQuery({
    queryKey: catalogKey,
    queryFn: async (): Promise<CatalogEntry[]> => unwrap(await api.GET('/admin/catalog')).catalog,
    refetchInterval: (q) => (anyBuilding(q.state.data) ? catalogPollMs : false),
  })
}

export function useAdminImages() {
  return useQuery({
    queryKey: ['admin', 'images'],
    queryFn: async (): Promise<AdminImage[]> =>
      unwrap(await api.GET('/admin/images', { params: { query: { per_page: 50 } } })).images,
  })
}

const paths = {
  enable: '/admin/catalog/{key}/locations/{location_id}/enable',
  refresh: '/admin/catalog/{key}/locations/{location_id}/refresh',
  disable: '/admin/catalog/{key}/locations/{location_id}/disable',
} as const

export interface CatalogActionVars {
  key: string
  locationId: number
  action: CatalogAction
}

export function useCatalogAction() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async ({ key, locationId, action }: CatalogActionVars) =>
      unwrap(await api.POST(paths[action], { params: { path: { key, location_id: locationId } } }))
        .location,
    onSuccess: (loc, { key }) => {
      qc.setQueryData<CatalogEntry[]>(catalogKey, (old) =>
        old?.map((e) =>
          e.key !== key
            ? e
            : {
                ...e,
                locations: e.locations.map((l) => (l.location_id === loc.location_id ? loc : l)),
              },
        ),
      )
    },
    onSettled: (_d, _e, { action }) =>
      Promise.all([
        qc.invalidateQueries({ queryKey: catalogKey }),
        action === 'disable' ? qc.invalidateQueries({ queryKey: ['admin', 'images'] }) : null,
      ]),
  })
}

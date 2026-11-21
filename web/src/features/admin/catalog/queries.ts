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

import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { api } from '@/api/client'
import { unwrap } from '@/api/errors'
import type { components } from '@/api/schema'

type S = components['schemas']
export type CatalogEntry = S['CatalogEntry']
export type CatalogLocationState = S['CatalogLocationState']
export type CatalogNode = S['CatalogNode']
export type AdminImage = S['AdminImage']
export type ImageInput = S['ImageInput']
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
    queryKey: imagesKey,
    queryFn: async (): Promise<AdminImage[]> =>
      unwrap(await api.GET('/admin/images', { params: { query: { per_page: 50 } } })).images,
  })
}

const imagesKey = ['admin', 'images']

export function useSaveImage() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (v: { id?: number; body: ImageInput }): Promise<AdminImage> =>
      v.id === undefined
        ? unwrap(await api.POST('/admin/images', { body: v.body }))
        : unwrap(
            await api.PUT('/admin/images/{id}', { params: { path: { id: v.id } }, body: v.body }),
          ),
    onSuccess: () => qc.invalidateQueries({ queryKey: imagesKey }),
  })
}

export function useDeleteImage() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (id: number) =>
      unwrap(await api.DELETE('/admin/images/{id}', { params: { path: { id } } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: imagesKey }),
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
        action === 'disable' ? qc.invalidateQueries({ queryKey: imagesKey }) : null,
      ]),
  })
}

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

import { computed } from 'vue'
import { useQuery } from '@tanstack/vue-query'
import { api } from '@/api/client'
import { unwrap } from '@/api/errors'
import type { Location, ServerType } from '@/api/types'
import { useCurrentProject } from '@/lib/project'
import type { Image } from './createServer'

const PAGE = { params: { query: { per_page: 50 } } }

export const locationsKey = (projectId: number) => ['locations', projectId]
export const fetchLocations = async (): Promise<Location[]> =>
  unwrap(await api.GET('/locations', PAGE)).locations

export function useLocations() {
  const { projectId } = useCurrentProject()
  return useQuery({
    queryKey: computed(() => locationsKey(projectId.value)),
    queryFn: fetchLocations,
    enabled: computed(() => projectId.value > 0),
  })
}

export function useImages() {
  const { projectId } = useCurrentProject()
  return useQuery({
    queryKey: computed(() => ['images', projectId.value]),
    queryFn: async (): Promise<Image[]> => unwrap(await api.GET('/images', PAGE)).images,
    enabled: computed(() => projectId.value > 0),
  })
}

export function useServerTypes() {
  const { projectId } = useCurrentProject()
  return useQuery({
    queryKey: computed(() => ['server-types', projectId.value]),
    queryFn: async (): Promise<ServerType[]> =>
      unwrap(await api.GET('/server_types', PAGE)).server_types,
    enabled: computed(() => projectId.value > 0),
  })
}

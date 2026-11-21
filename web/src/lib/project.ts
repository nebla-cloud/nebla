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

import { computed, watch, type ComputedRef, type Ref } from 'vue'
import { useRoute, type Router } from 'vue-router'
import { useQuery, type QueryClient } from '@tanstack/vue-query'
import { onForbidden } from '@/api/client'
import type { Project } from '@/api/types'
import { projectQuery } from '@/features/projects/queries'
import { parseProjectId } from './projectId'
import { readStorage, removeStorage, writeStorage } from './storage'

export { parseProjectId }

const LAST_PROJECT = 'nebla.lastProject'

export function readLastProject(): number | null {
  return parseProjectId(readStorage(LAST_PROJECT))
}

export function writeLastProject(id: number): void {
  writeStorage(LAST_PROJECT, String(id))
}

export function forgetProject(id: number): void {
  if (readLastProject() === id) removeStorage(LAST_PROJECT)
}

// Re-runs the guards after a role change so a lost page falls through to not found.
export async function recheckAccess(router: Router, qc: QueryClient, id: number): Promise<void> {
  await Promise.all([
    qc.invalidateQueries({ queryKey: ['project', id] }),
    qc.invalidateQueries({ queryKey: ['projects'] }),
  ])
  await router.replace({ path: router.currentRoute.value.fullPath, force: true })
}

// A 403 under a project means our role is stale: refetch it and re-run the guards on gated pages.
export function refreshOnForbidden(router: Router, qc: QueryClient): void {
  const busy = new Set<number>()
  onForbidden((id) => {
    if (busy.has(id)) return
    busy.add(id)
    const cur = router.currentRoute.value
    const gated = !!cur.meta.roles && parseProjectId(cur.params.projectId) === id
    const run = gated
      ? recheckAccess(router, qc, id)
      : Promise.all([
          qc.invalidateQueries({ queryKey: ['project', id] }),
          qc.invalidateQueries({ queryKey: ['projects'] }),
        ])
    void run.catch(() => {}).finally(() => busy.delete(id))
  })
}

export function useCurrentProject(): {
  projectId: ComputedRef<number>
  project: Ref<Project | undefined>
} {
  const route = useRoute()
  const projectId = computed(() => parseProjectId(route.params.projectId) ?? 0)
  watch(
    projectId,
    (id) => {
      if (id) writeLastProject(id)
    },
    { immediate: true },
  )
  const { data: project } = useQuery(
    computed(() => ({ ...projectQuery(projectId.value), enabled: projectId.value > 0 })),
  )
  return { projectId, project }
}

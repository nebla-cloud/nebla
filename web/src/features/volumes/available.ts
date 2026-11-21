import { ref } from 'vue'
import type { RouteLocationNormalizedLoaded } from 'vue-router'
import type { QueryClient } from '@tanstack/vue-query'
import type { Location } from '@/api/types'
import { fetchLocations, locationsKey } from '@/features/servers-create/queries'
import { parseProjectId } from '@/lib/projectId'
import { queryClient } from '@/lib/queryClient'

// Whether any of a project's locations offers volumes, by project id.
const known = ref<Record<number, boolean>>({})

export function anyVolumes(locations: Location[] | undefined): boolean | undefined {
  return locations ? locations.some((l) => l.volumes_available) : undefined
}

export function trackAvailability(qc: QueryClient): void {
  qc.getQueryCache().subscribe((e) => {
    const [head, pid] = e.query.queryKey as unknown[]
    if (head !== 'locations' || typeof pid !== 'number') return
    if (e.type === 'removed') {
      const rest = { ...known.value }
      delete rest[pid]
      known.value = rest
      return
    }
    const v = anyVolumes(e.query.state.data as Location[] | undefined)
    if (v !== undefined && known.value[pid] !== v) known.value = { ...known.value, [pid]: v }
  })
}

trackAvailability(queryClient)

// Nav: hidden until this project's locations are known to offer volumes.
export function volumesNavHidden(
  route: RouteLocationNormalizedLoaded,
  qc: QueryClient = queryClient,
): boolean {
  const pid = parseProjectId(route.params.projectId)
  if (pid === null) return true
  if (!qc.getQueryCache().find({ queryKey: locationsKey(pid), exact: true })) {
    void qc.prefetchQuery({ queryKey: locationsKey(pid), queryFn: fetchLocations })
  }
  return known.value[pid] !== true
}

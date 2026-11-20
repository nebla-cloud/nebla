import { computed, ref, type ComputedRef } from 'vue'
import { useQuery, type QueryClient } from '@tanstack/vue-query'
import { api } from '@/api/client'
import { ApiError, unwrap } from '@/api/errors'
import type { AdminLocation, ServerType } from '@/api/types'
import { queryClient } from '@/lib/queryClient'
import { useSession } from '@/lib/session'
import { catalogKey, type AdminImage, type CatalogEntry } from '../catalog/queries'
import { rangeSize } from '../ippools/cidr'
import type { IpPool } from '../ippools/queries'

export type StepKey = 'location' | 'images' | 'types' | 'pools'

export interface ChecklistData {
  locations: AdminLocation[]
  types: ServerType[]
  pools: IpPool[]
  catalog: CatalogEntry[]
  images: AdminImage[]
}

export interface ChecklistStep {
  key: StepKey
  done: boolean
  detail: string
}

function plural(n: number, one: string, many: string): string {
  return `${n} ${n === 1 ? one : many}`
}

// A pool has room when allocated is below the range size; a gateway inside the range takes one address.
function hasFreeAddresses(p: IpPool): boolean {
  const size = rangeSize(p.range_start, p.range_end)
  const gatewayInRange =
    rangeSize(p.range_start, p.gateway) > 0n && rangeSize(p.gateway, p.range_end) > 0n
  return BigInt(p.allocated) + (gatewayInRange ? 1n : 0n) < size
}

export function checklistSteps(d: ChecklistData): ChecklistStep[] {
  const online = d.locations
    .map((l) => ({ name: l.name, n: l.nodes.filter((n) => n.status === 'online').length }))
    .find((l) => l.n > 0)
  const readyCells = d.catalog.reduce(
    (n, e) => n + e.locations.filter((l) => l.state === 'ready' || l.state === 'partial').length,
    0,
  )
  const own = d.images.filter((i) => !i.catalog_key).length
  const freePools = d.pools.filter(hasFreeAddresses).length
  return [
    {
      key: 'location',
      done: !!online,
      detail: online
        ? `${online.name}: ${plural(online.n, 'node', 'nodes')} online`
        : 'Link a Proxmox cluster so Nebla can build servers on it.',
    },
    {
      key: 'images',
      done: readyCells + own > 0,
      detail:
        readyCells + own > 0
          ? plural(readyCells + own, 'image ready', 'images ready')
          : 'Pick the images people can install in each location.',
    },
    {
      key: 'types',
      done: d.types.length > 0,
      detail:
        d.types.length > 0
          ? plural(d.types.length, 'server type', 'server types')
          : 'Sizes people can pick, such as 2 vCPU, 4 GB, 40 GB.',
    },
    {
      key: 'pools',
      done: freePools > 0,
      detail:
        freePools > 0
          ? `${plural(freePools, 'pool', 'pools')} with free addresses`
          : 'Addresses Nebla hands out to new servers.',
    },
  ]
}

export function isPanelReady(d: ChecklistData): boolean {
  return checklistSteps(d).every((s) => s.done)
}

// Same keys as the admin pages' hooks, so the guard, nav and pages share one cache.
const sources = {
  locations: {
    queryKey: ['admin', 'locations'],
    queryFn: async () =>
      unwrap(await api.GET('/admin/locations', { params: { query: { per_page: 50 } } })).locations,
  },
  types: {
    queryKey: ['admin', 'server-types'],
    queryFn: async () =>
      unwrap(await api.GET('/admin/server_types', { params: { query: { per_page: 50 } } }))
        .server_types,
  },
  pools: {
    queryKey: ['admin', 'ip-pools'],
    queryFn: async () =>
      unwrap(await api.GET('/admin/ip_pools', { params: { query: { per_page: 50 } } })).ip_pools,
  },
  catalog: {
    queryKey: catalogKey,
    queryFn: async () => unwrap(await api.GET('/admin/catalog')).catalog,
  },
  images: {
    queryKey: ['admin', 'images'],
    queryFn: async () =>
      unwrap(await api.GET('/admin/images', { params: { query: { per_page: 50 } } })).images,
  },
}

// Last known answer, kept in sync with the cache so route meta (nav) stays current.
const known = ref<boolean | undefined>()
export const panelReadyKnown = known

function syncKnown(): void {
  const cache = queryClient.getQueryCache()
  const [locations, types, pools, catalog, images] = Object.values(sources).map(
    (src) => cache.find({ queryKey: src.queryKey, exact: true })?.state.data,
  ) as [AdminLocation[]?, ServerType[]?, IpPool[]?, CatalogEntry[]?, AdminImage[]?]
  known.value =
    locations && types && pools && catalog && images
      ? isPanelReady({ locations, types, pools, catalog, images })
      : undefined
}

const watched = new Set(Object.values(sources).map((src) => JSON.stringify(src.queryKey)))
queryClient.getQueryCache().subscribe((e) => {
  if (e.type === 'removed' || watched.has(JSON.stringify(e.query.queryKey))) syncKnown()
})

export function useChecklistData() {
  const { me } = useSession()
  const enabled = computed(() => !!me.value?.is_admin)
  const qs = [
    useQuery({ ...sources.locations, enabled }),
    useQuery({ ...sources.types, enabled }),
    useQuery({ ...sources.pools, enabled }),
    useQuery({ ...sources.catalog, enabled }),
    useQuery({ ...sources.images, enabled }),
  ] as const
  const [locations, types, pools, catalog, images] = qs
  const data = computed<ChecklistData | undefined>(() => {
    if (!locations.data.value || !types.data.value || !pools.data.value) return undefined
    if (!catalog.data.value || !images.data.value) return undefined
    return {
      locations: locations.data.value,
      types: types.data.value,
      pools: pools.data.value,
      catalog: catalog.data.value,
      images: images.data.value,
    }
  })
  const error = computed(() => qs.find((q) => q.error.value)?.error.value ?? null)
  const retry = () => {
    for (const q of qs) if (q.error.value) void q.refetch()
  }
  return { data, error, retry }
}

export function usePanelReady(): ComputedRef<boolean | undefined> {
  const { data } = useChecklistData()
  return computed(() => (data.value ? isPanelReady(data.value) : undefined))
}

// undefined when the check fails (other than 401), so navigation never blocks on it.
export async function panelReady(qc: QueryClient): Promise<boolean | undefined> {
  const staleTime = 30_000
  try {
    const [locations, types, pools, catalog, images] = await Promise.all([
      qc.fetchQuery({ ...sources.locations, staleTime }),
      qc.fetchQuery({ ...sources.types, staleTime }),
      qc.fetchQuery({ ...sources.pools, staleTime }),
      qc.fetchQuery({ ...sources.catalog, staleTime }),
      qc.fetchQuery({ ...sources.images, staleTime }),
    ])
    return isPanelReady({ locations, types, pools, catalog, images })
  } catch (e) {
    if (e instanceof ApiError && e.status === 401) throw e
    return undefined
  }
}

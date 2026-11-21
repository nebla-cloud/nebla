import type { Server } from '@/api/types'

export type StatusFilter = 'all' | 'running' | 'off'

export function parseStatusFilter(q: unknown): StatusFilter {
  return q === 'running' || q === 'off' ? q : 'all'
}

export function filterServers(list: Server[], f: StatusFilter): Server[] {
  return f === 'all' ? list : list.filter((s) => s.status === f)
}

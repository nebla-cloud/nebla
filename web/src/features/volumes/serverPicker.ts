import type { Server, Volume } from '@/api/types'

// A server can hold at most this many volumes.
export const MAX_PER_SERVER = 14
export const MIN_SIZE_GB = 10
export const MAX_SIZE_GB = 10240

export interface ServerChoice {
  server: Server
  disabledReason: string | null
}

const busy: Server['status'][] = ['initializing', 'rebuilding', 'deleting']

// Servers a volume in locationId could attach to; the API still has the final say.
export function serverChoices(servers: Server[], locationId: number): ServerChoice[] {
  return servers.map((server) => ({
    server,
    disabledReason:
      server.location.id !== locationId
        ? 'In a different location'
        : server.volumes.length >= MAX_PER_SERVER
          ? `Already has ${MAX_PER_SERVER} volumes`
          : busy.includes(server.status)
            ? 'Busy right now'
            : null,
  }))
}

// Detached volumes a server could take.
export function attachableVolumes(volumes: Volume[], server: Server): Volume[] {
  return volumes.filter((v) => v.status === 'available' && v.location.id === server.location.id)
}

export function formatSize(gb: number): string {
  return `${gb} GB`
}

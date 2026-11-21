import type { Action } from '@/api/types'
import type { Firewall } from './queries'

export function makeFirewall(over: Partial<Firewall> = {}): Firewall {
  return {
    id: 3,
    name: 'web',
    labels: {},
    rules: [],
    applied_to: [],
    created: '2026-11-20T10:00:00Z',
    ...over,
  }
}

export function applyAction(id: number, serverId: number): Action {
  return {
    id,
    command: 'apply_firewalls',
    status: 'running',
    progress: 0,
    resources: [{ id: serverId, type: 'server' }],
    error: null,
    started: '2026-11-20T10:00:00Z',
    finished: null,
  }
}

export const listMeta = (n: number) => ({ pagination: { page: 1, per_page: 50, total_entries: n } })

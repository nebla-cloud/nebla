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

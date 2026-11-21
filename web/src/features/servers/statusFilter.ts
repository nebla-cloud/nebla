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

import type { Server } from '@/api/types'

export type StatusFilter = 'all' | 'running' | 'off'

export function parseStatusFilter(q: unknown): StatusFilter {
  return q === 'running' || q === 'off' ? q : 'all'
}

export function filterServers(list: Server[], f: StatusFilter): Server[] {
  return f === 'all' ? list : list.filter((s) => s.status === f)
}

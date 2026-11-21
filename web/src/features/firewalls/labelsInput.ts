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

import { validateLabels } from '@/features/servers/labels'

// Parses "k=v, k2=v2" into labels, or the first validation message.
export function parseLabels(s: string): { labels: Record<string, string> } | { error: string } {
  const rows = s
    .split(',')
    .map((p) => p.trim())
    .filter(Boolean)
    .map((p) => {
      const i = p.indexOf('=')
      return i === -1 ? { key: p, value: '' } : { key: p.slice(0, i), value: p.slice(i + 1) }
    })
  const v = validateLabels(rows)
  return v.ok ? { labels: v.labels } : { error: Object.values(v.errors)[0]! }
}

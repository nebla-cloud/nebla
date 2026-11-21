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

import type { ServerType } from '@/api/types'

export function formatMemory(mb: number): string {
  if (mb < 1024) return `${mb} MB`
  const gb = mb / 1024
  return `${Number.isInteger(gb) ? gb : Number(gb.toFixed(1))} GB`
}

const dateFormat = new Intl.DateTimeFormat('en-GB', {
  day: 'numeric',
  month: 'short',
  year: 'numeric',
})

export function formatDate(iso: string): string {
  return dateFormat.format(new Date(iso))
}

export function typeSpec(t: ServerType): string {
  return `${t.cores} vCPU, ${formatMemory(t.memory_mb)}, ${t.disk_gb} GB`
}

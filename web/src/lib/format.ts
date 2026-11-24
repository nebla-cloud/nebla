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

const dateTimeFormat = new Intl.DateTimeFormat('en-GB', {
  day: 'numeric',
  month: 'short',
  year: 'numeric',
  hour: '2-digit',
  minute: '2-digit',
})

// The viewer's local time.
export function formatDateTime(iso: string): string {
  return dateTimeFormat.format(new Date(iso))
}

export function formatBytes(n: number): string {
  if (n <= 0) return '—'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let v = n
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${Number(v.toFixed(v < 10 && i > 0 ? 1 : 0))} ${units[i]}`
}

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

import type { Server, ServerType } from '@/api/types'
import { availableTypes } from '@/features/servers-create/createServer'

export interface TypeChoice {
  type: ServerType
  current: boolean
  disabledReason: string | null
  delta: string
}

const signed = (n: number) => (n >= 0 ? `+${n}` : `${n}`)

function delta(type: ServerType, server: Server, growDisk: boolean): string {
  const cur = server.server_type
  const mem = Math.round((type.memory_mb - cur.memory_mb) / 1024)
  const disk =
    growDisk && type.disk_gb > server.disk_gb
      ? `disk grows to ${type.disk_gb} GB`
      : `disk stays ${server.disk_gb} GB`
  return `${signed(type.cores - cur.cores)} vCPU, ${signed(mem)} GB memory, ${disk}`
}

export function typeChoices(types: ServerType[], server: Server, growDisk: boolean): TypeChoice[] {
  return availableTypes(types, server.location.id, server.location.name).map(
    ({ type, disabledReason }) => {
      const current = type.id === server.server_type.id
      let reason = disabledReason
      if (!reason && growDisk && type.disk_gb < server.disk_gb)
        reason = `Smaller disk than this server's ${server.disk_gb} GB`
      if (current) reason = 'Current type'
      return { type, current, disabledReason: reason, delta: delta(type, server, growDisk) }
    },
  )
}

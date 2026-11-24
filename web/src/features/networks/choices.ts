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

import type { Network, Server } from '@/api/types'

// A server can be on at most this many private networks.
export const MAX_PER_SERVER = 3

export interface ServerChoice {
  server: Server
  disabledReason: string | null
}

const busy: Server['status'][] = ['initializing', 'rebuilding', 'deleting']

// Servers in the network's location; the API still has the final say.
export function serverChoices(
  servers: Server[],
  locationId: number,
  networkId: number,
): ServerChoice[] {
  return servers
    .filter((s) => s.location.id === locationId)
    .map((server) => ({
      server,
      disabledReason: server.private_net.some((n) => n.network === networkId)
        ? 'Already on this network'
        : server.private_net.length >= MAX_PER_SERVER
          ? `Already on ${MAX_PER_SERVER} networks`
          : busy.includes(server.status)
            ? 'Busy right now'
            : null,
    }))
}

export interface NetworkChoice {
  network: Network
  disabledReason: string | null
}

// Networks in the server's location it could join; the API still has the final say.
export function networkChoices(networks: Network[], server: Server): NetworkChoice[] {
  return networks
    .filter((n) => n.location.id === server.location.id)
    .map((network) => ({
      network,
      disabledReason: server.private_net.some((p) => p.network === network.id)
        ? 'Already on this network'
        : server.private_net.length >= MAX_PER_SERVER
          ? `Already on ${MAX_PER_SERVER} networks`
          : busy.includes(server.status)
            ? 'Busy right now'
            : null,
    }))
}

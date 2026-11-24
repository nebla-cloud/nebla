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

import type { Network, NetworkGuestConfig } from '@/api/types'

export function makeNetwork(over: Partial<Network> = {}): Network {
  return {
    id: 1,
    name: 'backend',
    labels: {},
    location: { id: 1, name: 'home', description: '' },
    ip_range: '10.0.0.0/16',
    servers: [],
    created: '2026-11-23T10:00:00Z',
    ...over,
  }
}

export function makeNetworkGuestConfig(over: Partial<NetworkGuestConfig> = {}): NetworkGuestConfig {
  return {
    apply: 'sudo ip addr add 10.0.0.2/16 dev eth1\nsudo ip link set eth1 up',
    persist: 'echo persist | sudo tee /etc/netplan/61-nebla-private-1.yaml',
    remove: null,
    note: null,
    ...over,
  }
}

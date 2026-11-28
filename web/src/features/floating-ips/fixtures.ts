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

import type { FloatingIp, GuestConfig } from '@/api/types'

export function makeFloatingIP(over: Partial<FloatingIp> = {}): FloatingIp {
  return {
    id: 1,
    name: 'web-vip',
    description: '',
    labels: {},
    type: 'ipv4',
    ip: '203.0.113.50',
    location: { id: 1, name: 'home', description: '' },
    server: null,
    status: 'active',
    guest_config: null,
    created: '2026-11-23T10:00:00Z',
    protection: { delete: false },
    ...over,
  }
}

export function makeGuestConfig(over: Partial<GuestConfig> = {}): GuestConfig {
  return {
    server: 'web1',
    apply: 'sudo ip addr add 203.0.113.50/32 dev eth0\nsudo arping -U -c 3 -I eth0 203.0.113.50',
    persist: 'echo persist | sudo tee /etc/netplan/60-nebla-floating-203-0-113-50.yaml',
    previous_server: null,
    remove: null,
    ...over,
  }
}

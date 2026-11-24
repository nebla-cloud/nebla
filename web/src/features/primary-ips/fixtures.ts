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

import type { PrimaryIp } from '@/api/types'

export function makePrimaryIP(over: Partial<PrimaryIp> = {}): PrimaryIp {
  return {
    id: 1,
    name: 'web-ip',
    description: '',
    labels: {},
    type: 'ipv4',
    ip: '203.0.113.60',
    location: { id: 1, name: 'home', description: '' },
    server: null,
    auto_delete: true,
    status: 'active',
    created: '2026-11-23T10:00:00Z',
    ...over,
  }
}

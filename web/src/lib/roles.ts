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

import type { Role, TokenPermission } from '@/api/types'

const ROLE_LABELS: Record<Role, string> = {
  owner: 'Owner',
  admin: 'Admin',
  member: 'Member',
  read_only: 'Read-only',
}

export function roleLabel(r: Role): string {
  return ROLE_LABELS[r]
}

export function permissionLabel(p: TokenPermission): string {
  return p === 'read' ? 'Read' : 'Read and write'
}

export function isManager(r?: Role): boolean {
  return r === 'owner' || r === 'admin'
}

export function assignableRoles(caller: Role): Role[] {
  if (caller === 'owner') return ['owner', 'admin', 'member', 'read_only']
  if (caller === 'admin') return ['admin', 'member', 'read_only']
  return []
}

export function canWrite(r?: Role): boolean {
  return r !== 'read_only'
}

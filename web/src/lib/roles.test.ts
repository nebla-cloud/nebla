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

import { describe, expect, it } from 'vitest'
import { assignableRoles, canWrite, isManager, permissionLabel, roleLabel } from './roles'

describe('roles', () => {
  it('canWrite is false only for read_only', () => {
    expect(canWrite('read_only')).toBe(false)
    expect(canWrite('member')).toBe(true)
    expect(canWrite('owner')).toBe(true)
  })
  it('labels roles and permissions', () => {
    expect(roleLabel('read_only')).toBe('Read-only')
    expect(roleLabel('owner')).toBe('Owner')
    expect(permissionLabel('read_write')).toBe('Read and write')
    expect(permissionLabel('read')).toBe('Read')
  })
  it('isManager is true for owner and admin only', () => {
    expect(isManager('owner')).toBe(true)
    expect(isManager('admin')).toBe(true)
    expect(isManager('member')).toBe(false)
    expect(isManager(undefined)).toBe(false)
  })
  it('assignableRoles excludes owner for admin', () => {
    expect(assignableRoles('owner')).toEqual(['owner', 'admin', 'member', 'read_only'])
    expect(assignableRoles('admin')).toEqual(['admin', 'member', 'read_only'])
    expect(assignableRoles('member')).toEqual([])
    expect(assignableRoles('read_only')).toEqual([])
  })
})

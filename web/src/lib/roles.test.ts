import { describe, expect, it } from 'vitest'
import { assignableRoles, isManager, permissionLabel, roleLabel } from './roles'

describe('roles', () => {
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

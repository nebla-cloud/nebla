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

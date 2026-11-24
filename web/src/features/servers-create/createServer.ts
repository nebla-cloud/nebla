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

import type { components } from '@/api/schema'
import type { Location, PrimaryIp, ServerType } from '@/api/types'

export type Image = components['schemas']['Image']
export type Quota = components['schemas']['Quota']
export type ProjectUsage = components['schemas']['ProjectUsage']

export type CreateForm = {
  locationId: number | null
  imageId: number | null
  typeId: number | null
  ipv4: boolean
  ipv6: boolean
  // A primary IP id to reuse for the family; null allocates a new address.
  ipv4Existing: number | null
  ipv6Existing: number | null
  sshKeyIds: number[]
  userData: string
  name: string
  nameTouched: boolean
}

export interface CreateContext {
  locations: Location[]
  images: Image[]
  types: ServerType[]
}

export interface QuotaLine {
  label: string
  used: number
  after: number
  max: number
}

export const NAME_PATTERN = /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$/
export const MAX_USER_DATA = 32 * 1024

const notIn = (name: string) => `Not available in ${name}`

export function availableImages(
  images: Image[],
  locationId: number,
  locationName = '',
): { image: Image; disabledReason: string | null }[] {
  return images.map((image) => ({
    image,
    disabledReason: image.locations.includes(locationId) ? null : notIn(locationName),
  }))
}

// Deprecated types can't be created, so they're left out; an empty locations list means everywhere.
export function availableTypes(
  types: ServerType[],
  locationId: number,
  locationName = '',
): { type: ServerType; disabledReason: string | null }[] {
  return types
    .filter((t) => !t.deprecated)
    .map((type) => ({
      type,
      disabledReason:
        type.locations.length === 0 || type.locations.includes(locationId)
          ? null
          : notIn(locationName),
    }))
}

export const noAddresses = (name: string) =>
  `No free addresses in ${name}. Ask your Nebla admin to add an IP pool.`
export const PICK_FAMILY = 'Pick IPv4 or IPv6 so the server can be reached.'

// Unassigned, settled primary IPs of one family in the chosen location.
export function existingIPs(
  all: PrimaryIp[],
  locationId: number,
  family: 'ipv4' | 'ipv6',
): PrimaryIp[] {
  return all.filter(
    (p) =>
      p.type === family &&
      p.server === null &&
      p.status === 'active' &&
      p.location.id === locationId,
  )
}

// Only new addresses count toward the quota.
export function newIPCount(form: CreateForm): number {
  return (
    Number(form.ipv4 && form.ipv4Existing === null) +
    Number(form.ipv6 && form.ipv6Existing === null)
  )
}

export function publicNet(form: CreateForm): { ipv4: boolean | string; ipv6: boolean | string } {
  return {
    ipv4: form.ipv4 && form.ipv4Existing !== null ? String(form.ipv4Existing) : form.ipv4,
    ipv6: form.ipv6 && form.ipv6Existing !== null ? String(form.ipv6Existing) : form.ipv6,
  }
}

export function networkChoice(loc: Location): { ipv4: string | null; ipv6: string | null } {
  return {
    ipv4: loc.networks.ipv4 ? null : `No free IPv4 addresses in ${loc.name}`,
    ipv6: loc.networks.ipv6 ? null : `No free IPv6 addresses in ${loc.name}`,
  }
}

// A location with one free family starts with that family picked.
export function pickFamilies(form: CreateForm, loc: Location): CreateForm {
  const { ipv4, ipv6 } = loc.networks
  return ipv4 !== ipv6 ? { ...form, ipv4, ipv6 } : form
}

// Mirrors projects.ReserveQuota: a max of 0 is unlimited and only growing dimensions are checked.
export function quotaAfter(
  q: Quota,
  u: ProjectUsage,
  t: ServerType,
  ipCount: number,
): { ok: boolean; lines: QuotaLine[] } {
  const dims = [
    { label: 'Servers', used: u.servers, add: 1, max: q.max_servers },
    { label: 'vCPU', used: u.vcpu, add: t.cores, max: q.max_vcpu },
    { label: 'Memory', used: u.memory_mb, add: t.memory_mb, max: q.max_memory_mb },
    { label: 'Disk', used: u.disk_gb, add: t.disk_gb, max: q.max_disk_gb },
    { label: 'IPs', used: u.ips, add: ipCount, max: q.max_ips },
  ]
  const ok = dims.every((d) => d.max <= 0 || d.add <= 0 || d.used + d.add <= d.max)
  return {
    ok,
    lines: dims.map((d) => ({ label: d.label, used: d.used, after: d.used + d.add, max: d.max })),
  }
}

export function defaultName(image: Image, type: ServerType, existing: string[]): string {
  const base = `${image.name}-${type.name}`.toLowerCase().replace(/[^a-z0-9-]/g, '-')
  const taken = new Set(existing)
  for (let n = 1; ; n++) {
    const suffix = `-${n}`
    const name = base.slice(0, 63 - suffix.length) + suffix
    if (!taken.has(name)) return name
  }
}

export function reconcile(form: CreateForm, ctx: CreateContext): CreateForm {
  const loc = ctx.locations.find((l) => l.id === form.locationId)
  if (!loc) return form
  const imageOk = availableImages(ctx.images, loc.id).some(
    (o) => o.image.id === form.imageId && !o.disabledReason,
  )
  const typeOk = availableTypes(ctx.types, loc.id).some(
    (o) => o.type.id === form.typeId && !o.disabledReason,
  )
  const nets = networkChoice(loc)
  // An existing IP needs no free pool address.
  return {
    ...form,
    imageId: imageOk ? form.imageId : null,
    typeId: typeOk ? form.typeId : null,
    ipv4: form.ipv4 && (!nets.ipv4 || form.ipv4Existing !== null),
    ipv6: form.ipv6 && (!nets.ipv6 || form.ipv6Existing !== null),
    ipv4Existing: form.ipv4 ? form.ipv4Existing : null,
    ipv6Existing: form.ipv6 ? form.ipv6Existing : null,
  }
}

export function userDataBytes(s: string): number {
  return new TextEncoder().encode(s).length
}

export function createBlocker(
  form: CreateForm,
  ctx: CreateContext,
  opts: { quotaOk: boolean; existing: string[]; backupsOk?: boolean },
): string | null {
  const loc = ctx.locations.find((l) => l.id === form.locationId)
  if (!loc) return 'Pick a location.'
  const checked = reconcile(form, ctx)
  if (!loc.networks.ipv4 && !loc.networks.ipv6 && !checked.ipv4 && !checked.ipv6)
    return noAddresses(loc.name)
  if (checked.imageId === null) return 'Pick an image.'
  if (checked.typeId === null) return 'Pick a type.'
  if (!checked.ipv4 && !checked.ipv6) return PICK_FAMILY
  const name = form.name.trim()
  if (!name) return 'Give the server a name.'
  if (!NAME_PATTERN.test(name))
    return 'Use lowercase letters, numbers and hyphens in the name, starting and ending with a letter or number.'
  if (opts.existing.includes(name)) return `A server called ${name} already exists.`
  if (userDataBytes(form.userData) > MAX_USER_DATA)
    return 'Cloud-init user data must be 32 KiB or less.'
  if (!opts.quotaOk) return "This server doesn't fit in the project's quota."
  if (opts.backupsOk === false) return "Daily backups don't fit in the project's backup quota."
  return null
}

import { describe, expect, it } from 'vitest'
import type { Location, ServerType } from '@/api/types'
import {
  availableImages,
  availableTypes,
  createBlocker,
  defaultName,
  networkChoice,
  pickFamilies,
  quotaAfter,
  reconcile,
  type CreateForm,
  type Image,
  type ProjectUsage,
  type Quota,
} from './createServer'

const loc = (id: number, name: string, ipv4 = true, ipv6 = true): Location => ({
  id,
  name,
  description: '',
  networks: { ipv4, ipv6 },
})
const img = (id: number, name: string, locations: number[]): Image => ({
  id,
  name,
  description: '',
  os_flavor: 'debian',
  os_version: '13',
  locations,
})
const typ = (id: number, name: string, locations: number[] = [], over = {}): ServerType => ({
  id,
  name,
  description: '',
  cores: 2,
  memory_mb: 4096,
  disk_gb: 40,
  locations,
  deprecated: false,
  ...over,
})

const ctx = {
  locations: [loc(1, 'home'), loc(2, 'rack-2', false, true)],
  images: [img(3, 'debian-13', [1]), img(4, 'Ubuntu 24.04', [1, 2])],
  types: [typ(2, 'small-2'), typ(6, 'big-8', [1])],
}

const form = (over: Partial<CreateForm> = {}): CreateForm => ({
  locationId: 1,
  imageId: 3,
  typeId: 2,
  ipv4: true,
  ipv6: false,
  sshKeyIds: [],
  userData: '',
  name: 'web1',
  nameTouched: true,
  ...over,
})

const quota = (over: Partial<Quota> = {}): Quota => ({
  max_servers: 10,
  max_vcpu: 32,
  max_memory_mb: 65536,
  max_disk_gb: 1000,
  max_ips: 10,
  ...over,
})
const usage = (over: Partial<ProjectUsage> = {}): ProjectUsage => ({
  servers: 3,
  vcpu: 6,
  memory_mb: 12288,
  disk_gb: 120,
  ips: 3,
  ...over,
})

describe('createServer', () => {
  it('switching location clears unavailable picks', () => {
    const before = form()
    expect(createBlocker(before, ctx, { quotaOk: true, existing: [] })).toBeNull()
    const after = reconcile({ ...before, locationId: 2 }, ctx)
    expect(after.imageId).toBeNull()
    expect(after.typeId).toBe(2)
    expect(after.ipv4).toBe(false)
    expect(createBlocker(after, ctx, { quotaOk: true, existing: [] })).not.toBeNull()
  })

  it('reconcile clears a type restricted to another location', () => {
    const after = reconcile(form({ typeId: 6, imageId: 4, locationId: 2 }), ctx)
    expect(after.typeId).toBeNull()
    expect(after.imageId).toBe(4)
  })

  it('reconcile turns both families off when neither is available', () => {
    const c = { ...ctx, locations: [loc(1, 'home', false, false)] }
    const after = reconcile(form({ ipv4: true, ipv6: true }), c)
    expect(after.ipv4).toBe(false)
    expect(after.ipv6).toBe(false)
  })

  it('availableImages gives the location name as the reason', () => {
    expect(availableImages(ctx.images, 2, 'rack-2')).toEqual([
      { image: ctx.images[0], disabledReason: 'Not available in rack-2' },
      { image: ctx.images[1], disabledReason: null },
    ])
  })

  it('availableTypes treats no locations as everywhere and hides deprecated', () => {
    const types = [...ctx.types, typ(9, 'old-1', [], { deprecated: true })]
    expect(availableTypes(types, 2, 'rack-2')).toEqual([
      { type: ctx.types[0], disabledReason: null },
      { type: ctx.types[1], disabledReason: 'Not available in rack-2' },
    ])
  })

  it('defaultName skips taken names', () => {
    expect(defaultName(img(3, 'Debian 13', [1]), typ(2, 'small-2'), ['debian-13-small-2-1'])).toBe(
      'debian-13-small-2-2',
    )
  })

  it('defaultName sanitises', () => {
    expect(defaultName(img(4, 'Ubuntu 24.04', [1]), typ(2, 'small-2'), [])).toBe(
      'ubuntu-24-04-small-2-1',
    )
  })

  it('defaultName keeps the number when cutting to 63', () => {
    const name = defaultName(img(4, 'a'.repeat(80), [1]), typ(2, 'small-2'), [])
    expect(name).toHaveLength(63)
    expect(name.endsWith('-1')).toBe(true)
  })

  it('quotaAfter blocks when over', () => {
    const r = quotaAfter(quota({ max_servers: 3 }), usage({ servers: 3 }), typ(2, 'small-2'), 1)
    expect(r.ok).toBe(false)
    expect(r.lines[0]).toEqual({ label: 'Servers', used: 3, after: 4, max: 3 })
  })

  it('quotaAfter treats 0 as unlimited', () => {
    // projects.ReserveQuota only checks a limit when it's above 0.
    const r = quotaAfter(
      quota({ max_servers: 0, max_vcpu: 0, max_memory_mb: 0, max_disk_gb: 0, max_ips: 0 }),
      usage({ servers: 500, vcpu: 900 }),
      typ(2, 'small-2'),
      2,
    )
    expect(r.ok).toBe(true)
  })

  it('quotaAfter ignores dimensions this server does not grow', () => {
    const r = quotaAfter(quota({ max_ips: 2 }), usage({ ips: 5 }), typ(2, 'small-2'), 0)
    expect(r.ok).toBe(true)
    expect(r.lines.find((l) => l.label === 'IPs')).toEqual({
      label: 'IPs',
      used: 5,
      after: 5,
      max: 2,
    })
  })

  it('networkChoice gives reasons', () => {
    expect(networkChoice(loc(1, 'home'))).toEqual({ ipv4: null, ipv6: null })
    expect(networkChoice(loc(2, 'rack-2', false, false))).toEqual({
      ipv4: 'No free IPv4 addresses in rack-2',
      ipv6: 'No free IPv6 addresses in rack-2',
    })
  })

  it('createBlocker blocks a location with no free addresses', () => {
    const c = { ...ctx, locations: [loc(1, 'home', false, false)] }
    expect(createBlocker(form({ ipv4: true }), c, { quotaOk: true, existing: [] })).toBe(
      'No free addresses in home. Ask your Nebla admin to add an IP pool.',
    )
  })

  it('createBlocker asks for a family when both are off', () => {
    const opts = { quotaOk: true, existing: [] as string[] }
    expect(createBlocker(form({ ipv4: false, ipv6: false }), ctx, opts)).toBe(
      'Pick IPv4 or IPv6 so the server can be reached.',
    )
    expect(createBlocker(form({ ipv4: false, ipv6: true }), ctx, opts)).toBeNull()
  })

  it('pickFamilies selects the only available family', () => {
    expect(pickFamilies(form({ ipv4: true, ipv6: false }), loc(2, 'rack-2', false, true))).toEqual(
      form({ ipv4: false, ipv6: true }),
    )
    expect(pickFamilies(form({ ipv4: false, ipv6: true }), loc(3, 'v4', true, false))).toEqual(
      form({ ipv4: true, ipv6: false }),
    )
    const both = form({ ipv4: false, ipv6: true })
    expect(pickFamilies(both, loc(1, 'home'))).toEqual(both)
    const none = form({ ipv4: true, ipv6: false })
    expect(pickFamilies(none, loc(4, 'empty', false, false))).toEqual(none)
  })

  it('reconcile keeps a family the user unticked', () => {
    const c = { ...ctx, locations: [loc(2, 'rack-2', false, true)] }
    const picked = pickFamilies(form({ locationId: 2, imageId: 4 }), c.locations[0]!)
    const unticked = reconcile({ ...picked, ipv6: false }, c)
    expect(unticked.ipv6).toBe(false)
    expect(createBlocker(unticked, c, { quotaOk: true, existing: [] })).toBe(
      'Pick IPv4 or IPv6 so the server can be reached.',
    )
  })

  it('createBlocker checks name, quota and user data', () => {
    const opts = { quotaOk: true, existing: [] as string[] }
    expect(createBlocker(form({ name: 'Bad_Name' }), ctx, opts)).toMatch(/lowercase/)
    expect(createBlocker(form(), ctx, { ...opts, existing: ['web1'] })).toMatch(/already/)
    expect(createBlocker(form(), ctx, { ...opts, quotaOk: false })).toMatch(/quota/)
    expect(createBlocker(form({ userData: 'x'.repeat(32 * 1024 + 1) }), ctx, opts)).toMatch(
      /32 KiB/,
    )
  })
})

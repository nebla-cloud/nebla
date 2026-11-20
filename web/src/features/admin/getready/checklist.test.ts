import { beforeEach, describe, expect, it } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { queryClient } from '@/lib/queryClient'
import { createAppRouter } from '@/router'
import { createMemoryHistory } from 'vue-router'
import type { AdminLocation, ServerType } from '@/api/types'
import type { IpPool } from '../ippools/queries'
import type { CatalogEntry } from '../catalog/queries'
import { checklistSteps, isPanelReady, type ChecklistData } from './checklist'

const node = (name: string, status: string) => ({ id: 1, name, status, enabled: true })
const loc = (name: string, statuses: string[]) =>
  ({ id: 1, name, nodes: statuses.map((s, i) => node(`n${i}`, s)) }) as unknown as AdminLocation
const entry = (state: string) =>
  ({ key: 'debian-13', locations: [{ location_id: 1, state }] }) as unknown as CatalogEntry
const type = { id: 1, name: 'small' } as unknown as ServerType
const pool = (over: Partial<IpPool> = {}) =>
  ({
    cidr: '10.0.0.0/24',
    gateway: '10.0.0.1',
    range_start: '10.0.0.10',
    range_end: '10.0.0.12',
    allocated: 0,
    ...over,
  }) as IpPool

const empty: ChecklistData = { locations: [], types: [], pools: [], catalog: [], images: [] }
const step = (d: ChecklistData, key: string) => checklistSteps(d).find((s) => s.key === key)!

describe('checklistSteps', () => {
  it('location done when any location has an online node', () => {
    const s = step(
      { ...empty, locations: [loc('home', ['online', 'online', 'online', 'offline'])] },
      'location',
    )
    expect(s.done).toBe(true)
    expect(s.detail).toBe('home: 3 nodes online')
    expect(step({ ...empty, locations: [loc('home', ['offline'])] }, 'location').done).toBe(false)
    expect(step(empty, 'location').done).toBe(false)
  })

  it('images done when any catalog cell is ready or partial, or any hand-registered image exists', () => {
    expect(step({ ...empty, catalog: [entry('off')] }, 'images').done).toBe(false)
    expect(step({ ...empty, catalog: [entry('building')] }, 'images').done).toBe(false)
    expect(step({ ...empty, catalog: [entry('ready')] }, 'images').done).toBe(true)
    expect(step({ ...empty, catalog: [entry('partial')] }, 'images').done).toBe(true)
    const own = { id: 1, catalog_key: null } as never
    const cat = { id: 2, catalog_key: 'debian-13' } as never
    expect(step({ ...empty, images: [own] }, 'images').done).toBe(true)
    expect(step({ ...empty, images: [cat] }, 'images').done).toBe(false)
  })

  it('types done when at least one server type', () => {
    expect(step(empty, 'types').done).toBe(false)
    expect(step({ ...empty, types: [type] }, 'types').done).toBe(true)
  })

  it('pools done when at least one pool with free addresses', () => {
    expect(step(empty, 'pools').done).toBe(false)
    expect(step({ ...empty, pools: [pool({ allocated: 3 })] }, 'pools').done).toBe(false)
    expect(step({ ...empty, pools: [pool({ allocated: 2 })] }, 'pools').done).toBe(true)
  })

  it('pools count a gateway inside the range as used', () => {
    const p = pool({ gateway: '10.0.0.11', allocated: 2 })
    expect(step({ ...empty, pools: [p] }, 'pools').done).toBe(false)
  })

  it('steps come in order', () => {
    expect(checklistSteps(empty).map((s) => s.key)).toEqual([
      'location',
      'images',
      'types',
      'pools',
    ])
  })

  it('isPanelReady only when all four', () => {
    const all: ChecklistData = {
      locations: [loc('home', ['online'])],
      types: [type],
      pools: [pool()],
      catalog: [entry('ready')],
      images: [],
    }
    expect(isPanelReady(all)).toBe(true)
    expect(isPanelReady({ ...all, types: [] })).toBe(false)
    expect(isPanelReady({ ...all, pools: [] })).toBe(false)
    expect(isPanelReady({ ...all, catalog: [] })).toBe(false)
    expect(isPanelReady({ ...all, locations: [] })).toBe(false)
  })
})

describe('nav flag', () => {
  beforeEach(() => queryClient.clear())

  const hidden = () => {
    const r = createAppRouter(createMemoryHistory())
      .getRoutes()
      .find((x) => x.name === 'admin-get-ready')!
    return r.meta.nav!.hidden!()
  }

  it('follows the cache without visiting the root', async () => {
    const seed = (types: unknown[]) => {
      queryClient.setQueryData(['admin', 'locations'], [loc('home', ['online'])])
      queryClient.setQueryData(['admin', 'server-types'], types)
      queryClient.setQueryData(['admin', 'ip-pools'], [pool()])
      queryClient.setQueryData(['admin', 'catalog'], [entry('ready')])
      queryClient.setQueryData(['admin', 'images'], [])
    }
    seed([])
    await flushPromises()
    expect(hidden()).toBe(false)
    queryClient.setQueryData(['admin', 'server-types'], [type])
    await flushPromises()
    expect(hidden()).toBe(true)
    queryClient.setQueryData(['admin', 'server-types'], [])
    expect(hidden()).toBe(false)
    seed([type])
    queryClient.clear()
    await flushPromises()
    expect(hidden()).toBe(false)
  })
})

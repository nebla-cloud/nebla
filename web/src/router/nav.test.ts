import { describe, expect, it } from 'vitest'
import { defineComponent, h } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import { createAppRouter } from '@/router'
import { navItems } from './nav'

const Stub = defineComponent({ render: () => h('div') })
const router = createRouter({
  history: createMemoryHistory(),
  routes: [
    {
      path: '/a',
      name: 'a',
      component: Stub,
      meta: { side: 'project', nav: { label: 'A', order: 1 } },
    },
    {
      path: '/b',
      name: 'b',
      component: Stub,
      meta: { side: 'project', nav: { label: 'B', order: 2 }, roles: ['owner', 'admin'] },
    },
  ],
})

describe('navItems', () => {
  it('lists a role-gated route for an allowed role', () => {
    expect(navItems(router, 'project', 'admin').filter((i) => !i.soon).map((i) => i.name)).toEqual(['a', 'b'])
  })
  it('hides it for another role', () => {
    expect(navItems(router, 'project', 'member').filter((i) => !i.soon).map((i) => i.name)).toEqual(['a'])
  })
  it('hides it when the role is unknown', () => {
    expect(navItems(router, 'project', undefined).filter((i) => !i.soon).map((i) => i.name)).toEqual(['a'])
  })
})

describe('navItems groups and soon entries', () => {
  const app = () => createAppRouter(createMemoryHistory())

  it('links to Firewalls on the project side', () => {
    const fw = navItems(app(), 'project', 'owner').find((i) => i.label === 'Firewalls')
    expect(fw).toMatchObject({ name: 'firewalls', icon: 'i-lucide-shield' })
    expect(fw?.soon).toBeUndefined()
  })
  it('drops the Volumes soon entry even while the real one is hidden', () => {
    expect(navItems(app(), 'project', 'owner').filter((i) => i.label === 'Volumes')).toEqual([])
  })
  it('drops the soon entry when a real item has the same label', () => {
    const r = createRouter({
      history: createMemoryHistory(),
      routes: [
        {
          path: '/volumes',
          name: 'volumes',
          component: Stub,
          meta: { side: 'project', nav: { label: 'Volumes', order: 12 } },
        },
      ],
    })
    const items = navItems(r, 'project').filter((i) => i.label === 'Volumes')
    expect(items).toHaveLength(1)
    expect(items[0]!.soon).toBeUndefined()
    expect(items[0]!.icon).toBe('i-lucide-circle-dot')
  })
  it('excludes API tokens for members', () => {
    expect(navItems(app(), 'project', 'member').map((i) => i.label)).not.toContain('API tokens')
  })
  it('orders project groups Servers, Access, Project', () => {
    const items = navItems(app(), 'project', 'owner')
    const labels = items.map((i) => i.label)
    expect(labels.indexOf('Servers')).toBeLessThan(labels.indexOf('SSH keys'))
    expect(labels.indexOf('Members')).toBeLessThan(labels.indexOf('Activity'))
    expect(items.find((i) => i.label === 'SSH keys')!.group).toBe('Access')
    expect(items.find((i) => i.label === 'Settings')!.group).toBe('Project')
    expect(items.find((i) => i.label === 'Servers')!.group).toBeNull()
  })
  it('groups the admin side', () => {
    const items = navItems(app(), 'admin')
    expect(items.find((i) => i.label === 'Users')!.group).toBe('People')
    expect(items.find((i) => i.label === 'Settings')!.group).toBe('System')
    expect(items.find((i) => i.label === 'IP pools')!.icon).toBe('i-lucide-network')
  })
})

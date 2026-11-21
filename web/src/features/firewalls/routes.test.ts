import { describe, expect, it } from 'vitest'
import { createMemoryHistory } from 'vue-router'
import { createAppRouter } from '@/router'
import { navItems } from '@/router/nav'

describe('firewalls routes', () => {
  it('replaces the Soon entry with a real Firewalls nav item', () => {
    const router = createAppRouter(createMemoryHistory())
    const items = navItems(router, 'project', 'owner').filter((i) => i.label === 'Firewalls')
    expect(items).toHaveLength(1)
    expect(items[0]!.soon).toBeUndefined()
    expect(items[0]!.icon).toBe('i-lucide-shield')
  })

  it('resolves the list, detail and server tab', () => {
    const router = createAppRouter(createMemoryHistory())
    expect(router.resolve('/p/1/firewalls').name).toBe('firewalls')
    expect(router.resolve('/p/1/firewalls/3').name).toBe('firewall')
    expect(router.resolve('/p/1/servers/2/firewalls').name).toBe('server-firewalls')
  })
})

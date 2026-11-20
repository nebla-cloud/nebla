import { describe, expect, it } from 'vitest'
import { createMemoryHistory } from 'vue-router'
import { createAppRouter } from '@/router'
import { navItems } from '@/router/nav'

describe('locations routes', () => {
  it('shows Locations in the admin sidebar', () => {
    const router = createAppRouter(createMemoryHistory())
    expect(navItems(router, 'admin').map((i) => i.label)).toContain('Locations')
  })
})

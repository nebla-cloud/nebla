import { describe, expect, it } from 'vitest'
import { createMemoryHistory } from 'vue-router'
import { createAppRouter } from '@/router'
import { navItems } from '@/router/nav'

describe('ssh keys routes', () => {
  it('shows SSH keys in the project sidebar', () => {
    const router = createAppRouter(createMemoryHistory())
    expect(navItems(router, 'project').map((i) => i.label)).toContain('SSH keys')
  })
})

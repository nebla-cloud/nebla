import { describe, expect, it } from 'vitest'
import { defineComponent, h } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
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
    expect(navItems(router, 'project', 'admin').map((i) => i.name)).toEqual(['a', 'b'])
  })
  it('hides it for another role', () => {
    expect(navItems(router, 'project', 'member').map((i) => i.name)).toEqual(['a'])
  })
  it('hides it when the role is unknown', () => {
    expect(navItems(router, 'project', undefined).map((i) => i.name)).toEqual(['a'])
  })
})

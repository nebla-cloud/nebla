import { mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { describe, expect, it } from 'vitest'
import ListRow from './ListRow.vue'

const router = createRouter({
  history: createMemoryHistory(),
  routes: [{ path: '/x', component: { template: '<div />' } }],
})

describe('ListRow', () => {
  it('renders a linked row as li > a', () => {
    const w = mount(ListRow, {
      props: { to: '/x' },
      slots: { leading: 'L', default: 'Title', meta: 'M', trailing: 'T' },
      global: { plugins: [router] },
    })
    expect(w.element.tagName).toBe('LI')
    const a = w.find('li > a')
    expect(a.exists()).toBe(true)
    expect(a.attributes('href')).toBe('/x')
    expect(a.text()).toContain('Title')
    expect(a.text()).toContain('T')
  })

  it('truncates the title inside a shrinkable flex child', () => {
    const w = mount(ListRow, { slots: { default: 'Title' } })
    const title = w.find('[data-title]')
    expect(title.classes()).toContain('truncate')
    expect(title.classes()).toContain('min-w-0')
    expect(title.element.parentElement?.className).toContain('flex')
  })

  it('renders a plain row without a link', () => {
    const w = mount(ListRow, { slots: { default: 'Title', meta: 'M' } })
    expect(w.element.tagName).toBe('LI')
    expect(w.find('a').exists()).toBe(false)
    expect(w.text()).toContain('Title')
    expect(w.text()).toContain('M')
  })
})

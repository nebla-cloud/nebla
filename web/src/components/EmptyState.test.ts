import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import EmptyState from './EmptyState.vue'

describe('EmptyState', () => {
  it('renders title, body, actions and aside without a dashed border', () => {
    const w = mount(EmptyState, {
      props: { title: 'Nothing yet', body: 'Make one' },
      slots: { default: '<button>Go</button>', aside: '<div data-aside>art</div>' },
    })
    expect(w.element.tagName).toBe('SECTION')
    expect(w.find('h2').text()).toBe('Nothing yet')
    expect(w.text()).toContain('Make one')
    expect(w.find('button').text()).toBe('Go')
    expect(w.find('[data-aside]').exists()).toBe(true)
    expect(w.html()).not.toContain('border-dashed')
  })

  it('works without an aside', () => {
    const w = mount(EmptyState, { props: { title: 'T', body: 'B' } })
    expect(w.find('[data-aside]').exists()).toBe(false)
    expect(w.find('h2').text()).toBe('T')
  })
})

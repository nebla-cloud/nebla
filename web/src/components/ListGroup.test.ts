import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import ListGroup from './ListGroup.vue'

describe('ListGroup', () => {
  it('keeps list semantics with a label', () => {
    const w = mount(ListGroup, { props: { label: 'Servers' }, slots: { default: '<li>a</li>' } })
    expect(w.element.tagName).toBe('UL')
    expect(w.attributes('aria-label')).toBe('Servers')
    expect(w.find('li').exists()).toBe(true)
  })
})

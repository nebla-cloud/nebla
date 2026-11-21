import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import Panel from './Panel.vue'

describe('Panel', () => {
  it('renders an h2 title in a section by default', () => {
    const w = mount(Panel, { props: { title: 'Details' }, slots: { default: 'body', 'header-actions': '<i>act</i>' } })
    expect(w.element.tagName).toBe('SECTION')
    expect(w.find('h2.nb-h2').text()).toBe('Details')
    expect(w.text()).toContain('act')
    expect(w.text()).toContain('body')
  })

  it('renders a div and no h2 without a title', () => {
    const w = mount(Panel, { props: { as: 'div' }, slots: { default: 'body' } })
    expect(w.element.tagName).toBe('DIV')
    expect(w.find('h2').exists()).toBe(false)
  })
})

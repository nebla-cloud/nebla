import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import StatusBadge from './StatusBadge.vue'

describe('StatusBadge', () => {
  it('shows the label once, with a decorative glyph', () => {
    const w = mount(StatusBadge, { props: { status: 'running' } })
    expect(w.text()).toBe('Running')
    const svg = w.find('svg')
    expect(svg.attributes('aria-hidden')).toBe('true')
    expect(svg.attributes('role')).toBeUndefined()
    expect(svg.find('title').exists()).toBe(false)
  })
})

import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import OsBadge from './OsBadge.vue'

describe('OsBadge', () => {
  it('shows the first letter, uppercased, and is hidden from assistive tech', () => {
    const w = mount(OsBadge, { props: { name: 'debian' } })
    expect(w.text()).toBe('D')
    expect(w.attributes('aria-hidden')).toBe('true')
  })

  it('handles an empty name', () => {
    expect(mount(OsBadge, { props: { name: '' } }).text()).toBe('')
  })
})

import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import LabelChips from './LabelChips.vue'

describe('LabelChips', () => {
  it('renders ul[aria-label=Labels] > li, sorted, with a +N overflow item', () => {
    const w = mount(LabelChips, { props: { labels: { b: '2', a: '1', c: '', d: '4' }, max: 2 } })
    const items = w.findAll('ul[aria-label="Labels"] > li')
    expect(items.map((i) => i.text())).toEqual(['a=1', 'b=2', '+2'])
  })

  it('renders nothing without labels', () => {
    expect(mount(LabelChips, { props: { labels: {} } }).find('ul').exists()).toBe(false)
  })
})

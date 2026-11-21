import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import Sparkline from './Sparkline.vue'

describe('Sparkline', () => {
  it('draws a polyline, skipping nulls', () => {
    const w = mount(Sparkline, {
      props: {
        points: [
          [0, 0],
          [1, null],
          [2, 10],
        ],
        width: 100,
        height: 20,
      },
    })
    const svg = w.find('svg')
    expect(svg.attributes('aria-hidden')).toBe('true')
    expect(svg.attributes('viewBox')).toBe('0 0 100 20')
    const pts = w.find('polyline').attributes('points')!.trim().split(/\s+/)
    expect(pts).toHaveLength(2)
    expect(pts[0]).toMatch(/^0,/)
    expect(pts[1]).toMatch(/^100,/)
  })

  it('uses the tone token for the stroke', () => {
    const w = mount(Sparkline, {
      props: {
        points: [
          [0, 1],
          [1, 2],
        ],
        width: 100,
        height: 20,
        tone: 'run',
      },
    })
    expect(w.find('polyline').attributes('stroke')).toBe('var(--nb-run)')
  })

  it('defaults to the busy token', () => {
    const w = mount(Sparkline, {
      props: {
        points: [
          [0, 1],
          [1, 2],
        ],
        width: 100,
        height: 20,
      },
    })
    expect(w.find('polyline').attributes('stroke')).toBe('var(--nb-busy)')
  })

  it('draws nothing for fewer than 2 points', () => {
    const one = mount(Sparkline, {
      props: {
        points: [
          [0, 1],
          [1, null],
        ],
        width: 100,
        height: 20,
      },
    })
    expect(one.find('polyline').exists()).toBe(false)
    const none = mount(Sparkline, { props: { points: [], width: 100, height: 20 } })
    expect(none.find('polyline').exists()).toBe(false)
  })
})

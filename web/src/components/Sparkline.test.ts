import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import Sparkline from './Sparkline.vue'

const pts = (...v: (number | null)[]) => v.map((y, i) => [i, y] as [number, number | null])

describe('Sparkline', () => {
  it('draws a smooth line and a gradient area per line', () => {
    const w = mount(Sparkline, {
      props: { lines: [{ points: pts(0, null, 10, 4), tone: 'run' }], width: 100, height: 20 },
    })
    expect(w.find('svg').attributes('aria-hidden')).toBe('true')
    expect(w.find('svg').attributes('viewBox')).toBe('0 0 100 20')
    const line = w.find('path[data-line]')
    expect(line.attributes('stroke')).toBe('var(--nb-run)')
    expect(line.attributes('d')).toMatch(/^M0,/)
    // Three points: curved segments, not straight ones.
    expect(line.attributes('d')).toContain('C')
    const fill = w.find('path[data-area]').attributes('fill')!
    const id = fill.match(/^url\(#(.+)\)$/)![1]
    expect(w.find(`linearGradient[id="${id}"] stop`).attributes('style')).toContain(
      'var(--nb-run)',
    )
  })

  it('two sparklines on a page use different gradient ids', () => {
    const props = { lines: [{ points: pts(1, 2), tone: 'busy' as const }], width: 10, height: 10 }
    const page = mount(
      defineComponent({ render: () => h('div', [h(Sparkline, props), h(Sparkline, props)]) }),
    )
    const ids = page.findAll('linearGradient').map((g) => g.attributes('id'))
    expect(ids).toHaveLength(2)
    expect(ids[0]).not.toBe(ids[1])
  })

  it('max fixes the scale: 50 of 100 sits mid-height', () => {
    const w = mount(Sparkline, {
      props: { lines: [{ points: pts(50, 50), tone: 'busy' }], width: 100, height: 20, max: 100 },
    })
    expect(w.find('path[data-line]').attributes('d')).toMatch(/^M0,10[^\d.]/)
  })

  it('lines share one scale', () => {
    const w = mount(Sparkline, {
      props: {
        lines: [
          { points: pts(10, 10), tone: 'run' },
          { points: pts(5, 5), tone: 'err' },
        ],
        width: 100,
        height: 20,
      },
    })
    const [hi, lo] = w.findAll('path[data-line]').map((p) => p.attributes('d')!)
    expect(hi).toMatch(/^M0,0[^\d.]/)
    expect(lo).toMatch(/^M0,10[^\d.]/)
  })

  it('skips a line with fewer than 2 points, keeps the other', () => {
    const w = mount(Sparkline, {
      props: {
        lines: [
          { points: pts(1, 2), tone: 'run' },
          { points: pts(null, 3), tone: 'err' },
        ],
        width: 10,
        height: 10,
      },
    })
    expect(w.findAll('path[data-line]')).toHaveLength(1)
  })

  it('draws nothing without points', () => {
    const w = mount(Sparkline, { props: { lines: [{ points: [], tone: 'busy' }], width: 10, height: 10 } })
    expect(w.find('path').exists()).toBe(false)
  })

  it('never uses hex colours', () => {
    const w = mount(Sparkline, {
      props: { lines: [{ points: pts(1, 2), tone: 'busy' }], width: 10, height: 10 },
    })
    expect(w.html()).not.toMatch(/#[0-9a-f]{6}/i)
  })
})

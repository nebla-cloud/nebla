import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import MetricChart from './MetricChart.vue'
import { formatTime, type MetricRow } from './metrics'

// Stubs record the attrs each Unovis wrapper gets, so tests can read config and call callbacks.
type Attrs = Record<string, any> // eslint-disable-line @typescript-eslint/no-explicit-any
const seen = vi.hoisted(() => ({ props: {} as Record<string, Attrs[]> }))

function stubModule(name: string) {
  return async () => {
    const { defineComponent, h } = await import('vue')
    return {
      default: defineComponent({
        name,
        inheritAttrs: false,
        setup(_, { attrs, slots }) {
          return () => {
            ;(seen.props[name] ??= []).push({ ...attrs })
            return h('div', { 'data-stub': name }, slots.default?.())
          }
        },
      }),
    }
  }
}
vi.mock('@unovis/vue/containers/xy-container', stubModule('VisXYContainer'))
vi.mock('@unovis/vue/components/area', stubModule('VisArea'))
vi.mock('@unovis/vue/components/line', stubModule('VisLine'))
vi.mock('@unovis/vue/components/axis', stubModule('VisAxis'))
vi.mock('@unovis/vue/components/crosshair', stubModule('VisCrosshair'))
vi.mock('@unovis/vue/components/tooltip', stubModule('VisTooltip'))

const last = (n: string) => seen.props[n]!.at(-1)!
const lastOf = (n: string, count: number) => seen.props[n]!.slice(-count)

const network = {
  title: 'Network',
  unit: 'rate' as const,
  rows: [
    { t: 0, v: [100, 50] },
    { t: 60, v: [2048, 10] },
  ] as MetricRow[],
  series: [
    { label: 'In', tone: 'run' as const },
    { label: 'Out', tone: 'err' as const },
  ],
}
const cpu = {
  title: 'CPU',
  unit: 'percent' as const,
  rows: [
    { t: 0, v: [10] },
    { t: 60, v: [40] },
  ] as MetricRow[],
  series: [{ label: 'CPU', tone: 'busy' as const }],
}

enableAutoUnmount(afterEach)
beforeEach(() => {
  seen.props = {}
})

// The crosshair config the given chart was last rendered with.
function crosshairOf(w: ReturnType<typeof mount>): Attrs {
  return w.findComponent({ name: 'VisCrosshair' }).vm.$attrs as Attrs
}
const header = (w: ReturnType<typeof mount>) => w.find('[data-value]').text()

describe('MetricChart', () => {
  it('colours are tokens, not hex', async () => {
    const w = mount(MetricChart, { props: network })
    await flushPromises()
    const line = last('VisLine')
    const colors = [0, 1].map((i) =>
      typeof line.color === 'function' ? line.color(network.rows[0], i) : line.color[i],
    )
    expect(colors).toEqual(['var(--nb-run)', 'var(--nb-err)'])
    const defs = last('VisXYContainer').svgDefs as string
    const areas = lastOf('VisArea', 2)
    ;['--nb-run', '--nb-err'].forEach((token, i) => {
      const id = (areas[i]!.color as string).match(/^url\(#(.+)\)$/)![1]
      const grad = defs.slice(defs.indexOf(`id="${id}"`))
      expect(grad.slice(0, grad.indexOf('</linearGradient>'))).toContain(`var(${token})`)
    })
    expect(w.html()).not.toMatch(/#[0-9a-f]{6}/i)
    expect(defs).not.toMatch(/#[0-9a-f]{6}/i)
  })

  it('cpu domain is 0–100, rate floors at 1 KB/s', async () => {
    mount(MetricChart, { props: cpu })
    await flushPromises()
    expect(last('VisXYContainer').yDomain).toEqual([0, 100])
    mount(MetricChart, { props: { ...network, rows: [{ t: 0, v: [0, 0] }] } })
    await flushPromises()
    expect(last('VisXYContainer').yDomain).toEqual([0, 1024])
  })

  it('one area per series and gaps stay gaps', async () => {
    mount(MetricChart, { props: network })
    await flushPromises()
    const areas = lastOf('VisArea', 2)
    expect(seen.props.VisArea!.length).toBeGreaterThanOrEqual(2)
    expect(areas[0]!.y({ t: 0, v: [null, 1] })).toBeUndefined()
    expect(areas[1]!.y({ t: 0, v: [null, 1] })).toBe(1)
  })

  it('header shows latest values and stats', async () => {
    const c = mount(MetricChart, { props: cpu })
    await flushPromises()
    expect(header(c)).toBe('40%')
    expect(c.findAll('[data-stats] > *').map((s) => s.text())).toEqual([
      'min 10%',
      'avg 25%',
      'max 40%',
    ])
    const n = mount(MetricChart, { props: network })
    await flushPromises()
    expect(header(n)).toBe('2.0 KB/s')
    expect(n.find('[data-value]').element.nextElementSibling!.textContent!.trim()).toBe(
      'in / 10 B/s out',
    )
  })

  it('hovering moves the header, leaving restores it', async () => {
    const c = mount(MetricChart, { props: cpu })
    await flushPromises()
    crosshairOf(c).onCrosshairMove(0)
    await flushPromises()
    expect(header(c)).toBe('10%')
    crosshairOf(c).onCrosshairMove(undefined)
    await flushPromises()
    expect(header(c)).toBe('40%')
  })

  it('hover is synced across charts', async () => {
    const c = mount(MetricChart, { props: cpu })
    const n = mount(MetricChart, { props: network })
    await flushPromises()
    crosshairOf(c).onCrosshairMove(0)
    await flushPromises()
    expect(crosshairOf(n).forceShowAt).toBe(0)
    expect(header(n)).toBe('100 B/s')
    expect(crosshairOf(c).forceShowAt).toBeUndefined()
    // Only the hovered chart shows a tooltip.
    expect(crosshairOf(n).template(network.rows[0])).toBe('')
    expect(crosshairOf(c).template(cpu.rows[0])).not.toBe('')
  })

  it('unmounting the hovered chart releases the others', async () => {
    const c = mount(MetricChart, { props: cpu })
    const n = mount(MetricChart, { props: network })
    await flushPromises()
    crosshairOf(c).onCrosshairMove(0)
    await flushPromises()
    c.unmount()
    await flushPromises()
    expect(crosshairOf(n).forceShowAt).toBeUndefined()
    expect(header(n)).toBe('2.0 KB/s')
  })

  it('mouseleave clears hover', async () => {
    const c = mount(MetricChart, { props: cpu })
    await flushPromises()
    crosshairOf(c).onCrosshairMove(0)
    await flushPromises()
    await c.find('[role=img]').trigger('mouseleave')
    expect(header(c)).toBe('40%')
  })

  it('panel, legend and accessible name', async () => {
    const w = mount(MetricChart, { props: network })
    await flushPromises()
    expect(w.find('section h2').text()).toBe('Network')
    const swatches = w.findAll('li span[aria-hidden]').map((s) => s.attributes('style'))
    expect(swatches).toEqual(['background: var(--nb-run);', 'background: var(--nb-err);'])
    expect(w.find('[role=img]').attributes('aria-label')).toBe('Network over time')
    expect(w.html()).not.toMatch(/ink-\d/)
    const c = mount(MetricChart, { props: cpu })
    await flushPromises()
    expect(c.find('li').exists()).toBe(false)
  })

  it('tooltip lists time and every value', async () => {
    const w = mount(MetricChart, { props: network })
    await flushPromises()
    crosshairOf(w).onCrosshairMove(60)
    await flushPromises()
    const html = crosshairOf(w).template(network.rows[1]) as string
    expect(html).toContain(formatTime(60))
    expect(html).toContain('2.0 KB/s')
    expect(html).toContain('10 B/s')
  })
})

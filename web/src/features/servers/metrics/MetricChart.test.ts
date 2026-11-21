import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { effectScope } from 'vue'
import { useColorMode } from '@vueuse/core'
import MetricChart from './MetricChart.vue'

type Opts = {
  axes: { stroke: string; grid: { stroke: string } }[]
  series: { stroke?: string }[]
}
const built = vi.hoisted(() => [] as Opts[])

vi.mock('uplot', () => ({
  default: class {
    constructor(opts: Opts) {
      built.push(opts)
    }
    destroy() {}
    setData() {}
    setSize() {}
  },
}))

const vars = {
  '--ui-text-muted': '#5B6470',
  '--ui-border': '#DCE1E6',
  '--nb-busy': '#3A86D6',
  '--nb-run': '#1E9E74',
  '--nb-err': '#D9547D',
}
const dark = {
  '--ui-text-muted': '#98A2AE',
  '--ui-border': '#2A3039',
  '--nb-busy': '#4C9BEA',
  '--nb-run': '#2FC795',
  '--nb-err': '#EE6F95',
}
let style: HTMLStyleElement

enableAutoUnmount(afterEach)

function css(v: Record<string, string>) {
  return Object.entries(v)
    .map(([k, c]) => `${k}: ${c};`)
    .join(' ')
}

const network = {
  title: 'Network',
  unit: 'rate' as const,
  data: [[1], [3], [4]] as [number[], number[], number[]],
  series: [
    { label: 'In', tone: 'run' as const },
    { label: 'Out', tone: 'err' as const },
  ],
}

beforeEach(() => {
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      disconnect() {}
    },
  )
  built.length = 0
  localStorage.clear()
  document.documentElement.classList.remove('dark')
  style = document.createElement('style')
  style.textContent = `:root { ${css(vars)} } .dark { ${css(dark)} }`
  document.head.append(style)
})
afterEach(() => {
  vi.unstubAllGlobals()
  style.remove()
  document.documentElement.classList.remove('dark')
  document.body.innerHTML = ''
})

describe('MetricChart', () => {
  it('draws with the token colours', async () => {
    mount(MetricChart, { props: network, attachTo: document.body })
    await vi.waitFor(() => expect(built).toHaveLength(1))
    const o = built[0]!
    expect(o.axes[0]!.stroke).toBe('#5B6470')
    expect(o.axes[0]!.grid.stroke).toBe('#DCE1E6')
    expect(o.series.slice(1).map((s) => s.stroke)).toEqual(['#1E9E74', '#D9547D'])
  })

  it('cpu uses busy, disk pairs busy and err', async () => {
    mount(MetricChart, {
      props: {
        title: 'Disk',
        unit: 'rate',
        data: [[1], [1], [2]],
        series: [
          { label: 'Read', tone: 'busy' },
          { label: 'Write', tone: 'err' },
        ],
      },
    })
    await vi.waitFor(() => expect(built).toHaveLength(1))
    expect(built[0]!.series.slice(1).map((s) => s.stroke)).toEqual(['#3A86D6', '#D9547D'])
  })

  it('falls back when the tokens are missing', async () => {
    style.remove()
    mount(MetricChart, { props: network })
    await vi.waitFor(() => expect(built).toHaveLength(1))
    for (const s of built[0]!.series.slice(1)) expect(s.stroke).toMatch(/^#[0-9A-F]{6}$/i)
    expect(built[0]!.axes[0]!.stroke).toMatch(/^#[0-9A-F]{6}$/i)
  })

  it('recomputes when the colour mode changes', async () => {
    mount(MetricChart, { props: network })
    await vi.waitFor(() => expect(built).toHaveLength(1))
    const scope = effectScope()
    scope.run(() => {
      useColorMode().value = 'dark'
    })
    await flushPromises()
    await vi.waitFor(() => expect(built).toHaveLength(2))
    expect(built[1]!.series.slice(1).map((s) => s.stroke)).toEqual(['#2FC795', '#EE6F95'])
    expect(built[1]!.axes[0]!.grid.stroke).toBe('#2A3039')
    scope.stop()
  })

  it('sits in a panel with token legend swatches and an img role', async () => {
    const w = mount(MetricChart, { props: network })
    await flushPromises()
    expect(w.find('section h2').text()).toBe('Network')
    const swatches = w.findAll('li span[aria-hidden]').map((s) => s.attributes('style'))
    expect(swatches).toEqual(['background: var(--nb-run);', 'background: var(--nb-err);'])
    expect(w.find('[role=img]').attributes('aria-label')).toBe('Network over time')
    expect(w.html()).not.toMatch(/ink-\d/)
  })
})

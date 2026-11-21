import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import Step from './Step.vue'
import StepList from './StepList.vue'
import Meter from './Meter.vue'
import MetricCard from './MetricCard.vue'
import TerminalBlock from './TerminalBlock.vue'
import LabelChip from './LabelChip.vue'

describe('StepList and Step', () => {
  it('renders an ol of li with data-step and data-state', () => {
    const w = mount(StepList, {
      slots: {
        default: [
          '<li data-step="1" data-state="done">x</li>',
        ],
      },
    })
    expect(w.element.tagName).toBe('OL')
    const s = mount(Step, { props: { n: 2, state: 'current', title: 'Image' }, slots: { default: 'body' } })
    expect(s.element.tagName).toBe('LI')
    expect(s.attributes('data-step')).toBe('2')
    expect(s.attributes('data-state')).toBe('current')
    expect(s.text()).toContain('Image')
    expect(s.text()).toContain('2')
  })

  it('shows a check, not the number, when done', () => {
    const s = mount(Step, { props: { n: 3, state: 'done', title: 'Type' } })
    expect(s.find('svg, [class*="i-lucide-check"], [aria-label="Done"]').exists()).toBe(true)
  })
})

describe('Meter', () => {
  it('exposes role=meter with value, max and label', () => {
    const w = mount(Meter, { props: { value: 2, max: 8, label: 'vCPU' }, slots: { default: 'vCPU, 2 of 8' } })
    const m = w.get('[role="meter"]')
    expect(m.attributes('aria-valuenow')).toBe('2')
    expect(m.attributes('aria-valuemax')).toBe('8')
    expect(m.attributes('aria-label')).toBe('vCPU')
    expect(w.text()).toContain('vCPU, 2 of 8')
  })

  it('clamps the fill to the track', () => {
    const w = mount(Meter, { props: { value: 20, max: 8, label: 'x' } })
    expect(w.get('[data-fill]').attributes('style')).toContain('width: 100%')
  })
})

describe('MetricCard', () => {
  it('renders label, tabular value, unit and chart slot', () => {
    const w = mount(MetricCard, {
      props: { label: 'CPU', value: '42', unit: '%' },
      slots: { chart: '<i data-chart />' },
    })
    expect(w.text()).toContain('CPU')
    expect(w.get('[data-value]').classes()).toContain('tabular-nums')
    expect(w.text()).toContain('%')
    expect(w.find('[data-chart]').exists()).toBe(true)
  })
})

describe('TerminalBlock', () => {
  it('renders a caption and a div by default, pre when asked', () => {
    const w = mount(TerminalBlock, { props: { caption: 'user-data' }, slots: { default: '#cloud-config' } })
    expect(w.text()).toContain('user-data')
    expect(w.text()).toContain('#cloud-config')
    expect(w.find('pre').exists()).toBe(false)
    const p = mount(TerminalBlock, { props: { as: 'pre' }, slots: { default: 'x' } })
    expect(p.find('pre').exists()).toBe(true)
  })
})

describe('LabelChip', () => {
  it('shows k=v', () => {
    const w = mount(LabelChip, { props: { k: 'env', v: 'prod' } })
    expect(w.text()).toBe('env=prod')
  })

  it('shows only the key for an empty value', () => {
    expect(mount(LabelChip, { props: { k: 'env', v: '' } }).text()).toBe('env')
  })
})

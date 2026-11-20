import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import StatusArch from './StatusArch.vue'
import StatusBadge from './StatusBadge.vue'

describe('StatusArch', () => {
  it('dashes the busy arch and labels it', () => {
    const svg = mount(StatusArch, { props: { status: 'starting' } }).find('svg')
    expect(svg.attributes('aria-label')).toBe('Starting')
    expect(svg.attributes('role')).toBe('img')
    expect(svg.find('path').attributes('stroke-dasharray')).toBeTruthy()
  })

  it('draws a solid arch for running', () => {
    const path = mount(StatusArch, { props: { status: 'running' } }).find('path')
    expect(path.attributes('stroke-dasharray')).toBeUndefined()
    expect(path.attributes('stroke')).toBe('var(--nb-run)')
  })

  it('labels off', () => {
    const svg = mount(StatusArch, { props: { status: 'off' } }).find('svg')
    expect(svg.attributes('aria-label')).toBe('Off')
  })
})

describe('StatusBadge', () => {
  it('shows the label', () => {
    expect(mount(StatusBadge, { props: { status: 'running' } }).text()).toContain('Running')
  })
})

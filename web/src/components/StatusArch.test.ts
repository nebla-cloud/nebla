// Copyright (C) 2026 The Nebla contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

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

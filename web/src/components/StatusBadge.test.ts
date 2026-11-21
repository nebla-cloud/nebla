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

  it('gives off a visible border and error a tinted background', () => {
    const off = mount(StatusBadge, { props: { status: 'off' } }).classes()
    expect(off).toEqual(expect.arrayContaining(['border-(--ui-border)', 'text-toned']))
    const err = mount(StatusBadge, { props: { status: 'error' } }).classes()
    expect(err).toEqual(expect.arrayContaining(['bg-error/10', 'text-error']))
  })
})

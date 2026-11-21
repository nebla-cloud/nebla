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
import Panel from './Panel.vue'

describe('Panel', () => {
  it('renders an h2 title in a section by default', () => {
    const w = mount(Panel, { props: { title: 'Details' }, slots: { default: 'body', 'header-actions': '<i>act</i>' } })
    expect(w.element.tagName).toBe('SECTION')
    expect(w.find('h2.nb-h2').text()).toBe('Details')
    expect(w.text()).toContain('act')
    expect(w.text()).toContain('body')
  })

  it('renders a div and no h2 without a title', () => {
    const w = mount(Panel, { props: { as: 'div' }, slots: { default: 'body' } })
    expect(w.element.tagName).toBe('DIV')
    expect(w.find('h2').exists()).toBe(false)
  })
})

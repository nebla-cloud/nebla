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
import ListGroup from './ListGroup.vue'

describe('ListGroup', () => {
  it('keeps list semantics with a label', () => {
    const w = mount(ListGroup, { props: { label: 'Servers' }, slots: { default: '<li>a</li>' } })
    expect(w.element.tagName).toBe('UL')
    expect(w.attributes('aria-label')).toBe('Servers')
    expect(w.find('li').exists()).toBe(true)
  })

  it('puts a head row above the list, outside the ul', () => {
    const w = mount(ListGroup, {
      props: { label: 'Servers' },
      slots: { head: '<div data-head>Name</div>', default: '<li>a</li>' },
    })
    expect(w.element.tagName).toBe('DIV')
    expect(w.find('[data-head]').element.nextElementSibling?.tagName).toBe('UL')
    expect(w.find('ul').attributes('aria-label')).toBe('Servers')
    expect(w.findAll('ul [data-head]')).toHaveLength(0)
  })
})

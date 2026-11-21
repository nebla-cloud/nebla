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
import EmptyState from './EmptyState.vue'

describe('EmptyState', () => {
  it('renders title, body, actions and aside without a dashed border', () => {
    const w = mount(EmptyState, {
      props: { title: 'Nothing yet', body: 'Make one' },
      slots: { default: '<button>Go</button>', aside: '<div data-aside>art</div>' },
    })
    expect(w.element.tagName).toBe('SECTION')
    expect(w.find('h2').text()).toBe('Nothing yet')
    expect(w.text()).toContain('Make one')
    expect(w.find('button').text()).toBe('Go')
    expect(w.find('[data-aside]').exists()).toBe(true)
    expect(w.html()).not.toContain('border-dashed')
  })

  it('works without an aside', () => {
    const w = mount(EmptyState, { props: { title: 'T', body: 'B' } })
    expect(w.find('[data-aside]').exists()).toBe(false)
    expect(w.find('h2').text()).toBe('T')
  })
})

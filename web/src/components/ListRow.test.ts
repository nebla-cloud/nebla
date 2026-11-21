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
import { createMemoryHistory, createRouter } from 'vue-router'
import { describe, expect, it } from 'vitest'
import ListRow from './ListRow.vue'

const router = createRouter({
  history: createMemoryHistory(),
  routes: [{ path: '/x', component: { template: '<div />' } }],
})

describe('ListRow', () => {
  it('renders a linked row as li > a', () => {
    const w = mount(ListRow, {
      props: { to: '/x' },
      slots: { leading: 'L', default: 'Title', meta: 'M', trailing: 'T' },
      global: { plugins: [router] },
    })
    expect(w.element.tagName).toBe('LI')
    const a = w.find('li > a')
    expect(a.exists()).toBe(true)
    expect(a.attributes('href')).toBe('/x')
    expect(a.text()).toContain('Title')
    expect(a.text()).toContain('T')
  })

  it('truncates the title inside a shrinkable flex child', () => {
    const w = mount(ListRow, { slots: { default: 'Title' } })
    const title = w.find('[data-title]')
    expect(title.classes()).toContain('truncate')
    expect(title.classes()).toContain('min-w-0')
    expect(title.element.parentElement?.className).toContain('flex')
  })

  it('renders a plain row without a link', () => {
    const w = mount(ListRow, { slots: { default: 'Title', meta: 'M' } })
    expect(w.element.tagName).toBe('LI')
    expect(w.find('a').exists()).toBe(false)
    expect(w.text()).toContain('Title')
    expect(w.text()).toContain('M')
  })
})

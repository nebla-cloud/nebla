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
import PageHeader from './PageHeader.vue'

describe('PageHeader', () => {
  it('renders the h1, description, meta and actions', () => {
    const w = mount(PageHeader, {
      props: { title: 'Servers', description: 'All of them' },
      slots: { meta: '<span>beta</span>', actions: '<button>New</button>' },
    })
    expect(w.find('header h1').text()).toBe('Servers')
    expect(w.find('h1').classes()).toContain('truncate')
    expect(w.text()).toContain('All of them')
    expect(w.text()).toContain('beta')
    expect(w.find('button').text()).toBe('New')
  })
})

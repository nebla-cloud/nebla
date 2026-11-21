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
import LabelChips from './LabelChips.vue'

describe('LabelChips', () => {
  it('renders ul[aria-label=Labels] > li, sorted, with a +N overflow item', () => {
    const w = mount(LabelChips, { props: { labels: { b: '2', a: '1', c: '', d: '4' }, max: 2 } })
    const items = w.findAll('ul[aria-label="Labels"] > li')
    expect(items.map((i) => i.text())).toEqual(['a=1', 'b=2', '+2'])
  })

  it('renders nothing without labels', () => {
    expect(mount(LabelChips, { props: { labels: {} } }).find('ul').exists()).toBe(false)
  })
})

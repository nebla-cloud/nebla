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

import { afterEach, describe, expect, it } from 'vitest'
import { defineComponent, h } from 'vue'
import { mountApp } from '@/test/mount'
import ProtectedTip from './ProtectedTip.vue'

afterEach(() => {
  document.body.innerHTML = ''
})

const wrap = (text?: string) =>
  defineComponent({
    render: () => h(ProtectedTip, { text }, () => h('button', { disabled: true }, 'Delete')),
  })

describe('ProtectedTip', () => {
  it('wraps the button in a focusable span when it has text', () => {
    const { wrapper } = mountApp(wrap('Why not'))
    const span = wrapper.find('span')
    expect(span.attributes('tabindex')).toBe('0')
    expect(span.find('button').exists()).toBe(true)
  })

  it('adds no tab stop without text', () => {
    const { wrapper } = mountApp(wrap())
    expect(wrapper.find('span').attributes('tabindex')).toBeUndefined()
  })
})

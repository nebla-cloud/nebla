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

import { describe, expect, it } from 'vitest'
import UInput from '@nuxt/ui/components/Input.vue'
import UTextarea from '@nuxt/ui/components/Textarea.vue'
import { mountApp } from '@/test/mount'

describe('field borders', () => {
  it.each([
    ['UInput', UInput, 'input'],
    ['UTextarea', UTextarea, 'textarea'],
  ])('%s uses the --ui-border ring', (_n, comp, tag) => {
    const { wrapper } = mountApp(comp, { props: { modelValue: '' } })
    const cls = wrapper.find(tag).classes()
    expect(cls).toContain('ring-(color:--ui-border)')
    expect(cls).not.toContain('ring-accented')
    wrapper.unmount()
  })
})

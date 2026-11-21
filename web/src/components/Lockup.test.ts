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
import { mount } from '@vue/test-utils'
import { resetSession, useSession } from '@/lib/session'
import type { UiManifest } from '@/api/types'
import Lockup from './Lockup.vue'

function brand(name: string) {
  useSession().manifest.value = {
    branding: { product_name: name, logo_url: '', primary_color: '' },
  } as UiManifest
}

describe('Lockup', () => {
  afterEach(() => resetSession())

  it('renders the Nebla lockup as a labelled image, not text', () => {
    brand('Nebla')
    const w = mount(Lockup)
    expect(w.find('[aria-label="Nebla"]').exists()).toBe(true)
    expect(w.find('svg').exists()).toBe(true)
    expect(w.text()).not.toContain('Nebla')
  })

  it('gives each instance its own gradient id', () => {
    brand('Nebla')
    const w = mount({ components: { Lockup }, template: '<div><Lockup /><Lockup /></div>' })
    const [a, b] = w.findAll('linearGradient').map((g) => g.attributes('id'))
    expect(a).toBeTruthy()
    expect(a).not.toBe(b)
  })

  it('renders the mark and name for a custom product name', () => {
    brand('Acme Cloud')
    const w = mount(Lockup)
    expect(w.text()).toContain('Acme Cloud')
    expect(w.find('[aria-label="Nebla"]').exists()).toBe(false)
  })
})

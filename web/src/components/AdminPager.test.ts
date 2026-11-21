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
import { flushPromises } from '@vue/test-utils'
import { mountApp } from '@/test/mount'
import AdminPager from './AdminPager.vue'

afterEach(() => {
  document.body.innerHTML = ''
})

const button = (label: string) =>
  Array.from(document.body.querySelectorAll('button')).find((b) => b.textContent?.trim() === label)!

describe('AdminPager', () => {
  it('renders nothing when everything fits on one page', () => {
    mountApp(AdminPager, { props: { page: 1, perPage: 50, total: 50 } })
    expect(document.body.textContent).not.toContain('Showing')
    expect(document.body.querySelector('button')).toBeNull()
  })

  it('shows the range on page 2', () => {
    mountApp(AdminPager, { props: { page: 2, perPage: 50, total: 120 } })
    expect(document.body.textContent).toContain('Showing 51–100 of 120')
  })

  it('disables Previous on the first page and Next on the last', () => {
    mountApp(AdminPager, { props: { page: 1, perPage: 50, total: 120 } })
    expect(button('Previous').disabled).toBe(true)
    expect(button('Next').disabled).toBe(false)
    document.body.innerHTML = ''
    mountApp(AdminPager, { props: { page: 3, perPage: 50, total: 120 } })
    expect(document.body.textContent).toContain('Showing 101–120 of 120')
    expect(button('Previous').disabled).toBe(false)
    expect(button('Next').disabled).toBe(true)
  })

  it('emits the new page on click', async () => {
    const { wrapper } = mountApp(AdminPager, { props: { page: 2, perPage: 50, total: 120 } })
    button('Previous').click()
    button('Next').click()
    await flushPromises()
    expect(wrapper.findComponent(AdminPager).emitted('update:page')).toEqual([[1], [3]])
  })
})

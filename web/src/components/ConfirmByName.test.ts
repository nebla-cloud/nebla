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
import ConfirmByName from './ConfirmByName.vue'

afterEach(() => {
  document.body.innerHTML = ''
})

function confirmButton(): HTMLButtonElement {
  const b = Array.from(document.body.querySelectorAll('button')).find((x) =>
    x.textContent?.includes('Delete server'),
  )
  if (!b) throw new Error('confirm button not found')
  return b as HTMLButtonElement
}

async function type(value: string) {
  const input = document.body.querySelector('input[name="confirm_name"]') as HTMLInputElement
  input.value = value
  input.dispatchEvent(new Event('input'))
  await flushPromises()
}

describe('ConfirmByName', () => {
  it('keeps confirm disabled until the name matches exactly', async () => {
    const { wrapper } = mountApp(ConfirmByName, {
      props: { name: 'web-1', title: 'Delete web-1?', confirmLabel: 'Delete server', open: true },
    })
    await flushPromises()
    expect(confirmButton().disabled).toBe(true)
    await type('web-')
    expect(confirmButton().disabled).toBe(true)
    await type('WEB-1')
    expect(confirmButton().disabled).toBe(true)
    await type('web-1')
    expect(confirmButton().disabled).toBe(false)
    confirmButton().click()
    await flushPromises()
    expect(wrapper.findComponent(ConfirmByName).emitted('confirm')).toHaveLength(1)
  })
})

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

import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { mountApp } from '@/test/mount'
import CopyField from './CopyField.vue'

afterEach(() => {
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

const copyButton = () =>
  Array.from(document.body.querySelectorAll('button')).find((b) => /Cop/.test(b.textContent ?? ''))!

describe('CopyField', () => {
  it('shows Copied after copy', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText } })
    const { wrapper } = mountApp(CopyField, { props: { value: 'secret-1' } })
    copyButton().click()
    await flushPromises()
    expect(writeText).toHaveBeenCalledWith('secret-1')
    expect(copyButton().textContent).toContain('Copied')
    expect(wrapper.findComponent(CopyField).emitted('copied')).toHaveLength(1)
  })

  it('falls back to selecting', async () => {
    vi.stubGlobal('navigator', { ...navigator, clipboard: undefined })
    mountApp(CopyField, { props: { value: 'secret-1' } })
    copyButton().click()
    await flushPromises()
    const input = document.body.querySelector('input') as HTMLInputElement
    expect(input.selectionStart).toBe(0)
    expect(input.selectionEnd).toBe('secret-1'.length)
    expect(document.body.textContent).toMatch(/Press (Ctrl\+C|⌘C) to copy/)
  })
})

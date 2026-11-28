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
import ProtectionButton from './ProtectionButton.vue'

const flags = [
  {
    key: 'delete' as const,
    label: 'Delete',
    description: "Blocks deleting this server until it's turned off.",
  },
  {
    key: 'rebuild' as const,
    label: 'Rebuild',
    description: 'Blocks rebuild and restore from a backup.',
  },
]

// Path data of the lock and lock-open icons, which render inline.
const LOCKED = 'M7 11V7a5 5 0 0 1 10 0v4'
const OPEN = 'M7 11V7a5 5 0 0 1 9.9-1'

afterEach(() => {
  document.body.innerHTML = ''
})

function mountIt(value: Record<string, boolean>, save = vi.fn().mockResolvedValue(undefined)) {
  const m = mountApp(ProtectionButton, { props: { flags, value, save } })
  return { ...m, save }
}

const btn = (text: string) =>
  Array.from(document.body.querySelectorAll('button')).find(
    (b) => b.textContent?.trim() === text,
  ) as HTMLButtonElement

async function openDialog() {
  btn('Protection').click()
  await flushPromises()
}

async function untick(label: string) {
  const row = Array.from(document.body.querySelectorAll('[role="dialog"] label')).find((l) =>
    l.textContent?.includes(label),
  ) as HTMLElement
  const box = document.body.querySelector(
    `[role="dialog"] button[role="checkbox"][id="${row.getAttribute('for')}"]`,
  ) as HTMLButtonElement
  box.click()
  await flushPromises()
}

describe('ProtectionButton', () => {
  it('shows a lock when any flag is on and an open lock when none are', async () => {
    mountIt({ delete: true, rebuild: false })
    expect(btn('Protection').innerHTML).toContain(LOCKED)
    document.body.innerHTML = ''
    mountIt({ delete: false, rebuild: false })
    expect(btn('Protection').innerHTML).toContain(OPEN)
  })

  it('saves only the changed keys', async () => {
    const { save, wrapper } = mountIt({ delete: true, rebuild: true })
    await openDialog()
    expect(document.body.textContent).toContain('Change protection')
    await untick('Delete')
    btn('Save').click()
    await flushPromises()
    expect(save).toHaveBeenCalledWith({ delete: false })
    expect(wrapper.findComponent(ProtectionButton).emitted('saved')).toHaveLength(1)
    expect(document.body.querySelector('[role="dialog"]')).toBeNull()
  })

  it('sends nothing on cancel', async () => {
    const { save } = mountIt({ delete: true, rebuild: true })
    await openDialog()
    await untick('Delete')
    btn('Cancel').click()
    await flushPromises()
    expect(save).not.toHaveBeenCalled()
  })

  it('stays open when the save is rejected', async () => {
    const { save } = mountIt(
      { delete: true, rebuild: true },
      vi.fn().mockRejectedValue(new Error('no')),
    )
    await openDialog()
    await untick('Delete')
    btn('Save').click()
    await flushPromises()
    expect(save).toHaveBeenCalled()
    expect(document.body.querySelector('[role="dialog"]')).not.toBeNull()
  })
})

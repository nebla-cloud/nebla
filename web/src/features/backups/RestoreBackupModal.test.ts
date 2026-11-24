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
import { formatDateTime } from '@/lib/format'
import { mountApp } from '@/test/mount'
import { makeBackup } from './fixtures'
import RestoreBackupModal from './RestoreBackupModal.vue'
import { settle } from './testkit'

let unmount: () => void
afterEach(() => {
  unmount()
  document.body.innerHTML = ''
})

async function open(resets: boolean) {
  const m = mountApp(RestoreBackupModal, {
    props: {
      open: true,
      name: 'web1',
      backup: makeBackup({ restore_resets_identity: resets }),
    },
  })
  unmount = () => m.wrapper.unmount()
  await settle()
  return document.body.querySelector('[role="dialog"]')!.textContent ?? ''
}

describe('RestoreBackupModal', () => {
  it('warns that the disk is replaced', async () => {
    const text = await open(false)
    expect(text).toContain(
      `Restore web1 to ${formatDateTime('2026-11-22T02:14:00Z')}? The current disk will be replaced. Anything written since then is lost.`,
    )
    expect(text).not.toContain('SSH host keys will change')
  })

  it('adds the network warning when the identity resets', async () => {
    const text = await open(true)
    expect(text).toContain(
      "This server's network changed since this backup. Its network will be reconfigured and SSH host keys will change.",
    )
  })
})

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
import { http } from 'msw'
import { json, server } from '@/test/server'
import { mountApp } from '@/test/mount'
import RestoreSnapshotModal from './RestoreSnapshotModal.vue'
import { makeSnapshot } from './fixtures'
import { button, runningAction, settle } from './testkit'

let unmount: () => void
afterEach(() => {
  unmount()
  document.body.innerHTML = ''
})

async function mountModal(keeps: boolean | null) {
  const m = mountApp(RestoreSnapshotModal, {
    props: {
      snapshot: makeSnapshot({ restore_keeps_identity: keeps }),
      serverName: 'web1',
      open: true,
    },
  })
  unmount = () => m.wrapper.unmount()
  await settle()
  return m
}

describe('RestoreSnapshotModal', () => {
  it('explains what is lost and what stays', async () => {
    await mountModal(true)
    const text = document.body.textContent ?? ''
    expect(text).toContain('Restore web1 to before-upgrade?')
    expect(text).toContain('Changes since 18 Nov 2026 are lost. Volumes stay attached.')
    expect(text).not.toContain('SSH host keys')
  })

  it('warns about the identity when the network changed', async () => {
    await mountModal(false)
    expect(document.body.textContent).toContain(
      "web1's network changed since this snapshot, so it will come back with new SSH host keys and run its user data again.",
    )
  })

  it('rebuilds the source server from the snapshot', async () => {
    let body: unknown
    server.use(
      http.post('*/v1/servers/5/actions/rebuild', async ({ request }) => {
        body = await request.json()
        return json(201, { action: runningAction('rebuild_server') })
      }),
    )
    const m = await mountModal(true)
    button('Restore').click()
    await settle()
    expect(body).toEqual({ snapshot: '1' })
    expect(m.wrapper.findComponent(RestoreSnapshotModal).emitted('started')).toBeTruthy()
  })
})

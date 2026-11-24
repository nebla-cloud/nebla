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
import { makeServer } from '@/features/servers/fixtures'
import { json, server } from '@/test/server'
import { mountApp } from '@/test/mount'
import TakeSnapshotModal from './TakeSnapshotModal.vue'
import { makeSnapshot } from './fixtures'
import { button, runningAction, settle } from './testkit'

let unmount: () => void
afterEach(() => {
  unmount()
  document.body.innerHTML = ''
})

async function mountModal(status: 'running' | 'off') {
  const m = mountApp(TakeSnapshotModal, {
    props: { server: makeServer({ id: 1, name: 'web1', status }), open: true },
  })
  unmount = () => m.wrapper.unmount()
  await settle()
  return m
}

const WARNING =
  'web1 is running. Nebla will shut it down, save its disk and start it again. Expect a few minutes offline.'
const FORCE = "Force stop if it doesn't shut down"

describe('TakeSnapshotModal', () => {
  it('warns only when the server is running', async () => {
    await mountModal('running')
    expect(document.body.textContent).toContain(WARNING)
    expect(document.body.textContent).toContain(FORCE)
    unmount()
    document.body.innerHTML = ''
    await mountModal('off')
    expect(document.body.textContent).not.toContain('Expect a few minutes offline')
    expect(document.body.textContent).not.toContain(FORCE)
  })

  it('prefills the name from the server', async () => {
    await mountModal('off')
    const input = document.body.querySelector('input[name=name]') as HTMLInputElement
    expect(input.value).toMatch(/^web1-\d{8}-\d{4}$/)
  })

  it('sends force_stop when ticked', async () => {
    let body: Record<string, unknown> = {}
    server.use(
      http.post('*/v1/snapshots', async ({ request }) => {
        body = (await request.json()) as Record<string, unknown>
        return json(201, {
          snapshot: makeSnapshot({ status: 'creating' }),
          action: runningAction('create_snapshot'),
        })
      }),
    )
    await mountModal('running')
    ;(document.body.querySelector('button[role=checkbox]') as HTMLButtonElement).click()
    await settle()
    button('Take snapshot').click()
    await settle()
    expect(body.server).toBe('1')
    expect(body.force_stop).toBe(true)
    expect(String(body.name)).toMatch(/^web1-/)
  })
})

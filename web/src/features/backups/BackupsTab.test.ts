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

import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import type { Action, Server } from '@/api/types'
import { makeServer } from '@/features/servers/fixtures'
import { formatDateTime } from '@/lib/format'
import { json, server } from '@/test/server'
import { mountApp, testRouter } from '@/test/mount'
import { http } from 'msw'
import { makeLocation } from '../volumes/fixtures'
import BackupsTab from './BackupsTab.vue'
import { makeBackup, type Backup } from './fixtures'
import { backupAction, button, serveBackups, settle, type Calls } from './testkit'

let backups: Backup[]
let calls: Calls
let unmount: () => void
let started: Action[]
let offered = true

const on = (over: Partial<Server['backups']> = {}) =>
  makeServer({
    id: 5,
    backups: {
      enabled: true,
      keep: 7,
      count: 1,
      last_backup_at: '2026-11-22T02:14:00Z',
      next_backup_at: '2026-11-25T02:20:00Z',
      ...over,
    },
  })

beforeEach(() => {
  backups = [makeBackup()]
  calls = { list: 0, posts: [], deletes: [], restores: [], puts: [] }
  started = []
  offered = true
  serveBackups(() => backups, calls)
})

afterEach(() => {
  unmount()
  document.body.innerHTML = ''
})

async function mountTab(s: Server = on()) {
  server.use(
    http.get('*/v1/locations', () =>
      json(200, {
        locations: [makeLocation({ id: 1, backups_available: offered })],
        meta: { pagination: { page: 1, per_page: 50, total_entries: 1 } },
      }),
    ),
  )
  const router = testRouter([
    {
      path: '/p/:projectId/servers/:serverId/backups',
      name: 'server-backups',
      component: BackupsTab,
    },
  ])
  await router.push('/p/7/servers/5/backups')
  const m = mountApp(BackupsTab, {
    router,
    props: { server: s, onStarted: (a: Action) => started.push(a) },
  })
  unmount = () => m.wrapper.unmount()
  await settle()
  return m
}

const rows = () => Array.from(document.body.querySelectorAll('[data-testid="backup"]'))
const dialog = () => document.body.querySelector('[role="dialog"]') as HTMLElement

describe('BackupsTab', () => {
  it('shows the toggle, keep 1 to 14 and the local next and last times', async () => {
    await mountTab()
    expect(document.body.textContent).toContain('Daily backups')
    const keep = document.body.querySelector('select[name=keep]') as HTMLSelectElement
    expect(Array.from(keep.options).map((o) => o.value)).toEqual(
      Array.from({ length: 14 }, (_, i) => String(i + 1)),
    )
    expect(keep.value).toBe('7')
    expect(document.body.textContent).toContain(
      `Next backup: ${formatDateTime('2026-11-25T02:20:00Z')}`,
    )
    expect(document.body.textContent).toContain(
      `Last backup: ${formatDateTime('2026-11-22T02:14:00Z')}`,
    )
  })

  it('turns backups on with the chosen keep', async () => {
    await mountTab(on({ enabled: false, next_backup_at: null }))
    expect(document.body.textContent).not.toContain('Next backup:')
    const sw = document.body.querySelector('button[role=switch]') as HTMLButtonElement
    sw.click()
    await settle()
    expect(calls.puts).toEqual([{ backups: { enabled: true, keep: 7 } }])
  })

  it('saves a new keep', async () => {
    await mountTab()
    const keep = document.body.querySelector('select[name=keep]') as HTMLSelectElement
    keep.value = '3'
    keep.dispatchEvent(new Event('change'))
    await settle()
    expect(calls.puts).toEqual([{ backups: { enabled: true, keep: 3 } }])
  })

  it('backs up now', async () => {
    await mountTab()
    expect(document.body.textContent).not.toContain('A backup is already waiting or running')
    button('Back up now').click()
    await settle()
    expect(calls.posts).toEqual(['5'])
  })

  it.each(['queued', 'creating'] as const)('disables Back up now while one is %s', async (st) => {
    backups = [makeBackup({ id: 2, status: st, size: 0, completed: null })]
    await mountTab()
    expect(button('Back up now').disabled).toBe(true)
    expect(document.body.textContent).toContain('A backup is already waiting or running')
  })

  it('labels each status', async () => {
    backups = [
      makeBackup({ id: 1, status: 'queued', size: 0, completed: null }),
      makeBackup({ id: 2, status: 'creating', size: 0, completed: null }),
      makeBackup({ id: 3, status: 'available' }),
      makeBackup({ id: 4, status: 'restoring' }),
      makeBackup({ id: 5, status: 'missing' }),
      makeBackup({ id: 6, status: 'error' }),
    ]
    await mountTab()
    const text = rows().map((r) => r.textContent ?? '')
    expect(text[0]).toContain('Waiting for its turn')
    expect(text[1]).toContain('Backing up…')
    expect(text[2]).not.toMatch(/Waiting|Backing up|Restoring|Missing|Failed/)
    expect(text[3]).toContain('Restoring…')
    expect(text[4]).toContain('Missing')
    expect(text[5]).toContain('Failed')
  })

  it('lists kind and size', async () => {
    backups = [makeBackup({ kind: 'manual' })]
    await mountTab()
    expect(rows()[0]!.textContent).toContain('Manual')
    expect(rows()[0]!.textContent).toContain('2 GB')
  })

  it('offers Restore only on available backups', async () => {
    backups = [makeBackup({ id: 1 }), makeBackup({ id: 2, status: 'error' })]
    await mountTab()
    expect(rows()[0]!.querySelector('button[aria-label^="Restore"]')).toBeTruthy()
    expect(rows()[1]!.querySelector('button[aria-label^="Restore"]')).toBeNull()
  })

  it('restores after typing the server name and hands the action up', async () => {
    await mountTab()
    ;(rows()[0]!.querySelector('button[aria-label^="Restore"]') as HTMLElement).click()
    await settle()
    expect(dialog().textContent).toContain(
      `Restore web1 to ${formatDateTime('2026-11-22T02:14:00Z')}? The current disk will be replaced. Anything written since then is lost.`,
    )
    expect(calls.restores).toEqual([])
    const input = dialog().querySelector('input[name=confirm_name]') as HTMLInputElement
    input.value = 'web1'
    input.dispatchEvent(new Event('input'))
    await settle()
    button('Restore server').click()
    await settle()
    expect(calls.restores).toEqual([{ backup: 1 }])
    expect(started.map((a) => a.command)).toEqual(['restore_backup'])
  })

  it('deletes after confirming', async () => {
    await mountTab()
    ;(rows()[0]!.querySelector('button[aria-label^="Delete"]') as HTMLElement).click()
    await settle()
    expect(calls.deletes).toEqual([])
    button('Delete backup').click()
    await settle()
    expect(calls.deletes).toEqual(['1'])
    expect(started.map((a) => a.command)).toEqual([backupAction('delete_backup').command])
  })

  it('keeps the list and lets backups be turned off, but locks keep and Back up now where backups are off', async () => {
    offered = false
    await mountTab()
    expect(document.body.textContent).toContain(
      'Backups are turned off for this location. Existing backups stay restorable.',
    )
    expect(button('Back up now').disabled).toBe(true)
    expect((document.body.querySelector('select[name=keep]') as HTMLSelectElement).disabled).toBe(
      true,
    )
    expect(rows()).toHaveLength(1)
    expect(rows()[0]!.querySelector('button[aria-label^="Restore"]')).toBeTruthy()
    expect(rows()[0]!.querySelector('button[aria-label^="Delete"]')).toBeTruthy()
    const sw = document.body.querySelector('button[role=switch]') as HTMLButtonElement
    expect(sw.disabled).toBe(false)
    sw.click()
    await settle()
    expect(calls.puts).toEqual([{ backups: { enabled: false, keep: 7 } }])
  })

  it('does not let backups be turned on where the location has none', async () => {
    offered = false
    await mountTab(on({ enabled: false, next_backup_at: null }))
    expect((document.body.querySelector('button[role=switch]') as HTMLButtonElement).disabled).toBe(
      true,
    )
  })

  it('disables Back up now while backups are off', async () => {
    await mountTab(on({ enabled: false, next_backup_at: null }))
    expect(button('Back up now').disabled).toBe(true)
  })

  it('says nothing about it where backups are offered', async () => {
    await mountTab()
    expect(document.body.textContent).not.toContain('Backups are turned off')
    expect((document.body.querySelector('button[role=switch]') as HTMLButtonElement).disabled).toBe(
      false,
    )
  })

  it('explains an empty list', async () => {
    backups = []
    await mountTab()
    expect(document.body.textContent).toContain('No backups yet')
  })
})

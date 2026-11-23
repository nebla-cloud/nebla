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
import { mountApp } from '@/test/mount'
import GuestConfigDialog from './GuestConfigDialog.vue'
import { makeGuestConfig } from './fixtures'
import { settle } from './testkit'

let unmount: () => void

afterEach(() => {
  unmount()
  document.body.innerHTML = ''
})

async function mountDialog(props: Record<string, unknown>) {
  const m = mountApp(GuestConfigDialog, { props: { open: true, ...props } })
  unmount = () => m.wrapper.unmount()
  await settle()
  return document.body.querySelector('[role="dialog"]') as HTMLElement
}

describe('GuestConfigDialog', () => {
  it('shows the apply and persist commands with copy buttons', async () => {
    const d = await mountDialog({ config: makeGuestConfig() })
    expect(d.textContent).toContain('Run this on the server')
    expect(d.textContent).toContain(
      'New and rebuilt servers set this up automatically. For a running server, run these commands.',
    )
    expect(d.textContent).toContain('web1')
    expect(d.querySelector('[data-testid="guest-apply"]')!.textContent).toContain(
      'sudo arping -U -c 3 -I eth0 203.0.113.50',
    )
    expect(d.querySelector('[data-testid="guest-persist"]')!.textContent).toContain('netplan')
    const copies = Array.from(d.querySelectorAll('button')).filter(
      (b) => b.textContent?.trim() === 'Copy',
    )
    expect(copies).toHaveLength(2)
    expect(d.textContent).not.toContain('to stop using the address')
    expect(d.querySelector('[data-testid="guest-remove"]')).toBeNull()
    expect(d.textContent).not.toMatch(/reboot/i)
  })

  it('adds the previous server section only when it is set', async () => {
    const d = await mountDialog({
      config: makeGuestConfig({
        server: 'web2',
        previous_server: 'web1',
        remove: 'sudo ip addr del 203.0.113.50/32 dev eth0',
      }),
    })
    expect(d.textContent).toContain('Run this on web1 to stop using the address')
    expect(d.querySelector('[data-testid="guest-remove"]')!.textContent).toContain(
      'sudo ip addr del 203.0.113.50/32 dev eth0',
    )
    const copies = Array.from(d.querySelectorAll('button')).filter(
      (b) => b.textContent?.trim() === 'Copy',
    )
    expect(copies).toHaveLength(3)
  })

  it('shows only the removal after an unassign', async () => {
    const d = await mountDialog({
      config: makeGuestConfig({
        server: null,
        apply: null,
        persist: null,
        previous_server: 'web1',
        remove: 'sudo ip addr del 203.0.113.50/32 dev eth0',
      }),
    })
    expect(d.querySelector('[data-testid="guest-apply"]')).toBeNull()
    expect(d.textContent).toContain('Run this on web1 to stop using the address')
  })
})

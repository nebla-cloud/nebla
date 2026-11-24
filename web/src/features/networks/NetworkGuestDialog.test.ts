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
import NetworkGuestDialog from './NetworkGuestDialog.vue'
import { makeNetworkGuestConfig } from './fixtures'
import { settle } from './testkit'

let unmount: () => void

afterEach(() => {
  unmount()
  document.body.innerHTML = ''
})

async function mountDialog(props: Record<string, unknown>) {
  const m = mountApp(NetworkGuestDialog, { props: { open: true, serverName: 'web1', ...props } })
  unmount = () => m.wrapper.unmount()
  await settle()
  return document.body.querySelector('[role="dialog"]') as HTMLElement
}

describe('NetworkGuestDialog', () => {
  it('shows the panel copy and copyable apply and persist blocks', async () => {
    const d = await mountDialog({ config: makeNetworkGuestConfig() })
    expect(d.textContent).toContain('Run this on the server')
    expect(d.textContent).toContain(
      'New and rebuilt servers set this up automatically. For a running server, run these commands.',
    )
    expect(d.textContent).toContain('web1')
    expect(d.querySelector('[data-testid="guest-apply"]')!.textContent).toContain(
      'sudo ip addr add 10.0.0.2/16 dev eth1',
    )
    expect(d.querySelector('[data-testid="guest-persist"]')!.textContent).toContain('netplan')
    expect(d.querySelector('[data-testid="guest-remove"]')).toBeNull()
    const copies = Array.from(d.querySelectorAll('button')).filter(
      (b) => b.textContent?.trim() === 'Copy',
    )
    expect(copies).toHaveLength(2)
  })

  it('shows the remove block after a detach', async () => {
    const d = await mountDialog({
      config: makeNetworkGuestConfig({
        apply: null,
        persist: null,
        remove: 'sudo ip link set eth1 down',
      }),
    })
    expect(d.querySelector('[data-testid="guest-remove"]')!.textContent).toContain('eth1 down')
    expect(d.querySelector('[data-testid="guest-apply"]')).toBeNull()
  })

  it('puts the note above the commands for a stopped server', async () => {
    const d = await mountDialog({
      config: makeNetworkGuestConfig({ note: 'Run these after starting the server.' }),
    })
    const note = d.querySelector('[data-testid="guest-note"]')!
    expect(note.textContent).toContain('Run these after starting the server.')
    const apply = d.querySelector('[data-testid="guest-apply"]')!
    expect(note.compareDocumentPosition(apply) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  })

  it('never mentions Proxmox words', async () => {
    const d = await mountDialog({ config: makeNetworkGuestConfig() })
    expect(d.textContent).not.toMatch(/vnet|vmid|bridge|\bnic\b|zone|sdn/i)
  })
})

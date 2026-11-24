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
import { mountApp, testRouter } from '@/test/mount'
import { makeLocation } from '@/features/volumes/fixtures'
import NetworkPicker from './NetworkPicker.vue'
import { makeNetwork } from './fixtures'
import { serveLists, settle } from './testkit'

let unmount: () => void

afterEach(() => {
  unmount()
  document.body.innerHTML = ''
})

const nets = [
  makeNetwork({ id: 1, name: 'a' }),
  makeNetwork({ id: 2, name: 'b' }),
  makeNetwork({ id: 3, name: 'c' }),
  makeNetwork({ id: 4, name: 'd' }),
  makeNetwork({ id: 5, name: 'other', location: { id: 2, name: 'far', description: '' } }),
]

async function mountPicker(location: ReturnType<typeof makeLocation>, modelValue: number[] = []) {
  serveLists({ networks: nets })
  const m = await mountIn7(location, modelValue)
  unmount = () => m.wrapper.unmount()
  await settle()
}

async function mountIn7(location: ReturnType<typeof makeLocation>, modelValue: number[]) {
  const router = testRouter([{ path: '/p/:projectId', component: NetworkPicker }])
  await router.push('/p/7')
  return mountApp(NetworkPicker, { router, props: { location, modelValue } })
}

const boxes = () =>
  Array.from(document.body.querySelectorAll<HTMLInputElement>('input[name="network"]'))

describe('NetworkPicker', () => {
  it('lists only networks in the chosen location', async () => {
    await mountPicker(makeLocation({ private_networks_available: true }))
    expect(boxes().map((b) => b.value)).toEqual(['1', '2', '3', '4'])
    expect(document.body.textContent).toContain('Private networks')
  })

  it('is hidden when the location has no private networks', async () => {
    await mountPicker(makeLocation({ private_networks_available: false }))
    expect(boxes()).toHaveLength(0)
    expect(document.body.textContent).not.toContain('Private networks')
  })

  it('stops at three picks', async () => {
    await mountPicker(makeLocation({ private_networks_available: true }), [1, 2, 3])
    const d = boxes().find((b) => b.value === '4')!
    expect(d.disabled).toBe(true)
    expect(boxes().find((b) => b.value === '1')!.disabled).toBe(false)
  })

  it('is hidden when the location has no networks yet', async () => {
    serveLists({ networks: [nets[4]!] })
    const m = await mountIn7(makeLocation({ private_networks_available: true }), [])
    unmount = () => m.wrapper.unmount()
    await settle()
    expect(boxes()).toHaveLength(0)
  })
})

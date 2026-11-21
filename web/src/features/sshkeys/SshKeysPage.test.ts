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
import { flushPromises } from '@vue/test-utils'
import { http } from 'msw'
import { json, server } from '@/test/server'
import { mountApp, testRouter } from '@/test/mount'
import SshKeysPage from './SshKeysPage.vue'

const ED = 'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFake laptop@home'

interface Key {
  id: number
  name: string
  public_key: string
  fingerprint: string
  created: string
}

let mounted: { unmount: () => void } | null = null
let keys: Key[]
let listUrls: string[]
let calls: { method: string; url: string; body?: unknown }[]

function key(id: number, name: string): Key {
  return {
    id,
    name,
    public_key: ED,
    fingerprint: `aa:bb:cc:dd:${id}`,
    created: '2026-11-01T10:00:00Z',
  }
}

async function mountPage() {
  const router = testRouter([{ path: '/p/:projectId/ssh-keys', component: SshKeysPage }])
  await router.push('/p/7/ssh-keys')
  const m = mountApp(SshKeysPage, { router })
  mounted = m.wrapper
  await settle()
  return m
}

async function settle() {
  for (let i = 0; i < 5; i++) {
    await new Promise((r) => setTimeout(r, 10))
    await flushPromises()
  }
}

function body(): HTMLElement {
  return document.body
}

function button(text: string): HTMLButtonElement {
  const b = Array.from(body().querySelectorAll('button')).find((x) => x.textContent?.includes(text))
  if (!b) throw new Error(`button ${text} not found`)
  return b as HTMLButtonElement
}

async function fill(selector: string, value: string) {
  const el = body().querySelector(selector) as HTMLInputElement
  el.value = value
  el.dispatchEvent(new Event('input'))
  await flushPromises()
}

beforeEach(() => {
  keys = [key(1, 'laptop')]
  calls = []
  listUrls = []
  server.use(
    http.get('*/v1/projects/:id', () => json(200, { id: 7, name: 'Home', usage: { servers: 0 } })),
    http.get('*/v1/ssh_keys', ({ request }) => {
      listUrls.push(request.url)
      return json(200, { ssh_keys: keys, meta: {} })
    }),
    http.post('*/v1/ssh_keys', async ({ request }) => {
      const b = (await request.json()) as { name: string; public_key: string }
      calls.push({ method: 'POST', url: request.url, body: b })
      const k = { ...key(2, b.name), public_key: b.public_key }
      keys = [...keys, k]
      return json(201, k)
    }),
    http.put('*/v1/ssh_keys/:id', async ({ request, params }) => {
      const b = (await request.json()) as { name: string }
      calls.push({ method: 'PUT', url: request.url, body: b })
      keys = keys.map((k) => (String(k.id) === params.id ? { ...k, name: b.name } : k))
      return json(
        200,
        keys.find((k) => String(k.id) === params.id),
      )
    }),
    http.delete('*/v1/ssh_keys/:id', ({ request, params }) => {
      calls.push({ method: 'DELETE', url: request.url })
      keys = keys.filter((k) => String(k.id) !== params.id)
      return new Response(null, { status: 204 })
    }),
  )
})

afterEach(() => {
  mounted?.unmount()
  mounted = null
  document.body.innerHTML = ''
})

describe('SshKeysPage', () => {
  it('lists keys with fingerprint', async () => {
    await mountPage()
    expect(body().textContent).toContain('laptop')
    expect(body().textContent).toContain('aa:bb:cc:dd:1')
    expect(new URL(listUrls[0]!).searchParams.get('per_page')).toBe('50')
  })

  it('adding a key posts name and public_key and shows it', async () => {
    await mountPage()
    button('Add key').click()
    await flushPromises()
    await fill('textarea[name="public_key"]', ED)
    await fill('input[name="name"]', 'work')
    body().querySelector<HTMLFormElement>('#add-ssh-key')!.requestSubmit()
    await settle()
    expect(calls).toHaveLength(1)
    expect(calls[0]!.body).toEqual({ name: 'work', public_key: ED })
    expect(body().textContent).toContain('work')
  })

  it('prefills the name from the key comment', async () => {
    await mountPage()
    button('Add key').click()
    await flushPromises()
    await fill('textarea[name="public_key"]', ED)
    body().querySelector<HTMLFormElement>('#add-ssh-key')!.requestSubmit()
    await settle()
    expect(calls[0]!.body).toEqual({ name: 'laptop@home', public_key: ED })
  })

  it("rejects text that isn't an OpenSSH public key before sending", async () => {
    await mountPage()
    button('Add key').click()
    await flushPromises()
    await fill('textarea[name="public_key"]', 'hello')
    body().querySelector<HTMLFormElement>('#add-ssh-key')!.requestSubmit()
    await settle()
    expect(calls).toHaveLength(0)
    expect(body().textContent).toContain("doesn't look like an OpenSSH public key")
  })

  it('rename sends PUT', async () => {
    await mountPage()
    button('Rename').click()
    await flushPromises()
    await fill('input[name="rename"]', 'desk')
    body().querySelector<HTMLFormElement>('#rename-ssh-key')!.requestSubmit()
    await settle()
    expect(calls).toEqual([expect.objectContaining({ method: 'PUT', body: { name: 'desk' } })])
    expect(calls[0]!.url).toMatch(/\/ssh_keys\/1$/)
    expect(body().textContent).toContain('desk')
  })

  it('delete asks for confirmation then sends DELETE', async () => {
    await mountPage()
    button('Delete').click()
    await settle()
    expect(calls).toHaveLength(0)
    const confirm = button('Delete key')
    expect(confirm.disabled).toBe(true)
    await fill('input[name="confirm_name"]', 'laptop')
    expect(confirm.disabled).toBe(false)
    confirm.click()
    await settle()
    expect(calls.map((c) => c.method)).toEqual(['DELETE'])
    expect(calls[0]!.url).toMatch(/\/ssh_keys\/1$/)
  })
})

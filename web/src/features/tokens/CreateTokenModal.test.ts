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

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, h, ref } from 'vue'
import { flushPromises } from '@vue/test-utils'
import { http } from 'msw'
import { json, server } from '@/test/server'
import { mountApp } from '@/test/mount'
import CreateTokenModal from './CreateTokenModal.vue'

let mounted: { unmount: () => void } | null = null
let posts: unknown[]

async function settle() {
  for (let i = 0; i < 5; i++) {
    await new Promise((r) => setTimeout(r, 10))
    await flushPromises()
  }
}

const body = () => document.body

function button(text: string): HTMLButtonElement {
  const b = Array.from(body().querySelectorAll('button')).find((x) => x.textContent?.includes(text))
  if (!b) throw new Error(`button ${text} not found`)
  return b as HTMLButtonElement
}

async function fill(value: string) {
  const el = body().querySelector('input[name="name"]') as HTMLInputElement
  el.value = value
  el.dispatchEvent(new Event('input'))
  await flushPromises()
}

const open = ref(true)

async function mountModal() {
  open.value = true
  const Host = defineComponent({
    render: () =>
      h(CreateTokenModal, {
        projectId: 7,
        projectName: 'Home lab',
        open: open.value,
        'onUpdate:open': (v: boolean) => (open.value = v),
      }),
  })
  const m = mountApp(Host)
  mounted = m.wrapper
  await settle()
  return m
}

async function create(name = 'ci') {
  await fill(name)
  body().querySelector<HTMLFormElement>('#create-token')!.requestSubmit()
  await settle()
}

beforeEach(() => {
  posts = []
  vi.stubGlobal('location', { ...window.location, origin: 'http://localhost:3000' })
  Object.defineProperty(navigator, 'clipboard', {
    value: { writeText: vi.fn().mockResolvedValue(undefined) },
    configurable: true,
  })
  server.use(
    http.post('*/v1/projects/:id/tokens', async ({ request }) => {
      const b = (await request.json()) as { name: string; permission: string }
      posts.push(b)
      return json(201, {
        id: 1,
        name: b.name,
        permission: b.permission,
        created: '2026-11-20T10:00:00Z',
        last_used: null,
        token: 'nb_abc',
      })
    }),
    http.get('*/v1/projects/:id/tokens', () => json(200, { tokens: [] })),
  )
})

afterEach(() => {
  mounted?.unmount()
  mounted = null
  document.body.innerHTML = ''
  vi.unstubAllGlobals()
})

describe('CreateTokenModal', () => {
  it('validates name 1-100', async () => {
    await mountModal()
    await create('   ')
    expect(posts).toHaveLength(0)
    expect(body().textContent).toContain('Give the token a name.')
    await create('x'.repeat(101))
    expect(posts).toHaveLength(0)
    expect(body().textContent).toContain('100 characters')
    await create('x'.repeat(100))
    expect(posts).toHaveLength(1)
  })

  it('defaults to Read', async () => {
    await mountModal()
    await create()
    expect(posts).toEqual([{ name: 'ci', permission: 'read' }])
  })

  it('sends read_write when chosen', async () => {
    await mountModal()
    const rw = body().querySelector<HTMLButtonElement>('button[role="radio"][value="read_write"]')!
    rw.click()
    await settle()
    await create()
    expect(posts).toEqual([{ name: 'ci', permission: 'read_write' }])
  })

  it('reveal shows token once', async () => {
    await mountModal()
    await create()
    const input = body().querySelector<HTMLInputElement>('input[readonly]')!
    expect(input.value).toBe('nb_abc')
    expect(body().textContent).toContain("You won't see it again.")
    expect(body().textContent).toContain(
      'nebla context create home-lab --endpoint http://localhost:3000',
    )
  })

  it('close without copy asks first', async () => {
    await mountModal()
    await create()
    button('Done').click()
    await settle()
    expect(body().textContent).toContain('Close without copying?')
    button('Close anyway').click()
    await settle()
    expect(body().textContent).not.toContain('nb_abc')
    expect(body().textContent).not.toContain('Close without copying?')
  })

  it('Escape or overlay close before Copy asks first', async () => {
    await mountModal()
    await create()
    const dialog = body().querySelector('[role="dialog"]')!
    dialog.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await settle()
    expect(body().textContent).toContain('Close without copying?')
    expect(open.value).toBe(true)
  })

  it('ignores a create that resolves after the modal closed', async () => {
    let release!: () => void
    const gate = new Promise<void>((r) => (release = r))
    server.use(
      http.post('*/v1/projects/:id/tokens', async () => {
        await gate
        return json(201, {
          id: 1,
          name: 'ci',
          permission: 'read',
          created: '2026-11-20T10:00:00Z',
          last_used: null,
          token: 'nb_late',
        })
      }),
    )
    await mountModal()
    await fill('ci')
    body().querySelector<HTMLFormElement>('#create-token')!.requestSubmit()
    await settle()
    button('Cancel').click()
    await settle()
    release()
    await settle()
    open.value = true
    await settle()
    expect(body().textContent).not.toContain('nb_late')
    expect(body().querySelector('input[name="name"]')).not.toBeNull()
  })

  it('Done after copy closes directly', async () => {
    await mountModal()
    await create()
    button('Copy').click()
    await settle()
    button('Done').click()
    await settle()
    expect(body().textContent).not.toContain('Close without copying?')
    expect(body().textContent).not.toContain('nb_abc')
  })

  it("reopening doesn't show the old token", async () => {
    await mountModal()
    await create()
    button('Copy').click()
    await settle()
    button('Done').click()
    await settle()
    expect(open.value).toBe(false)
    open.value = true
    await settle()
    expect(body().textContent).not.toContain('nb_abc')
    expect(body().querySelector('input[name="name"]')).not.toBeNull()
  })
})

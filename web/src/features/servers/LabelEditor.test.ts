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

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { http } from 'msw'
import { json, server } from '@/test/server'
import { mountApp, testRouter } from '@/test/mount'
import { makeServer } from './fixtures'
import LabelEditor from './LabelEditor.vue'

async function mountEditor(labels: Record<string, string>) {
  const router = testRouter([
    { path: '/p/:projectId/servers', name: 'servers', component: LabelEditor },
  ])
  await router.push('/p/1/servers')
  return mountApp(LabelEditor, { router, props: { server: makeServer({ labels }) } })
}

beforeEach(() => {
  document.body.innerHTML = ''
  server.use(
    http.get('*/v1/projects/1', () =>
      json(200, { id: 1, name: 'Home lab', description: '', created: '2026-11-01T00:00:00Z' }),
    ),
  )
})

const save = (w: Awaited<ReturnType<typeof mountEditor>>['wrapper']) =>
  w.findAll('button').find((b) => b.text() === 'Save labels')!

describe('LabelEditor', () => {
  it('saves with PUT labels', async () => {
    let body: unknown
    server.use(
      http.put('*/v1/servers/1', async ({ request }) => {
        body = await request.json()
        return json(200, makeServer({ labels: { env: 'prod' } }))
      }),
    )
    const { wrapper } = await mountEditor({})
    await wrapper
      .findAll('button')
      .find((b) => b.text() === 'Add label')!
      .trigger('click')
    await wrapper.find('input[name="key-0"]').setValue('env')
    await wrapper.find('input[name="value-0"]').setValue('prod')
    await save(wrapper).trigger('click')
    await vi.waitFor(() => expect(body).toEqual({ labels: { env: 'prod' } }))
  })

  it('keeps rows on error', async () => {
    server.use(
      http.put('*/v1/servers/1', () =>
        json(422, { error: { code: 'invalid_input', message: 'label rejected by the server' } }),
      ),
    )
    const { wrapper } = await mountEditor({ env: 'prod' })
    await save(wrapper).trigger('click')
    await vi.waitFor(() => expect(wrapper.text()).toContain('label rejected by the server'))
    expect((wrapper.find('input[name="key-0"]').element as HTMLInputElement).value).toBe('env')
  })

  it('remove row', async () => {
    const { wrapper } = await mountEditor({ env: 'prod', team: 'a' })
    expect(wrapper.findAll('input[name^="key-"]')).toHaveLength(2)
    await wrapper.find('button[aria-label="Remove label 1"]').trigger('click')
    expect(wrapper.findAll('input[name^="key-"]')).toHaveLength(1)
    expect((wrapper.find('input[name="key-0"]').element as HTMLInputElement).value).toBe('team')
  })

  it('bad key sends no PUT and keeps the rows', async () => {
    let puts = 0
    server.use(
      http.put('*/v1/servers/1', () => {
        puts++
        return json(200, makeServer())
      }),
    )
    const { wrapper } = await mountEditor({ env: 'prod' })
    await wrapper.find('input[name="key-0"]').setValue('bad key')
    await save(wrapper).trigger('click')
    await vi.waitFor(() => expect(wrapper.text()).toContain('Keys use letters'))
    await new Promise((r) => setTimeout(r, 30))
    expect(puts).toBe(0)
    expect((wrapper.find('input[name="key-0"]').element as HTMLInputElement).value).toBe('bad key')
    expect((wrapper.find('input[name="value-0"]').element as HTMLInputElement).value).toBe('prod')
  })

  it('a blank row does not block the save', async () => {
    let body: unknown
    server.use(
      http.put('*/v1/servers/1', async ({ request }) => {
        body = await request.json()
        return json(200, makeServer({ labels: { env: 'prod' } }))
      }),
    )
    const { wrapper } = await mountEditor({ env: 'prod' })
    await wrapper
      .findAll('button')
      .find((b) => b.text() === 'Add label')!
      .trigger('click')
    expect(wrapper.find('input[name="key-1"]').exists()).toBe(true)
    await save(wrapper).trigger('click')
    await vi.waitFor(() => expect(body).toEqual({ labels: { env: 'prod' } }))
  })
})

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
import { http } from 'msw'
import { defineComponent, h } from 'vue'
import { makeServer } from '@/features/servers/fixtures'
import { json, server } from '@/test/server'
import { mountApp, testRouter } from '@/test/mount'
import CreateVolumeModal from './CreateVolumeModal.vue'
import { makeLocation, makeVolume } from './fixtures'
import { fill, runningAction, serveLists, settle } from './testkit'

let posts: unknown[]
let unmount: () => void

beforeEach(() => {
  posts = []
  serveLists({
    locations: [
      makeLocation(),
      makeLocation({ id: 2, name: 'away' }),
      makeLocation({ id: 3, name: 'novol', volumes_available: false }),
    ],
    servers: [
      makeServer({ id: 5, name: 'web1' }),
      makeServer({ id: 6, name: 'far', location: { id: 2, name: 'away', description: '' } }),
    ],
  })
  server.use(
    http.post('*/v1/volumes', async ({ request }) => {
      posts.push(await request.json())
      return json(201, { volume: makeVolume(), action: runningAction('create_volume') })
    }),
  )
})

afterEach(() => {
  unmount()
  document.body.innerHTML = ''
})

async function open(props: Record<string, unknown> = {}) {
  const router = testRouter([
    { path: '/p/:projectId/volumes', component: defineComponent({ render: () => h('div') }) },
  ])
  await router.push('/p/7/volumes')
  const onCreated = vi.fn()
  const m = mountApp(CreateVolumeModal, { router, props: { open: true, onCreated, ...props } })
  unmount = () => m.wrapper.unmount()
  await settle()
  return { onCreated }
}

const submit = async () => {
  document.body.querySelector<HTMLFormElement>('#create-volume')!.requestSubmit()
  await settle()
}

describe('CreateVolumeModal', () => {
  it('only offers locations with volumes', async () => {
    await open()
    const opts = Array.from(document.body.querySelectorAll('select[name="location"] option'))
    expect(opts.map((o) => o.textContent?.trim())).toEqual(['home', 'away'])
  })

  it('keeps the size within 10 to 10240 GB', async () => {
    await open()
    await fill('input[name="name"]', 'data')
    for (const bad of ['9', '10241', '']) {
      await fill('input[name="size"]', bad)
      await submit()
      expect(document.body.textContent).toContain('between 10 and 10240 GB')
    }
    expect(posts).toEqual([])
  })

  it('checks the name', async () => {
    await open()
    await fill('input[name="name"]', 'Bad Name')
    await submit()
    expect(document.body.textContent).toContain('lowercase letters')
    expect(posts).toEqual([])
  })

  it('posts without a server when none is picked', async () => {
    const { onCreated } = await open()
    await fill('input[name="name"]', 'data')
    await fill('input[name="size"]', '20')
    await submit()
    expect(posts).toEqual([{ name: 'data', size: 20, location: '1' }])
    expect(onCreated).toHaveBeenCalled()
  })

  it('posts the picked server and greys out servers elsewhere', async () => {
    await open()
    const far = Array.from(
      document.body.querySelectorAll<HTMLOptionElement>('select[name="server"] option'),
    ).find((o) => o.value === '6')!
    expect(far.disabled).toBe(true)
    expect(far.textContent).toContain('In a different location')
    await fill('input[name="name"]', 'data')
    await fill('input[name="size"]', '20')
    await fill('select[name="server"]', '5')
    await submit()
    expect(posts).toEqual([{ name: 'data', size: 20, location: '1', server: '5' }])
  })

  it('attaches to a preset server in its location', async () => {
    await open({ server: makeServer({ id: 5, name: 'web1' }) })
    expect(document.body.querySelector('select[name="server"]')).toBeNull()
    expect(document.body.textContent).toContain('web1')
    await fill('input[name="name"]', 'logs')
    await submit()
    expect(posts).toEqual([{ name: 'logs', size: 10, location: '1', server: '5' }])
  })

  it('shows an API error', async () => {
    server.use(
      http.post('*/v1/volumes', () =>
        json(422, { error: { code: 'invalid_input', message: 'over quota' } }),
      ),
    )
    await open()
    await fill('input[name="name"]', 'data')
    await submit()
    expect(document.body.textContent).toContain('over quota')
  })
})

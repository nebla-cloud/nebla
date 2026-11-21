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
import { json, server } from '@/test/server'
import { mountApp, testRouter } from '@/test/mount'
import ResizeVolumeModal from './ResizeVolumeModal.vue'
import { makeVolume } from './fixtures'
import { fill, runningAction, serveLists, settle } from './testkit'

let posts: unknown[]
let unmount: () => void

beforeEach(() => {
  posts = []
  serveLists({})
  server.use(
    http.post('*/v1/volumes/:id/actions/resize', async ({ request }) => {
      posts.push(await request.json())
      return json(201, { action: runningAction('resize_volume') })
    }),
  )
})

afterEach(() => {
  unmount()
  document.body.innerHTML = ''
})

async function open() {
  const router = testRouter([
    { path: '/p/:projectId/volumes', component: defineComponent({ render: () => h('div') }) },
  ])
  await router.push('/p/7/volumes')
  const onStarted = vi.fn()
  const m = mountApp(ResizeVolumeModal, {
    router,
    props: { open: true, volume: makeVolume({ size: 20 }), onStarted },
  })
  unmount = () => m.wrapper.unmount()
  await settle()
  return { onStarted }
}

const submit = async () => {
  document.body.querySelector<HTMLFormElement>('#resize-volume')!.requestSubmit()
  await settle()
}

describe('ResizeVolumeModal', () => {
  it('only grows', async () => {
    await open()
    await fill('input[name="size"]', '20')
    await submit()
    expect(document.body.textContent).toContain('bigger than 20 GB')
    expect(posts).toEqual([])
  })

  it('posts the new size', async () => {
    const { onStarted } = await open()
    await fill('input[name="size"]', '50')
    await submit()
    expect(posts).toEqual([{ size: 50 }])
    expect(onStarted).toHaveBeenCalled()
  })
})

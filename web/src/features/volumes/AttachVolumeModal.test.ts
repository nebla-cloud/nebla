import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { http } from 'msw'
import { defineComponent, h } from 'vue'
import { makeServer } from '@/features/servers/fixtures'
import { json, server } from '@/test/server'
import { mountApp, testRouter } from '@/test/mount'
import AttachVolumeModal from './AttachVolumeModal.vue'
import { makeVolume } from './fixtures'
import { fill, runningAction, serveLists, settle } from './testkit'

let posts: unknown[]
let unmount: () => void

beforeEach(() => {
  posts = []
  serveLists({
    servers: [
      makeServer({ id: 5, name: 'web1' }),
      makeServer({ id: 6, name: 'far', location: { id: 2, name: 'away', description: '' } }),
    ],
  })
  server.use(
    http.post('*/v1/volumes/:id/actions/attach', async ({ request }) => {
      posts.push(await request.json())
      return json(201, { action: runningAction('attach_volume') })
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
  const m = mountApp(AttachVolumeModal, {
    router,
    props: { open: true, volume: makeVolume(), onStarted },
  })
  unmount = () => m.wrapper.unmount()
  await settle()
  return { onStarted }
}

const submit = async () => {
  document.body.querySelector<HTMLFormElement>('#attach-volume')!.requestSubmit()
  await settle()
}

describe('AttachVolumeModal', () => {
  it('greys out servers in a different location', async () => {
    await open()
    const opts = Array.from(
      document.body.querySelectorAll<HTMLOptionElement>('select[name="server"] option'),
    )
    const far = opts.find((o) => o.value === '6')!
    expect(far.disabled).toBe(true)
    expect(far.textContent).toContain('In a different location')
    expect(opts.find((o) => o.value === '5')!.disabled).toBe(false)
  })

  it('attaches to the picked server', async () => {
    const { onStarted } = await open()
    await fill('select[name="server"]', '5')
    await submit()
    expect(posts).toEqual([{ server: '5' }])
    expect(onStarted).toHaveBeenCalledWith(expect.objectContaining({ command: 'attach_volume' }))
  })

  it('shows the API message on volume_server_mismatch', async () => {
    server.use(
      http.post('*/v1/volumes/:id/actions/attach', () =>
        json(422, {
          error: {
            code: 'volume_server_mismatch',
            message: 'This volume is on a different host in this location',
          },
        }),
      ),
    )
    const { onStarted } = await open()
    await fill('select[name="server"]', '5')
    await submit()
    expect(document.body.textContent).toContain(
      'This volume is on a different host in this location',
    )
    expect(onStarted).not.toHaveBeenCalled()
  })
})

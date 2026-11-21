import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { http } from 'msw'
import type { Volume } from '@/api/types'
import { makeServer } from '@/features/servers/fixtures'
import { json, server } from '@/test/server'
import { mountApp, testRouter } from '@/test/mount'
import VolumeDetailPage from './VolumeDetailPage.vue'
import { makeVolume } from './fixtures'
import { button, fill, runningAction, serveLists, settle } from './testkit'

let vol: Volume
let calls: string[]
let actionState: Record<string, unknown> | null
let unmount: () => void

beforeEach(() => {
  vol = makeVolume({ id: 1, name: 'data', size: 20 })
  calls = []
  actionState = null
  serveLists({ servers: [makeServer({ id: 5, name: 'web1' })] })
  server.use(
    http.get('*/v1/volumes/:id', () => json(200, vol)),
    http.post('*/v1/volumes/:id/actions/detach', () => {
      calls.push('detach')
      return json(201, { action: runningAction('detach_volume') })
    }),
    http.delete('*/v1/volumes/:id', () => {
      calls.push('delete')
      return json(200, { action: runningAction('delete_volume') })
    }),
    http.get('*/v1/actions/:id', () => json(200, actionState ?? runningAction('detach_volume'))),
  )
})

afterEach(() => {
  unmount()
  document.body.innerHTML = ''
})

async function mountPage() {
  const router = testRouter([
    { path: '/p/:projectId/volumes', name: 'volumes', component: VolumeDetailPage },
    { path: '/p/:projectId/volumes/:volumeId', name: 'volume', component: VolumeDetailPage },
    { path: '/p/:projectId/servers/:serverId', name: 'server', component: VolumeDetailPage },
  ])
  await router.push('/p/7/volumes/1')
  const m = mountApp(VolumeDetailPage, { router })
  unmount = () => m.wrapper.unmount()
  await settle()
  return m
}

describe('VolumeDetailPage', () => {
  it('shows the device path with copy and the mount hint', async () => {
    await mountPage()
    expect(document.body.textContent).toContain('20 GB')
    const device = document.body.querySelector('[data-testid="linux-device"]')!
    expect(device.textContent).toContain('/dev/disk/by-id/virtio-nb-vol-1')
    expect(button('Copy')).toBeTruthy()
    const hint = document.body.querySelector('[data-testid="mount-hint"]')!.textContent!
    expect(hint).toContain('sudo mkfs.ext4 /dev/disk/by-id/virtio-nb-vol-1')
    expect(hint).toContain('sudo mount /dev/disk/by-id/virtio-nb-vol-1 /mnt/data')
    expect(document.body.textContent).toContain('by UUID in /etc/fstab')
  })

  it('names the attached server and blocks delete', async () => {
    vol = makeVolume({ status: 'attached', server: 5 })
    await mountPage()
    expect(document.body.textContent).toContain('web1')
    expect(button('Delete').disabled).toBe(true)
    expect(button('Detach').disabled).toBe(false)
  })

  it('detaches after confirming and shows a power-off failure', async () => {
    vol = makeVolume({ status: 'attached', server: 5 })
    await mountPage()
    button('Detach').click()
    await settle()
    const confirm = Array.from(document.body.querySelectorAll('[role="dialog"] button')).find(
      (b) => b.textContent?.trim() === 'Detach',
    ) as HTMLButtonElement
    actionState = {
      ...runningAction('detach_volume'),
      status: 'error',
      error: { code: 'power_off_required', message: 'Power off the server to detach the volume.' },
    }
    confirm.click()
    await settle()
    expect(calls).toEqual(['detach'])
    await settle()
    expect(document.body.textContent).toContain('Power off the server to detach the volume.')
  })

  it('deletes by name', async () => {
    await mountPage()
    button('Delete').click()
    await settle()
    expect(button('Delete volume').disabled).toBe(true)
    await fill('input[name="confirm_name"]', 'data')
    button('Delete volume').click()
    await settle()
    expect(calls).toEqual(['delete'])
  })
})

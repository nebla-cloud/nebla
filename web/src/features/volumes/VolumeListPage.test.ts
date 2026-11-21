import { afterEach, describe, expect, it } from 'vitest'
import { http } from 'msw'
import { makeServer } from '@/features/servers/fixtures'
import { json, server } from '@/test/server'
import { mountApp, testRouter } from '@/test/mount'
import VolumeListPage from './VolumeListPage.vue'
import { makeLocation, makeVolume } from './fixtures'
import { button, fill, runningAction, serveLists, settle } from './testkit'

let unmount: () => void

afterEach(() => {
  unmount()
  document.body.innerHTML = ''
})

async function mountPage() {
  const router = testRouter([
    { path: '/p/:projectId/volumes', name: 'volumes', component: VolumeListPage },
    { path: '/p/:projectId/volumes/:volumeId', name: 'volume', component: VolumeListPage },
  ])
  await router.push('/p/7/volumes')
  const m = mountApp(VolumeListPage, { router })
  unmount = () => m.wrapper.unmount()
  await settle()
  return m
}

describe('VolumeListPage', () => {
  it('shows an empty state with a create button', async () => {
    serveLists({ locations: [makeLocation()], volumes: [] })
    await mountPage()
    expect(document.body.textContent).toContain('No volumes yet')
    expect(button('Create volume')).toBeTruthy()
  })

  it('explains when no location offers volumes', async () => {
    serveLists({ locations: [makeLocation({ volumes_available: false })], volumes: [] })
    await mountPage()
    expect(document.body.textContent).toContain("Volumes aren't available here yet")
    expect(() => button('Create volume')).toThrow()
  })

  it('lists volumes with size, server and status', async () => {
    serveLists({
      locations: [makeLocation()],
      servers: [makeServer({ id: 5, name: 'web1' })],
      volumes: [
        makeVolume({ id: 1, name: 'data', size: 20, status: 'attached', server: 5 }),
        makeVolume({ id: 2, name: 'spare', size: 100 }),
      ],
    })
    const { wrapper } = await mountPage()
    const rows = wrapper.findAll('li')
    expect(rows).toHaveLength(2)
    expect(rows[0]!.text()).toContain('data')
    expect(rows[0]!.text()).toContain('20 GB')
    expect(rows[0]!.text()).toContain('web1')
    expect(rows[0]!.find('[role="img"]').attributes('aria-label')).toBe('Attached')
    expect(rows[1]!.text()).toContain('100 GB')
    expect(rows[1]!.text()).toContain('Not attached')
    expect(rows[0]!.find('a').attributes('href')).toBe('/p/7/volumes/1')
  })

  it('creates a volume and shows it', async () => {
    serveLists({ locations: [makeLocation()], volumes: [] })
    let created = false
    server.use(
      http.post('*/v1/volumes', () => {
        created = true
        return json(201, {
          volume: makeVolume({ status: 'creating' }),
          action: runningAction('create_volume'),
        })
      }),
      http.get('*/v1/volumes', () =>
        json(200, {
          volumes: created ? [makeVolume({ status: 'creating' })] : [],
          meta: { pagination: { page: 1, per_page: 50, total_entries: 0 } },
        }),
      ),
    )
    await mountPage()
    button('Create volume').click()
    await settle()
    await fill('input[name="name"]', 'data')
    document.body.querySelector<HTMLFormElement>('#create-volume')!.requestSubmit()
    await settle()
    expect(created).toBe(true)
    expect(document.body.textContent).toContain('Creating data')
  })
})

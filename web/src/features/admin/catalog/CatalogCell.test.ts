import { afterEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, h } from 'vue'
import { flushPromises } from '@vue/test-utils'
import { http } from 'msw'
import { useToast } from '@nuxt/ui/composables/useToast'
import { json, server } from '@/test/server'
import { mountApp } from '@/test/mount'
import type { CatalogEntry, CatalogLocationState, CatalogNode } from './queries'
import { useCatalog } from './queries'
import CatalogCell from './CatalogCell.vue'

const node = (n: string, status: string, error = ''): CatalogNode => ({
  node: n,
  status,
  vmid: status === 'ready' ? 9000 : null,
  file: 'debian-13-genericcloud-amd64.qcow2',
  built_at: status === 'ready' ? '2026-11-18T10:00:00Z' : null,
  error,
})

const loc = (state: string, nodes: CatalogNode[] = []): CatalogLocationState => ({
  location_id: 1,
  available: true,
  reason: '',
  state,
  nodes,
})

let retired = false

const entry = (l: CatalogLocationState): CatalogEntry => ({
  key: 'debian-13',
  description: 'Debian 13',
  os_flavor: 'debian',
  os_version: '13',
  retired,
  locations: [l],
})

// Renders the cell from the live catalog query, like the page does.
const Harness = defineComponent({
  setup() {
    const { data } = useCatalog()
    return () => {
      const e = data.value?.[0]
      return e ? h(CatalogCell, { entry: e, loc: e.locations[0]!, locationName: 'rack-2' }) : null
    }
  },
})

function serveCatalog(...states: CatalogLocationState[]) {
  let calls = 0
  server.use(
    http.get('*/v1/admin/catalog', () => {
      const s = states[Math.min(calls, states.length - 1)]!
      calls++
      return json(200, { catalog: [entry(s)] })
    }),
  )
}

function button(label: string): HTMLButtonElement {
  const b = Array.from(document.body.querySelectorAll('button')).find(
    (x) => x.textContent?.trim() === label,
  )
  if (!b) throw new Error(`button ${label} not found`)
  return b as HTMLButtonElement
}

function buttons(label: string): HTMLButtonElement[] {
  return Array.from(document.body.querySelectorAll('button')).filter(
    (x) => x.textContent?.trim() === label,
  ) as HTMLButtonElement[]
}

async function openPopover() {
  const trigger = document.body.querySelector('[data-cell-trigger]') as HTMLButtonElement
  trigger.click()
  await flushPromises()
}

async function mountCell() {
  mountApp(Harness)
  await vi.waitFor(() => expect(document.body.querySelector('[data-cell-trigger]')).not.toBe(null))
}

afterEach(() => {
  useToast().clear()
  retired = false
  document.body.innerHTML = ''
})

describe('CatalogCell', () => {
  it('retry calls enable', async () => {
    const failed = loc('failed', [node('pve1', 'failed', 'checksum mismatch')])
    serveCatalog(failed, loc('building', [node('pve1', 'building')]))
    const posts: string[] = []
    server.use(
      http.post('*/v1/admin/catalog/:key/locations/:id/:action', ({ request }) => {
        posts.push(new URL(request.url).pathname)
        return json(200, { location: loc('building', [node('pve1', 'building')]) })
      }),
    )
    await mountCell()
    button('Retry').click()
    await vi.waitFor(() =>
      expect(posts).toEqual(['/v1/admin/catalog/debian-13/locations/1/enable']),
    )
    await vi.waitFor(() => expect(document.body.textContent).toContain('Building'))
  })

  it('409 on enable while building is silent', async () => {
    serveCatalog(loc('off'), loc('building', [node('pve1', 'building')]))
    server.use(
      http.post('*/v1/admin/catalog/debian-13/locations/1/enable', () =>
        json(409, { error: { code: 'conflict', message: 'a build is already running' } }),
      ),
    )
    await mountCell()
    button('Enable').click()
    await flushPromises()
    await vi.waitFor(() => expect(document.body.textContent).toContain('Building'))
    expect(document.body.textContent).not.toContain('a build is already running')
    expect(document.body.textContent).not.toContain("Couldn't")
  })

  it('409 that is not a running build shows a toast', async () => {
    serveCatalog(loc('off'))
    server.use(
      http.post('*/v1/admin/catalog/debian-13/locations/1/enable', () =>
        json(409, {
          error: { code: 'conflict', message: 'an image named "debian-13" already exists' },
        }),
      ),
    )
    await mountCell()
    button('Enable').click()
    await vi.waitFor(() =>
      expect(document.body.textContent).toContain("Couldn't enable Debian 13 in rack-2"),
    )
    expect(document.body.textContent).toContain('an image named "debian-13" already exists')
  })

  it('retired entries offer no enable where nothing is built', async () => {
    retired = true
    serveCatalog(loc('off'))
    await mountCell()
    expect(document.body.textContent).toContain('Not built')
    await openPopover()
    expect(buttons('Enable')).toHaveLength(0)
    expect(buttons('Disable')).toHaveLength(0)
  })

  it('retired entries with builds only offer disable', async () => {
    retired = true
    serveCatalog(loc('retired', [node('pve1', 'ready')]))
    await mountCell()
    await openPopover()
    expect(buttons('Disable')).toHaveLength(1)
    expect([...buttons('Refresh'), ...buttons('Retry'), ...buttons('Enable')]).toHaveLength(0)
  })

  it('other enable errors show a toast', async () => {
    serveCatalog(loc('off'))
    server.use(
      http.post('*/v1/admin/catalog/debian-13/locations/1/enable', () =>
        json(422, { error: { code: 'invalid', message: 'no import storage' } }),
      ),
    )
    await mountCell()
    button('Enable').click()
    await vi.waitFor(() =>
      expect(document.body.textContent).toContain("Couldn't enable Debian 13 in rack-2"),
    )
    expect(document.body.textContent).toContain('no import storage')
  })

  it('disable is in the popover and confirms', async () => {
    serveCatalog(loc('ready', [node('pve1', 'ready')]), loc('off'))
    const posts: string[] = []
    server.use(
      http.post('*/v1/admin/catalog/:key/locations/:id/:action', ({ request }) => {
        posts.push(new URL(request.url).pathname)
        return json(200, { location: loc('off') })
      }),
    )
    await mountCell()
    expect(buttons('Disable')).toHaveLength(0)
    await openPopover()
    button('Disable').click()
    await flushPromises()
    expect(posts).toEqual([])
    const confirm = buttons('Disable').at(-1)!
    confirm.click()
    await vi.waitFor(() =>
      expect(posts).toEqual(['/v1/admin/catalog/debian-13/locations/1/disable']),
    )
    await vi.waitFor(() => expect(document.body.textContent).toContain('Off'))
  })

  it('popover lists per-node errors', async () => {
    serveCatalog(
      loc('partial', [
        node('pve1', 'ready'),
        node('pve2', 'ready'),
        node('pve3', 'failed', 'checksum mismatch'),
      ]),
    )
    await mountCell()
    expect(document.body.textContent).toContain('2 of 3 nodes')
    await openPopover()
    const pop = document.body.textContent ?? ''
    expect(pop).toContain('Debian 13 in rack-2')
    expect(pop).toContain('pve3')
    expect(pop).toContain('checksum mismatch')
  })
})

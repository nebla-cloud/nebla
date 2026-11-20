import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { http } from 'msw'
import { json, server } from '@/test/server'
import { mountApp, testRouter } from '@/test/mount'
import type { Action, Server } from '@/api/types'
import { makeServer } from './fixtures'
import ServerDetailPage from './ServerDetailPage.vue'

function action(over: Partial<Action> = {}): Action {
  return {
    id: 7,
    command: 'reboot_server',
    status: 'running',
    progress: 40,
    resources: [{ id: 1, type: 'server' }],
    error: null,
    started: '2026-11-20T10:00:00Z',
    finished: null,
    ...over,
  }
}

function serve(s: Server) {
  server.use(
    http.get('*/v1/projects/1', () =>
      json(200, { id: 1, name: 'Home lab', description: '', created: '2026-11-01T00:00:00Z' }),
    ),
    http.get('*/v1/servers/1', () => json(200, s)),
  )
}

async function mountDetail() {
  const router = testRouter([
    { path: '/p/:projectId/servers', name: 'servers', component: ServerDetailPage },
    { path: '/p/:projectId/servers/:serverId', name: 'server', component: ServerDetailPage },
  ])
  await router.push('/p/1/servers/1')
  const res = mountApp(ServerDetailPage, { router })
  await vi.waitFor(() => expect(res.wrapper.text()).not.toContain('Loading server'))
  return { ...res, router }
}

const buttons = () => Array.from(document.body.querySelectorAll('button'))
const byText = (t: string) => buttons().find((b) => b.textContent?.trim() === t)
const toastText = () => document.body.textContent ?? ''

async function openMore() {
  ;(
    document.body.querySelector('button[aria-label="More power actions"]') as HTMLElement
  ).dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
  await vi.waitFor(() => expect(document.body.querySelector('[role="menuitem"]')).toBeTruthy())
}

beforeEach(() => {
  document.body.innerHTML = ''
})
afterEach(() => {
  document.body.innerHTML = ''
})

describe('ServerDetailPage', () => {
  it('detail power actions by status', async () => {
    serve(
      makeServer({
        status: 'running',
        public_net: { ipv4: { ip: '203.0.113.10' }, ipv6: { ip: '2001:db8::10' } },
      }),
    )
    const { wrapper } = await mountDetail()
    expect(wrapper.text()).toContain('2001:db8::10')
    expect(wrapper.text()).toContain('2 vCPU, 4 GB, 40 GB')
    expect(byText('Shut down')).toBeTruthy()
    expect(byText('Reboot')).toBeTruthy()
    expect(byText('Power on')).toBeUndefined()
    expect(byText('Power off')).toBeUndefined()
    await openMore()
    const items = Array.from(document.body.querySelectorAll('[role="menuitem"]')).map((e) =>
      e.textContent?.trim(),
    )
    expect(items).toEqual(['Power off', 'Reset'])
  })

  it('off server shows Power on only', async () => {
    serve(makeServer({ status: 'off' }))
    await mountDetail()
    expect(byText('Power on')).toBeTruthy()
    expect(byText('Shut down')).toBeUndefined()
    expect(byText('Reboot')).toBeUndefined()
    expect(document.body.querySelector('button[aria-label="More power actions"]')).toBeNull()
  })

  it('transitional status disables every power action', async () => {
    serve(makeServer({ status: 'starting' }))
    await mountDetail()
    expect(byText('Shut down')!.disabled).toBe(true)
    expect(byText('Reboot')!.disabled).toBe(true)
    const more = document.body.querySelector(
      'button[aria-label="More power actions"]',
    ) as HTMLButtonElement
    expect(more.disabled).toBe(true)
  })

  it('reboot shows inline progress then toast', async () => {
    serve(makeServer())
    let polls = 0
    server.use(
      http.post('*/v1/servers/1/actions/reboot', () => json(201, { action: action() })),
      http.get('*/v1/actions/7', () => {
        polls++
        return json(
          200,
          action(
            polls > 1 ? { status: 'success', progress: 100, finished: '2026-11-20T10:00:02Z' } : {},
          ),
        )
      }),
    )
    const { wrapper } = await mountDetail()
    byText('Reboot')!.click()
    await vi.waitFor(() => expect(wrapper.text()).toContain('Rebooting'))
    expect(wrapper.find('[role="status"]').exists()).toBe(true)
    await vi.waitFor(() => expect(toastText()).toContain('web1 rebooted'), { timeout: 4000 })
  })

  it('failed action shows its message, stops polling and re-enables buttons', async () => {
    serve(makeServer())
    let polls = 0
    server.use(
      http.post('*/v1/servers/1/actions/reboot', () => json(201, { action: action() })),
      http.get('*/v1/actions/7', () => {
        polls++
        return json(
          200,
          action({
            status: 'error',
            error: { code: 'pve_error', message: 'Proxmox refused to reboot' },
            finished: '2026-11-20T10:00:02Z',
          }),
        )
      }),
    )
    const { wrapper } = await mountDetail()
    byText('Reboot')!.click()
    await vi.waitFor(() => expect(wrapper.text()).toContain('Proxmox refused to reboot'))
    const seen = polls
    vi.useFakeTimers()
    try {
      await vi.advanceTimersByTimeAsync(10_000)
    } finally {
      vi.useRealTimers()
    }
    expect(polls).toBe(seen)
    expect(byText('Shut down')!.disabled).toBe(false)
    expect(byText('Reboot')!.disabled).toBe(false)
  })

  it('delete requires typing the name and returns to list', async () => {
    serve(makeServer())
    server.use(
      http.delete('*/v1/servers/1', () =>
        json(200, {
          action: action({ id: 8, command: 'delete_server', status: 'success', progress: 100 }),
        }),
      ),
      http.get('*/v1/actions/8', () =>
        json(200, action({ id: 8, command: 'delete_server', status: 'success', progress: 100 })),
      ),
      http.get('*/v1/servers', () =>
        json(200, {
          servers: [],
          meta: { pagination: { page: 1, per_page: 50, total_entries: 0 } },
        }),
      ),
    )
    const { router } = await mountDetail()
    byText('Delete server')!.click()
    await flushPromises()
    const confirm = () =>
      buttons()
        .filter((b) => b.textContent?.trim() === 'Delete server')
        .at(-1)!
    expect(confirm().disabled).toBe(true)
    const input = document.body.querySelector('input[name="confirm_name"]') as HTMLInputElement
    input.value = 'web1'
    input.dispatchEvent(new Event('input'))
    await flushPromises()
    expect(confirm().disabled).toBe(false)
    confirm().click()
    await vi.waitFor(() => expect(router.currentRoute.value.name).toBe('servers'))
    expect(toastText()).toContain('web1 deleted')
  })

  it('rename sends PUT name and validates pattern locally', async () => {
    serve(makeServer())
    const bodies: unknown[] = []
    server.use(
      http.put('*/v1/servers/1', async ({ request }) => {
        const body = (await request.json()) as { name: string }
        bodies.push(body)
        return json(200, makeServer({ name: body.name }))
      }),
      http.get('*/v1/servers', () =>
        json(200, {
          servers: [],
          meta: { pagination: { page: 1, per_page: 50, total_entries: 0 } },
        }),
      ),
    )
    const { wrapper } = await mountDetail()
    byText('Rename')!.click()
    await flushPromises()
    const input = wrapper.find('input[name="name"]')
    await input.setValue('Web_1')
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(bodies).toHaveLength(0)
    expect(wrapper.text()).toContain('lowercase letters')
    await input.setValue('web-2')
    await wrapper.find('form').trigger('submit')
    await vi.waitFor(() => expect(bodies).toEqual([{ name: 'web-2' }]))
    await vi.waitFor(() => expect(wrapper.text()).toContain('web-2'))
  })
})

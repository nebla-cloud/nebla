import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { http } from 'msw'
import { json, server } from '@/test/server'
import { RouterView, type HistoryState } from 'vue-router'
import { mountApp, testRouter } from '@/test/mount'
import type { Action, Server } from '@/api/types'
import { makeServer } from './fixtures'
import routes from './routes'

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

const emptyList = { servers: [], meta: { pagination: { page: 1, per_page: 50, total_entries: 0 } } }

function serve(s: Server) {
  server.use(
    http.get('*/v1/projects/1', () =>
      json(200, { id: 1, name: 'Home lab', description: '', created: '2026-11-01T00:00:00Z' }),
    ),
    http.get(`*/v1/servers/${s.id}`, () => json(200, s)),
  )
}

async function mountDetail(path = '/p/1/servers/1', state?: HistoryState) {
  const router = testRouter([
    {
      path: '/p/:projectId',
      component: RouterView,
      children: [{ path: 'servers/new', name: 'server-new', component: RouterView }, ...routes],
    },
  ])
  await router.push({ path, state })
  const res = mountApp(RouterView, { router })
  await vi.waitFor(() => expect(res.wrapper.text()).not.toContain('Loading server'))
  await flushPromises()
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

describe('ServerDetailShell', () => {
  it('child route renders its tab', async () => {
    serve(makeServer({ id: 5 }))
    const { wrapper } = await mountDetail('/p/1/servers/5/rebuild')
    expect(wrapper.text()).toContain('Coming soon')
    const current = wrapper.find('nav[aria-label="Server sections"] a[aria-current="page"]')
    expect(current.text()).toBe('Rebuild')
  })

  it('tab bar links keep the server', async () => {
    serve(makeServer({ id: 5 }))
    const { wrapper } = await mountDetail('/p/1/servers/5')
    const links = wrapper
      .findAll('nav[aria-label="Server sections"] a')
      .map((a) => [a.text(), a.attributes('href')])
    expect(links).toEqual([
      ['Overview', '/p/1/servers/5'],
      ['Graphs', '/p/1/servers/5/graphs'],
      ['Console', '/p/1/servers/5/console'],
      ['Rebuild', '/p/1/servers/5/rebuild'],
      ['Resize', '/p/1/servers/5/resize'],
      ['Activity', '/p/1/servers/5/activity'],
    ])
  })

  it('header and chips stay across tabs', async () => {
    serve(makeServer({ id: 5 }))
    const { wrapper, router } = await mountDetail('/p/1/servers/5')
    for (const tab of ['', '/graphs', '/console', '/rebuild', '/resize', '/activity']) {
      await router.push(`/p/1/servers/5${tab}`)
      await flushPromises()
      expect(wrapper.find('h1').text()).toBe('web1')
      expect(wrapper.text()).toContain('Running')
      expect(byText('Shut down')).toBeTruthy()
      expect(byText('Reboot')).toBeTruthy()
    }
  })

  it('breadcrumb links back to the server list', async () => {
    serve(makeServer())
    const { wrapper } = await mountDetail()
    const crumb = wrapper.find('nav[aria-label="Breadcrumb"]')
    expect(crumb.find('a').attributes('href')).toBe('/p/1/servers')
    expect(crumb.find('[aria-current="page"]').text()).toBe('web1')
  })

  it('handoff ignored for another server', async () => {
    serve(makeServer({ id: 5 }))
    let polled = false
    server.use(
      http.get('*/v1/actions/3', () => {
        polled = true
        return json(
          200,
          action({ id: 3, command: 'create_server', resources: [{ id: 9, type: 'server' }] }),
        )
      }),
    )
    const { wrapper, router } = await mountDetail('/p/1/servers/5', { actionId: 3 })
    await flushPromises()
    await new Promise((r) => setTimeout(r, 50))
    expect(polled).toBe(true)
    expect(wrapper.find('[role="status"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('Creating')
    expect(router.options.history.state.actionId ?? null).toBeNull()
  })

  it('create shows created toast', async () => {
    serve(makeServer({ id: 5, status: 'initializing' }))
    let polls = 0
    server.use(
      http.get('*/v1/actions/3', () => {
        polls++
        const done = polls > 1
        return json(
          200,
          action({
            id: 3,
            command: 'create_server',
            resources: [{ id: 5, type: 'server' }],
            ...(done ? { status: 'success', progress: 100, finished: '2026-11-20T10:01:00Z' } : {}),
          }),
        )
      }),
      http.get('*/v1/servers', () => json(200, emptyList)),
    )
    const { wrapper, router } = await mountDetail('/p/1/servers/5', { actionId: 3 })
    await vi.waitFor(() => expect(wrapper.text()).toContain('Creating'))
    expect(router.options.history.state.actionId ?? null).toBeNull()
    await vi.waitFor(() => expect(toastText()).toContain('web1 created'), { timeout: 4000 })
  })

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

  it('error server offers Power on and Power off', async () => {
    serve(makeServer({ status: 'error' }))
    await mountDetail()
    expect(byText('Power on')!.disabled).toBe(false)
    expect(byText('Power off')!.disabled).toBe(false)
    expect(byText('Shut down')).toBeUndefined()
  })

  it('missing server shows Power on disabled with a reason', async () => {
    serve(makeServer({ status: 'missing' }))
    await mountDetail()
    const on = byText('Power on')!
    expect(on.disabled).toBe(true)
    expect(on.getAttribute('title')).toBe("Nebla can't find this server right now")
    expect(byText('Power off')).toBeUndefined()
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
      http.get('*/v1/servers', () => json(200, emptyList)),
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
      http.get('*/v1/servers', () => json(200, emptyList)),
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

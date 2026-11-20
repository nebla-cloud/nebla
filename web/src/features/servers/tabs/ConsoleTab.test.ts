import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { http } from 'msw'
import { RouterView } from 'vue-router'
import { json, server } from '@/test/server'
import { mountApp, testRouter } from '@/test/mount'
import type { Server } from '@/api/types'
import { makeServer } from '../fixtures'
import routes from '../routes'
import consoleRoutes from '../console/routes'

const screen = vi.hoisted(() => ({
  props: null as null | { url: string; password: string },
  emit: null as null | ((e: string, ...args: unknown[]) => void),
  sendCtrlAltDel: vi.fn(),
  typeText: vi.fn(),
  focus: vi.fn(),
  unmounts: 0,
}))

vi.mock('../console/VncScreen.vue', async () => {
  const { defineComponent, h, onUnmounted } = await import('vue')
  return {
    default: defineComponent({
      props: { url: { type: String, required: true }, password: { type: String, required: true } },
      emits: ['connect', 'disconnect', 'credentials-required'],
      setup(props, { emit, expose }) {
        screen.props = props
        screen.emit = emit as (e: string, ...args: unknown[]) => void
        expose({
          sendCtrlAltDel: screen.sendCtrlAltDel,
          typeText: screen.typeText,
          focus: screen.focus,
        })
        onUnmounted(() => {
          screen.unmounts++
        })
        return () => h('div', { 'data-testid': 'vnc', 'data-url': props.url })
      },
    }),
  }
})

let grantCount = 0

function serve(s: Server) {
  grantCount = 0
  server.use(
    http.get('*/v1/projects/1', () =>
      json(200, { id: 1, name: 'Home lab', description: '', created: '2026-11-01T00:00:00Z' }),
    ),
    http.get(`*/v1/servers/${s.id}`, () => json(200, s)),
    http.post(`*/v1/servers/${s.id}/actions/request_console`, () => {
      grantCount++
      return json(201, { wss_url: `wss://panel/v1/console/t${grantCount}`, password: 'pw' })
    }),
  )
}

async function mountConsole(s: Server = makeServer()) {
  serve(s)
  const router = testRouter([
    { path: '/p/:projectId', component: RouterView, children: routes },
    ...consoleRoutes,
  ])
  await router.push(`/p/1/servers/${s.id}/console`)
  const res = mountApp(RouterView, { router })
  await vi.waitFor(() => expect(res.wrapper.text()).not.toContain('Loading server'))
  await flushPromises()
  return res
}

const text = () => document.body.textContent ?? ''
const byText = (t: string) =>
  Array.from(document.body.querySelectorAll('button')).find(
    (b) => b.textContent?.trim() === t || b.getAttribute('aria-label') === t,
  ) as HTMLButtonElement | undefined

async function connected() {
  await vi.waitFor(() => expect(screen.emit).toBeTruthy())
  screen.emit!('connect')
  await flushPromises()
}

beforeEach(() => {
  document.body.innerHTML = ''
  screen.props = null
  screen.emit = null
  screen.unmounts = 0
  screen.sendCtrlAltDel.mockReset()
  screen.typeText.mockReset()
  screen.focus.mockReset()
})
afterEach(() => {
  vi.useRealTimers()
  vi.restoreAllMocks()
  document.body.innerHTML = ''
})

describe('ConsoleTab', () => {
  it('connects on mount and shows Connecting then Connected', async () => {
    await mountConsole()
    expect(grantCount).toBe(1)
    expect(screen.props?.url).toBe('wss://panel/v1/console/t1')
    expect(screen.props?.password).toBe('pw')
    expect(text()).toContain('Connecting')
    await connected()
    expect(text()).toContain('Connected')
    expect(text()).not.toContain('Connecting')
    expect(screen.focus).toHaveBeenCalled()
  })

  it('shows that keys go to the server while connected', async () => {
    await mountConsole()
    expect(text()).not.toContain('Keys go to the server')
    await connected()
    expect(text()).toContain('Keys go to the server')
  })

  it('not running shows power on', async () => {
    await mountConsole(makeServer({ status: 'off' }))
    expect(grantCount).toBe(0)
    expect(byText('Power on')).toBeTruthy()
    expect(document.body.querySelector('[data-testid="vnc"]')).toBeNull()
  })

  it('disconnect shows reconnect', async () => {
    await mountConsole()
    await connected()
    screen.emit!('disconnect', true)
    await flushPromises()
    expect(text()).toContain('Disconnected')
    expect(document.body.querySelector('[data-testid="vnc"]')).toBeNull()
    byText('Reconnect')!.click()
    await vi.waitFor(() => expect(grantCount).toBe(2))
    await flushPromises()
    expect(screen.props?.url).toBe('wss://panel/v1/console/t2')
  })

  it('ctrl alt del calls screen', async () => {
    await mountConsole()
    await connected()
    byText('Ctrl+Alt+Del')!.click()
    expect(screen.sendCtrlAltDel).toHaveBeenCalledTimes(1)
  })

  it('paste sends text and caps at 4000', async () => {
    await mountConsole()
    await connected()
    byText('Paste text')!.click()
    await vi.waitFor(() => expect(document.body.querySelector('textarea')).toBeTruthy())
    const area = document.body.querySelector('textarea') as HTMLTextAreaElement

    area.value = 'x'.repeat(4001)
    area.dispatchEvent(new Event('input'))
    await flushPromises()
    byText('Type it')!.click()
    await flushPromises()
    expect(screen.typeText).not.toHaveBeenCalled()
    expect(text()).toContain('more than 4,000 characters')

    area.value = 'echo hi\n'
    area.dispatchEvent(new Event('input'))
    await flushPromises()
    byText('Type it')!.click()
    await flushPromises()
    expect(screen.typeText).toHaveBeenCalledWith('echo hi\n')
  })

  it('paste names characters it cannot type and sends nothing', async () => {
    await mountConsole()
    await connected()
    byText('Paste text')!.click()
    await vi.waitFor(() => expect(document.body.querySelector('textarea')).toBeTruthy())
    const area = document.body.querySelector('textarea') as HTMLTextAreaElement
    area.value = 'café ok'
    area.dispatchEvent(new Event('input'))
    await flushPromises()
    expect(text()).toContain("can't type these characters: é")
    byText('Type it')!.click()
    await flushPromises()
    expect(screen.typeText).not.toHaveBeenCalled()
  })

  it('pop out disconnects inline and opens window', async () => {
    const popup = { closed: false, close: vi.fn(), focus: vi.fn() }
    const open = vi.spyOn(window, 'open').mockReturnValue(popup as unknown as Window)
    await mountConsole()
    await connected()
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] })

    byText('Pop out')!.click()
    await flushPromises()
    expect(open).toHaveBeenCalledTimes(1)
    expect(open.mock.calls[0]![0]).toBe('/console/1/1')
    expect(document.body.querySelector('[data-testid="vnc"]')).toBeNull()
    expect(screen.unmounts).toBe(1)
    expect(text()).toContain('Open in another window')

    vi.advanceTimersByTime(1000)
    await flushPromises()
    expect(byText('Reconnect')).toBeUndefined()

    popup.closed = true
    vi.advanceTimersByTime(1000)
    await flushPromises()
    expect(text()).not.toContain('Open in another window')
    expect(byText('Reconnect')).toBeTruthy()
    expect(grantCount).toBe(1)
  })

  it('leaving the tab disconnects', async () => {
    const { router } = await mountConsole()
    await connected()
    await router.push('/p/1/servers/1/rebuild')
    await flushPromises()
    expect(screen.unmounts).toBe(1)
    expect(document.body.querySelector('[data-testid="vnc"]')).toBeNull()
  })

  it('window route renders only the console and requests its own grant', async () => {
    serve(makeServer())
    const router = testRouter([
      { path: '/p/:projectId', component: RouterView, children: routes },
      ...consoleRoutes,
    ])
    await router.push('/console/1/1')
    mountApp(RouterView, { router })
    await vi.waitFor(() => expect(grantCount).toBe(1))
    await flushPromises()
    expect(screen.props?.url).toBe('wss://panel/v1/console/t1')
    expect(byText('Pop out')).toBeUndefined()
    expect(document.body.querySelector('nav[aria-label="Server sections"]')).toBeNull()
    await vi.waitFor(() => expect(text()).toContain('web1 console'))
  })
})

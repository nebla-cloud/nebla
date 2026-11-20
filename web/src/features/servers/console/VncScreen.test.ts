import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import VncScreen from './VncScreen.vue'

const fake = vi.hoisted(() => ({
  instances: [] as Array<{
    keys: Array<[number, string | null, boolean | undefined]>
    disconnects: number
    options: unknown
    fire: (type: string, detail?: unknown) => void
  }>,
  throwOnCreate: false,
}))

vi.mock('@novnc/novnc', () => ({
  default: class extends EventTarget {
    keys: Array<[number, string | null, boolean | undefined]> = []
    disconnects = 0
    options: unknown
    scaleViewport = false
    resizeSession = false
    background = ''
    constructor(_t: HTMLElement, _url: string, options: unknown) {
      super()
      if (fake.throwOnCreate) throw new Error('bad url')
      this.options = options
      fake.instances.push(this)
    }
    fire(type: string, detail?: unknown) {
      this.dispatchEvent(new CustomEvent(type, { detail }))
    }
    sendKey(keysym: number, code: string | null, down?: boolean) {
      this.keys.push([keysym, code, down])
    }
    disconnect() {
      this.disconnects++
    }
    sendCtrlAltDel() {}
    focus() {}
  },
}))

const SHIFT_DOWN: [number, string, boolean] = [0xffe1, 'ShiftLeft', true]
const SHIFT_UP: [number, string, boolean] = [0xffe1, 'ShiftLeft', false]
const key = (sym: number): [number, null, undefined] => [sym, null, undefined]

function mountScreen() {
  const w = mount(VncScreen, { props: { url: 'wss://panel/v1/console/t', password: 'pw' } })
  const vm = w.vm as unknown as { typeText: (t: string) => boolean }
  return { w, vm, rfb: fake.instances.at(-1)! }
}

beforeEach(() => {
  fake.instances = []
  fake.throwOnCreate = false
})
afterEach(() => {
  vi.restoreAllMocks()
})

describe('VncScreen', () => {
  it('passes the password as the VNC credential', () => {
    const { rfb } = mountScreen()
    expect(rfb.options).toEqual({ credentials: { password: 'pw' } })
  })

  it('types Aa1! holding shift for shifted characters', () => {
    const { vm, rfb } = mountScreen()
    expect(vm.typeText('Aa1!')).toBe(true)
    expect(rfb.keys).toEqual([
      SHIFT_DOWN,
      key(0x41),
      SHIFT_UP,
      key(0x61),
      key(0x31),
      SHIFT_DOWN,
      key(0x21),
      SHIFT_UP,
    ])
  })

  it('types $HOME holding shift for every character', () => {
    const { vm, rfb } = mountScreen()
    vm.typeText('$HOME')
    expect(rfb.keys).toEqual(
      [0x24, 0x48, 0x4f, 0x4d, 0x45].flatMap((s) => [SHIFT_DOWN, key(s), SHIFT_UP]),
    )
  })

  it('releases shift even if a shifted key fails', () => {
    const { vm, rfb } = mountScreen()
    type Send = (s: number, c: string | null, d?: boolean) => void
    const r = rfb as unknown as { sendKey: Send }
    const send = r.sendKey.bind(rfb)
    r.sendKey = (s, c, d) => {
      if (s === 0x41) throw new Error('socket closed')
      send(s, c, d)
    }
    expect(() => vm.typeText('A')).toThrow('socket closed')
    expect(rfb.keys).toEqual([SHIFT_DOWN, SHIFT_UP])
  })

  it('sends Return and Tab for newlines and tabs', () => {
    const { vm, rfb } = mountScreen()
    vm.typeText('a\tb\r\n')
    expect(rfb.keys).toEqual([key(0x61), key(0xff09), key(0x62), key(0xff0d)])
  })

  it('refuses text with characters it cannot type', () => {
    const { vm, rfb } = mountScreen()
    expect(vm.typeText('café')).toBe(false)
    expect(rfb.keys).toEqual([])
  })

  it('does not disconnect a session that already closed', async () => {
    const { w, rfb } = mountScreen()
    rfb.fire('disconnect', { clean: true })
    expect(w.emitted('disconnect')).toEqual([[true]])
    w.unmount()
    expect(rfb.disconnects).toBe(0)
  })

  it('disconnects a live session on unmount', () => {
    const { w, rfb } = mountScreen()
    w.unmount()
    expect(rfb.disconnects).toBe(1)
  })

  it('reports a constructor failure as a disconnect', () => {
    fake.throwOnCreate = true
    const w = mount(VncScreen, { props: { url: 'nope', password: 'pw' } })
    expect(w.emitted('disconnect')).toEqual([[false]])
  })
})

import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { defineComponent, h, ref, type Ref } from 'vue'
import { mount, type VueWrapper } from '@vue/test-utils'
import { useCommandShortcut } from './useCommandShortcut'

let open: Ref<boolean>
let wrapper: VueWrapper | null = null

function press(target: EventTarget, init: KeyboardEventInit = {}): KeyboardEvent {
  const e = new KeyboardEvent('keydown', {
    key: 'k',
    ctrlKey: true,
    bubbles: true,
    cancelable: true,
    ...init,
  })
  target.dispatchEvent(e)
  return e
}

function add(html: string): HTMLElement {
  const host = document.createElement('div')
  host.innerHTML = html
  document.body.appendChild(host)
  return host
}

beforeEach(() => {
  document.body.innerHTML = ''
  open = ref(false)
  wrapper = mount(
    defineComponent({
      setup() {
        useCommandShortcut(open)
        return () => h('div')
      },
    }),
  )
})

afterEach(() => {
  wrapper?.unmount()
  wrapper = null
})

describe('useCommandShortcut', () => {
  it('toggles on Ctrl+K and Meta+K from the page body', () => {
    const e = press(document.body)
    expect(open.value).toBe(true)
    expect(e.defaultPrevented).toBe(true)
    press(document.body, { ctrlKey: false, metaKey: true })
    expect(open.value).toBe(false)
  })

  it('ignores K without a modifier, with Shift or Alt, and key repeats', () => {
    press(document.body, { ctrlKey: false })
    press(document.body, { shiftKey: true, key: 'K' })
    press(document.body, { altKey: true })
    press(document.body, { repeat: true })
    expect(open.value).toBe(false)
  })

  it('ignores fields and editable text', () => {
    const host = add(
      '<input><textarea></textarea><select><option>a</option></select><div contenteditable="true"><b>x</b></div>',
    )
    for (const sel of ['input', 'textarea', 'select', '[contenteditable] b']) {
      const e = press(host.querySelector(sel)!)
      expect(e.defaultPrevented, sel).toBe(false)
    }
    expect(open.value).toBe(false)
  })

  it('leaves Ctrl+K inside the console to the VM', () => {
    const host = add('<div data-console><canvas tabindex="-1"></canvas></div>')
    const e = press(host.querySelector('canvas')!)
    expect(open.value).toBe(false)
    expect(e.defaultPrevented).toBe(false)
  })

  it('stops listening once the owner unmounts', () => {
    wrapper?.unmount()
    wrapper = null
    press(document.body)
    expect(open.value).toBe(false)
  })
})

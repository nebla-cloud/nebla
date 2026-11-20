import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { mountApp } from '@/test/mount'
import CopyField from './CopyField.vue'

afterEach(() => {
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

const copyButton = () =>
  Array.from(document.body.querySelectorAll('button')).find((b) => /Cop/.test(b.textContent ?? ''))!

describe('CopyField', () => {
  it('shows Copied after copy', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText } })
    const { wrapper } = mountApp(CopyField, { props: { value: 'secret-1' } })
    copyButton().click()
    await flushPromises()
    expect(writeText).toHaveBeenCalledWith('secret-1')
    expect(copyButton().textContent).toContain('Copied')
    expect(wrapper.findComponent(CopyField).emitted('copied')).toHaveLength(1)
  })

  it('falls back to selecting', async () => {
    vi.stubGlobal('navigator', { ...navigator, clipboard: undefined })
    mountApp(CopyField, { props: { value: 'secret-1' } })
    copyButton().click()
    await flushPromises()
    const input = document.body.querySelector('input') as HTMLInputElement
    expect(input.selectionStart).toBe(0)
    expect(input.selectionEnd).toBe('secret-1'.length)
    expect(document.body.textContent).toMatch(/Press (Ctrl\+C|⌘C) to copy/)
  })
})

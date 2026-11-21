import { describe, expect, it } from 'vitest'
import { defineComponent, h, nextTick, ref } from 'vue'
import { mount } from '@vue/test-utils'
import { useCrumbs, useCurrentCrumbs, type Crumb } from './crumbs'

function page(items: Crumb[] | (() => Crumb[])) {
  return defineComponent({
    setup() {
      useCrumbs(items)
      return () => h('div')
    },
  })
}

describe('crumbs', () => {
  it('is null when no page sets crumbs', () => {
    expect(useCurrentCrumbs().value).toBeNull()
  })

  it('sets the current crumbs and clears them on unmount', () => {
    const current = useCurrentCrumbs()
    const w = mount(page([{ label: 'Servers', to: '/x' }, { label: 'web-1' }]))
    expect(current.value).toEqual([{ label: 'Servers', to: '/x' }, { label: 'web-1' }])
    w.unmount()
    expect(current.value).toBeNull()
  })

  it('follows a getter', async () => {
    const name = ref('web-1')
    const current = useCurrentCrumbs()
    const w = mount(page(() => [{ label: 'Servers' }, { label: name.value }]))
    name.value = 'web-2'
    await nextTick()
    expect(current.value?.[1]?.label).toBe('web-2')
    w.unmount()
  })

  it('keeps a newer page when an older one unmounts late', () => {
    const current = useCurrentCrumbs()
    const a = mount(page([{ label: 'A' }]))
    const b = mount(page([{ label: 'B' }]))
    a.unmount()
    expect(current.value).toEqual([{ label: 'B' }])
    b.unmount()
    expect(current.value).toBeNull()
  })
})

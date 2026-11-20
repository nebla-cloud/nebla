import { describe, expect, it } from 'vitest'
import UInput from '@nuxt/ui/components/Input.vue'
import UTextarea from '@nuxt/ui/components/Textarea.vue'
import { mountApp } from '@/test/mount'

describe('field borders', () => {
  it.each([
    ['UInput', UInput, 'input'],
    ['UTextarea', UTextarea, 'textarea'],
  ])('%s uses the --ui-border ring', (_n, comp, tag) => {
    const { wrapper } = mountApp(comp, { props: { modelValue: '' } })
    const cls = wrapper.find(tag).classes()
    expect(cls).toContain('ring-(color:--ui-border)')
    expect(cls).not.toContain('ring-accented')
    wrapper.unmount()
  })
})

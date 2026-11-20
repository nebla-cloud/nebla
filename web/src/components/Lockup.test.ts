import { afterEach, describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { resetSession, useSession } from '@/lib/session'
import type { UiManifest } from '@/api/types'
import Lockup from './Lockup.vue'

function brand(name: string) {
  useSession().manifest.value = {
    branding: { product_name: name, logo_url: '', primary_color: '' },
  } as UiManifest
}

describe('Lockup', () => {
  afterEach(() => resetSession())

  it('renders the Nebla lockup as a labelled image, not text', () => {
    brand('Nebla')
    const w = mount(Lockup)
    expect(w.find('[aria-label="Nebla"]').exists()).toBe(true)
    expect(w.find('svg').exists()).toBe(true)
    expect(w.text()).not.toContain('Nebla')
  })

  it('gives each instance its own gradient id', () => {
    brand('Nebla')
    const w = mount({ components: { Lockup }, template: '<div><Lockup /><Lockup /></div>' })
    const [a, b] = w.findAll('linearGradient').map((g) => g.attributes('id'))
    expect(a).toBeTruthy()
    expect(a).not.toBe(b)
  })

  it('renders the mark and name for a custom product name', () => {
    brand('Acme Cloud')
    const w = mount(Lockup)
    expect(w.text()).toContain('Acme Cloud')
    expect(w.find('[aria-label="Nebla"]').exists()).toBe(false)
  })
})

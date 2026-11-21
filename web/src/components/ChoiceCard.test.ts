import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import { defineComponent, ref } from 'vue'
import ChoiceCard from './ChoiceCard.vue'

const Host = defineComponent({
  components: { ChoiceCard },
  props: { variant: { type: String, default: 'default' } },
  setup() {
    return { picked: ref('small-2') }
  },
  template: `
    <div>
      <ChoiceCard v-model="picked" type="radio" name="t" value="small-2" :variant="variant">small-2<template #meta>2 vCPU</template></ChoiceCard>
      <ChoiceCard v-model="picked" type="radio" name="t" value="medium-4" :variant="variant">medium-4</ChoiceCard>
      <ChoiceCard v-model="picked" type="radio" name="t" value="large-8" disabled>large-8<template #meta>Over quota</template></ChoiceCard>
      <span data-picked>{{ picked }}</span>
    </div>`,
})

describe('ChoiceCard', () => {
  it('puts a real input inside the label, sharing a name across the group', () => {
    const w = mount(Host)
    const radios = w.findAll('label > input[type="radio"]')
    expect(radios).toHaveLength(3)
    expect(new Set(radios.map((r) => r.attributes('name')))).toEqual(new Set(['t']))
    expect((radios[0]!.element as HTMLInputElement).checked).toBe(true)
  })

  it('stretches the invisible input over the card so clicks land on it', () => {
    const cls = mount(Host).get('input').classes()
    expect(cls).toEqual(expect.arrayContaining(['absolute', 'inset-0', 'opacity-0']))
    expect(cls).not.toContain('sr-only')
  })

  it('updates v-model on change', async () => {
    const w = mount(Host)
    await w.findAll('input')[1]!.setValue(true)
    expect(w.get('[data-picked]').text()).toBe('medium-4')
  })

  it("names each radio by its card's title", () => {
    const w = mount(Host)
    const labels = w.findAll('label').map((l) => l.text())
    expect(labels[1]).toContain('medium-4')
    expect(labels[0]).toContain('small-2')
    expect(labels[0]).toContain('2 vCPU')
  })

  it('disables the input and dashes the border', () => {
    const w = mount(Host)
    const label = w.findAll('label')[2]!
    expect(label.get('input').attributes('disabled')).toBeDefined()
    expect(label.classes()).toContain('border-dashed')
  })

  it('marks the arch variant', () => {
    const w = mount(Host, { props: { variant: 'arch' } })
    expect(w.get('label').attributes('data-variant')).toBe('arch')
  })

  it('supports checkboxes', async () => {
    const w = mount(ChoiceCard, {
      props: { type: 'checkbox', name: 'k', value: 'a', modelValue: [] as string[] },
      slots: { default: 'key a' },
    })
    await w.get('input').setValue(true)
    expect(w.emitted('update:modelValue')![0]).toEqual([['a']])
  })
})

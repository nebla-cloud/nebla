import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import PageHeader from './PageHeader.vue'

describe('PageHeader', () => {
  it('renders the h1, description, meta and actions', () => {
    const w = mount(PageHeader, {
      props: { title: 'Servers', description: 'All of them' },
      slots: { meta: '<span>beta</span>', actions: '<button>New</button>' },
    })
    expect(w.find('header h1').text()).toBe('Servers')
    expect(w.find('h1').classes()).toContain('truncate')
    expect(w.text()).toContain('All of them')
    expect(w.text()).toContain('beta')
    expect(w.find('button').text()).toBe('New')
  })
})

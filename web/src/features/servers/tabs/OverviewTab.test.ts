import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { flushPromises, type VueWrapper } from '@vue/test-utils'
import { RouterView } from 'vue-router'
import { mountApp, testRouter } from '@/test/mount'
import type { Server } from '@/api/types'
import { makeServer } from '../fixtures'
import OverviewTab from './OverviewTab.vue'

let mounted: VueWrapper | undefined

async function mountTab(s: Server) {
  const router = testRouter([{ path: '/p/:projectId', component: RouterView }])
  await router.push('/p/1')
  const res = mountApp(OverviewTab, { router, props: { server: s } })
  mounted = res.wrapper
  await flushPromises()
  return res.wrapper
}

const panels = (w: VueWrapper) =>
  w.findAll('section').filter((s) => s.classes().includes('rounded-(--nb-radius-panel)'))

beforeEach(() => {
  document.body.innerHTML = ''
})
afterEach(() => {
  mounted?.unmount()
  mounted = undefined
})

describe('OverviewTab', () => {
  it('puts the facts in a panel with a description list', async () => {
    const w = await mountTab(
      makeServer({
        public_net: { ipv4: { ip: '203.0.113.10' }, ipv6: { ip: '2001:db8::10' } },
        labels: { env: 'prod' },
      }),
    )
    const facts = panels(w).find((p) => p.find('dl').exists())
    expect(facts).toBeTruthy()
    expect(facts!.findAll('dt').map((d) => d.text())).toEqual([
      'Name',
      'IPv4',
      'IPv6',
      'Image',
      'Type',
      'Location',
      'Created',
      'Labels',
    ])
    for (const ip of ['203.0.113.10', '2001:db8::10']) {
      const dd = facts!.findAll('dd').find((d) => d.text().includes(ip))!
      expect(dd.find('.font-mono').text()).toBe(ip)
    }
    expect(facts!.find('[aria-label="Copy IPv4"]').exists()).toBe(true)
    expect(facts!.find('[aria-label="Copy IPv6"]').exists()).toBe(true)
    expect(facts!.text()).toContain('Debian 13')
    expect(facts!.text()).toContain('small-2: 2 vCPU, 4 GB, 40 GB')
    expect(facts!.text()).toContain('env=prod')
    const buttons = facts!.findAll('button').map((b) => b.text())
    expect(buttons).toContain('Rename')
    expect(buttons).toContain('Edit labels')
  })

  it('keeps Delete in its own panel', async () => {
    const w = await mountTab(makeServer())
    const danger = panels(w).find((p) => !p.find('dl').exists())
    expect(danger).toBeTruthy()
    expect(danger!.text()).toContain('Delete this server and free its IP addresses.')
    expect(danger!.findAll('button').map((b) => b.text())).toEqual(['Delete server'])
  })

  it('opens the label editor in place of the chips', async () => {
    const w = await mountTab(makeServer({ labels: { env: 'prod' } }))
    const edit = w.findAll('button').find((b) => b.text() === 'Edit labels')!
    await edit.trigger('click')
    await flushPromises()
    expect(w.findAll('button').some((b) => b.text() === 'Save labels')).toBe(true)
    expect(w.findAll('button').some((b) => b.text() === 'Edit labels')).toBe(false)
  })
})

import { afterEach, describe, expect, it } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { mountApp, testRouter } from '@/test/mount'
import { makeServer } from '@/features/servers/fixtures'
import { RouterView } from 'vue-router'
import FirewallsTab from './FirewallsTab.vue'
import routes from './routes'

async function router() {
  const r = testRouter([
    {
      path: '/p/:projectId',
      component: RouterView,
      children: [...routes, { path: 'servers/:serverId/firewalls', component: FirewallsTab }],
    },
  ])
  await r.push('/p/7/servers/1/firewalls')
  return r
}

afterEach(() => {
  document.body.innerHTML = ''
})

describe('FirewallsTab', () => {
  it('shows each firewall and why it applies', async () => {
    const s = makeServer({
      firewalls: [
        { id: 3, name: 'web', via: 'role=web' },
        { id: 4, name: 'base', via: 'direct' },
      ],
    })
    const { wrapper } = mountApp(FirewallsTab, { router: await router(), props: { server: s } })
    await flushPromises()
    expect(wrapper.text()).toContain('via role=web')
    expect(wrapper.text()).toContain('Applied directly')
    expect(document.body.querySelector('a[href="/p/7/firewalls/3"]')).not.toBeNull()
    wrapper.unmount()
  })

  it('says when no firewall applies', async () => {
    const { wrapper } = mountApp(FirewallsTab, { router: await router(), props: { server: makeServer() } })
    await flushPromises()
    expect(wrapper.text()).toContain('No firewalls')
    wrapper.unmount()
  })
})

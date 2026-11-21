import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { defineComponent, h } from 'vue'
import { flushPromises } from '@vue/test-utils'
import { RouterView, type RouteRecordRaw } from 'vue-router'
import { mountApp, testQueryClient, testRouter } from '@/test/mount'
import { resetSession, setMe } from '@/lib/session'
import type { Project } from '@/api/types'
import { useCrumbs, type Crumb } from '@/lib/crumbs'
import ProjectLayout from './ProjectLayout.vue'

const longName = 'Long project '.padEnd(120, 'x')
const project = (name: string) =>
  ({ id: 1, name, role: 'owner', usage: { servers: 0 } }) as unknown as Project

const Stub = defineComponent({ render: () => h('div') })
let crumbs: Crumb[] | null = null
const CrumbPage = defineComponent({
  setup() {
    if (crumbs) useCrumbs(crumbs)
    return () => h('div')
  },
})
const Host = defineComponent({ render: () => h(RouterView) })

const routes: RouteRecordRaw[] = [
  {
    path: '/p/:projectId',
    component: ProjectLayout,
    meta: { side: 'project' },
    children: [
      {
        path: 'servers',
        name: 'servers',
        component: Stub,
        meta: { side: 'project', nav: { label: 'Servers', order: 10 } },
      },
      { path: 'servers/web-1', name: 'server', component: CrumbPage },
      {
        path: 'keys',
        name: 'keys',
        component: Stub,
        meta: { side: 'project', nav: { label: 'SSH keys', order: 20, group: 'Access' } },
      },
    ],
  },
]

let unmount: (() => void) | null = null

async function mountAt(path: string, name = 'Home lab') {
  setMe({ id: 1, email: 'kai@example.com', name: 'Kai', is_admin: false, totp_enabled: false })
  const qc = testQueryClient()
  qc.setQueryData(['project', 1], project(name))
  qc.setQueryData(['projects'], [project(name)])
  const router = testRouter(routes)
  await router.push(path)
  const { wrapper } = mountApp(Host, { router, queryClient: qc })
  unmount = () => wrapper.unmount()
  await flushPromises()
}

const topBar = () => document.querySelector('[data-topbar]') as HTMLElement

beforeEach(() => {
  resetSession()
  crumbs = null
  document.body.innerHTML = ''
})

afterEach(() => {
  unmount?.()
  unmount = null
})

describe('ShellFrame', () => {
  it('shows the active nav label as the title when there are no crumbs', async () => {
    await mountAt('/p/1/keys')
    expect(document.querySelector('nav[aria-label="Breadcrumb"]')).toBeNull()
    expect(topBar().querySelector('p.font-semibold')?.textContent?.trim()).toBe('SSH keys')
    expect(topBar().querySelector('h1')).toBeNull()
  })

  it('shows the crumbs a page sets, with the last one as the current page', async () => {
    crumbs = [{ label: 'Servers', to: '/x' }, { label: 'web-1' }]
    await mountAt('/p/1/servers/web-1')
    const navs = document.querySelectorAll('nav[aria-label="Breadcrumb"]')
    expect(navs).toHaveLength(1)
    const links = navs[0]!.querySelectorAll('a')
    expect(links).toHaveLength(1)
    expect(links[0]!.textContent?.trim()).toBe('Servers')
    const items = navs[0]!.querySelectorAll('li')
    const last = items[items.length - 1]!.querySelector('[aria-current="page"]')
    expect(last?.textContent?.trim()).toBe('web-1')
  })

  it('hides the nav title when crumbs are set, so Servers shows once', async () => {
    crumbs = [{ label: 'Servers', to: '/x' }, { label: 'web-1' }]
    await mountAt('/p/1/servers/web-1')
    expect(topBar().querySelector('p.font-semibold')).toBeNull()
    const visible = [...topBar().querySelectorAll('nav, p')].map((e) => e.textContent ?? '')
    expect(visible.join(' ').match(/Servers/g)).toHaveLength(1)
  })

  it('truncates a long project name in the switcher at 390px', async () => {
    window.innerWidth = 390
    window.matchMedia = ((q: string) => ({
      matches: !/min-width/.test(q),
      media: q,
      onchange: null,
      addEventListener: () => {},
      removeEventListener: () => {},
      addListener: () => {},
      removeListener: () => {},
      dispatchEvent: () => false,
    })) as unknown as typeof window.matchMedia
    await mountAt('/p/1/servers', longName)
    ;(document.querySelector('button[aria-label="Open menu"]') as HTMLElement).click()
    await flushPromises()
    const switchers = document.querySelectorAll('button[aria-label="Switch project"]')
    expect(switchers.length).toBe(2)
    for (const s of switchers) {
      const label = [...s.querySelectorAll('span')].find(
        (el) => el.textContent?.trim() === longName,
      )
      expect(label?.classList.contains('truncate')).toBe(true)
      expect(s.textContent).toContain('Owner')
    }
  })
})

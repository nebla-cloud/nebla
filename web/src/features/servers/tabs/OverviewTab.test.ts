// Copyright (C) 2026 The Nebla contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, type VueWrapper } from '@vue/test-utils'
import { RouterView } from 'vue-router'
import { http } from 'msw'
import { json, server as msw } from '@/test/server'
import { mountApp, testRouter } from '@/test/mount'
import type { Action, Server } from '@/api/types'
import { makeServer } from '../fixtures'
import OverviewTab from './OverviewTab.vue'

let mounted: VueWrapper | undefined

const metricsFixture = {
  metrics: {
    start: '2026-11-20T10:00:00Z',
    end: '2026-11-20T11:00:00Z',
    step: 60,
    time_series: {
      cpu: [
        [1, 12],
        [2, 18.4],
      ],
      'network.in': [
        [1, 1024],
        [2, 2.1 * 1024 * 1024],
      ],
      'network.out': [
        [1, 1000],
        [2, 0.4 * 1024 * 1024],
      ],
      'disk.read': [
        [1, 100],
        [2, 512],
      ],
      'disk.write': [
        [1, 50],
        [2, 3 * 1024],
      ],
    },
  },
}

function act(id: number, over: Partial<Action> = {}): Action {
  return {
    id,
    command: 'start_server',
    status: 'success',
    progress: 100,
    resources: [{ id: 1, type: 'server' }],
    error: null,
    started: new Date(Date.now() - 120_000).toISOString(),
    finished: new Date(Date.now() - 110_000).toISOString(),
    ...over,
  }
}

const actions = [
  act(5, { command: 'reboot_server', status: 'running', progress: 40, finished: null }),
  act(4, { command: 'change_server_type' }),
  act(3, { command: 'rebuild_server', status: 'error' }),
  act(2, { command: 'start_server' }),
  act(1, { command: 'create_server' }),
]

beforeEach(() => {
  msw.use(
    http.get('*/v1/projects/1', () =>
      json(200, { id: 1, name: 'Home lab', description: '', created: '2026-11-01T00:00:00Z' }),
    ),
    http.get('*/v1/servers/:id/metrics', () => json(200, metricsFixture)),
    http.get('*/v1/servers/:id/actions', () =>
      json(200, {
        actions,
        meta: { pagination: { page: 1, per_page: 25, total_entries: actions.length } },
      }),
    ),
  )
})

async function mountTab(s: Server) {
  const router = testRouter([
    { path: '/p/:projectId', component: RouterView },
    {
      path: '/p/:projectId/servers/:serverId/activity',
      name: 'server-activity',
      component: RouterView,
    },
  ])
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
        public_net: { ipv4: { ip: '203.0.113.10' }, ipv6: { ip: '2001:db8::10' }, floating_ips: [] },
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

  it('shows the OS name with the image slug as a second, muted value', async () => {
    const w = await mountTab(makeServer({ image: { ...makeServer().image, name: 'debian-13' } }))
    const dds = w.findAll('dd').map((d) => d.text())
    expect(dds).toContain('Debian 13')
    expect(dds).toContain('debian-13')
  })

  it('puts Delete in the recent activity panel footer', async () => {
    const w = await mountTab(makeServer())
    const recent = panels(w).find(
      (p) => p.find('h2').exists() && p.find('h2').text() === 'Recent activity',
    )
    expect(recent).toBeTruthy()
    const footer = recent!.find('footer')
    expect(footer.text()).toContain('Delete this server and free its IP addresses.')
    expect(footer.findAll('button').map((b) => b.text())).toEqual(['Delete server'])
    expect(w.findAll('button').filter((b) => b.text() === 'Delete server')).toHaveLength(1)
  })

  it('lists the last 3 actions with a link to the activity tab', async () => {
    const w = await mountTab(makeServer({ id: 7 }))
    const recent = panels(w).find(
      (p) => p.find('h2').exists() && p.find('h2').text() === 'Recent activity',
    )!
    await vi.waitFor(() => expect(recent.findAll('li')).toHaveLength(3))
    const items = recent.findAll('li').map((li) => li.text())
    expect(items[0]).toContain('Reboot')
    expect(items[0]).toContain('Running 40%')
    expect(items[1]).toContain('Resize')
    expect(items[1]).toContain('Done')
    expect(items[1]).toContain('2 min ago')
    expect(items[2]).toContain('Rebuild')
    expect(items[2]).toContain('Failed')
    const all = recent.findAll('a').find((a) => a.text() === 'View all')!
    expect(all.attributes('href')).toBe('/p/1/servers/7/activity')
  })

  it('shows live metric cards for a running server', async () => {
    const w = await mountTab(makeServer())
    await vi.waitFor(() => expect(w.find('[data-metric="cpu"] [data-value]').text()).toBe('18%'))
    // The second figure sits in the card's unit slot.
    const reading = (k: string) => {
      const v = w.find(`[data-metric="${k}"] [data-value]`)
      return `${v.text()} ${v.element.nextElementSibling!.textContent!.trim()}`
    }
    expect(reading('network')).toBe('2.1 MB/s in / 410 KB/s out')
    expect(reading('disk')).toBe('512 B/s read / 3.0 KB/s write')
    const lines = (k: string) =>
      w.findAll(`[data-metric="${k}"] path[data-line]`).map((p) => p.attributes('stroke'))
    expect(lines('cpu')).toEqual(['var(--nb-busy)'])
    expect(lines('network')).toEqual(['var(--nb-run)', 'var(--nb-err)'])
    expect(lines('disk')).toEqual(['var(--nb-busy)', 'var(--nb-err)'])
    expect(w.find('[data-metric] [stroke-dasharray]').exists()).toBe(false)
    expect(w.find('[data-metric="cpu"] svg').attributes('aria-hidden')).toBe('true')
    expect(w.text()).not.toContain("Graphs appear once it's running")
  })

  it('shows placeholders while the metrics load', async () => {
    msw.use(http.get('*/v1/servers/:id/metrics', () => new Promise(() => {})))
    const w = await mountTab(makeServer())
    const values = w.findAll('[data-metric] [data-value]').map((v) => v.text())
    expect(values).toEqual(['—', '—', '—'])
  })

  it('an off server shows a note and makes no metrics request', async () => {
    const handler = vi.fn(() => json(200, metricsFixture))
    msw.use(http.get('*/v1/servers/9/metrics', handler))
    const w = await mountTab(makeServer({ id: 9, status: 'off' }))
    await new Promise((r) => setTimeout(r, 20))
    await flushPromises()
    expect(handler).not.toHaveBeenCalled()
    expect(w.find('[data-metric]').exists()).toBe(false)
    expect(w.text()).toContain("Graphs appear once it's running")
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

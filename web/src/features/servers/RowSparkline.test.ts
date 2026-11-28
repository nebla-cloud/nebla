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
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { defineComponent, h, ref, toValue, type MaybeRef } from 'vue'
import { VueQueryPlugin } from '@tanstack/vue-query'
import { http } from 'msw'
import { json, server } from '@/test/server'
import { testQueryClient, testRouter } from '@/test/mount'
import Sparkline from '@/components/Sparkline.vue'
import RowSparkline from './RowSparkline.vue'

const visible = ref(true)
vi.mock('@vueuse/core', async (orig) => ({
  ...(await orig<typeof import('@vueuse/core')>()),
  useElementVisibility: () => visible,
}))

const body = {
  metrics: {
    start: '2026-11-20T10:00:00Z',
    end: '2026-11-20T11:00:00Z',
    step: 60,
    time_series: {
      cpu: [
        [1, 12],
        [2, 18],
        [3, 9],
      ],
      'memory.used': [
        [1, 10],
        [2, 40],
        [3, 50],
      ],
      'memory.total': [
        [1, 100],
        [2, 100],
        [3, 100],
      ],
    },
  },
}

let urls: URL[] = []
let gate: Promise<void> | null = null
function serve(status = 200, payload: unknown = body) {
  urls = []
  server.use(
    http.get('*/v1/projects/1', () =>
      json(200, { id: 1, name: 'Home lab', description: '', created: '2026-11-01T00:00:00Z' }),
    ),
    http.get('*/v1/servers/:id/metrics', async ({ request }) => {
      urls.push(new URL(request.url))
      if (gate) await gate
      return status === 200
        ? json(200, payload)
        : json(status, { error: { code: 'unavailable', message: 'nope' } })
    }),
  )
}

async function mountRows(
  rows: MaybeRef<{ serverId: number; running: boolean; metric?: 'cpu' | 'memory' }[]>,
) {
  const router = testRouter([{ path: '/p/:projectId', component: { render: () => null } }])
  await router.push('/p/1')
  const C = defineComponent({
    render: () =>
      h(
        'div',
        toValue(rows).map((r) => h(RowSparkline, { key: r.serverId, metric: 'cpu', ...r })),
      ),
  })
  return mount(C, {
    global: { plugins: [router, [VueQueryPlugin, { queryClient: testQueryClient() }]] },
  })
}

const idle = () => new Promise((r) => setTimeout(r, 30))

enableAutoUnmount(afterEach)

beforeEach(() => {
  visible.value = true
  gate = null
})

describe('RowSparkline', () => {
  it('a visible running row fetches cpu for the last hour and draws a line', async () => {
    serve()
    const w = await mountRows([{ serverId: 5, running: true }])
    await vi.waitFor(() => expect(w.find('path[data-line]').exists()).toBe(true))
    expect(urls).toHaveLength(1)
    expect(urls[0]!.pathname).toMatch(/\/servers\/5\/metrics$/)
    expect(urls[0]!.searchParams.getAll('type').flatMap((t) => t.split(','))).toEqual([
      'cpu',
      'memory',
    ])
    expect(urls[0]!.searchParams.get('timeframe')).toBe('hour')
    const svg = w.find('svg')
    expect(svg.attributes('viewBox')).toBe('0 0 80 22')
    expect(w.text()).toBe('9%')
    expect(w.find('[aria-hidden="true"]').exists()).toBe(true)
  })

  it('cpu is drawn on a fixed 0–100 scale', async () => {
    serve()
    const w = await mountRows([{ serverId: 5, running: true }])
    await vi.waitFor(() => expect(w.find('path[data-line]').exists()).toBe(true))
    expect(w.findComponent(Sparkline).props('max')).toBe(100)
  })

  it('memory draws a run-tone line on a 0–100 scale and shows used over total', async () => {
    serve()
    const w = await mountRows([{ serverId: 5, running: true, metric: 'memory' }])
    await vi.waitFor(() => expect(w.find('path[data-line]').exists()).toBe(true))
    expect(w.findComponent(Sparkline).props('max')).toBe(100)
    expect(w.findComponent(Sparkline).props('lines')[0]!.tone).toBe('run')
    expect(w.find('path[data-line]').attributes('stroke')).toBe('var(--nb-run)')
    expect(w.text()).toBe('50%')
  })

  it('a cpu and a memory sparkline for one server send one request', async () => {
    serve()
    const w = await mountRows([
      { serverId: 5, running: true, metric: 'cpu' },
      { serverId: 5, running: true, metric: 'memory' },
    ])
    await vi.waitFor(() => expect(w.findAll('path[data-line]')).toHaveLength(2))
    await idle()
    expect(urls).toHaveLength(1)
  })

  it('a response without memory keys shows a dash for memory', async () => {
    serve(200, { metrics: { ...body.metrics, time_series: { cpu: body.metrics.time_series.cpu } } })
    const w = await mountRows([{ serverId: 5, running: true, metric: 'memory' }])
    await vi.waitFor(() => expect(urls).toHaveLength(1))
    await flushPromises()
    await idle()
    expect(w.text()).toBe('—')
    expect(w.find('svg').exists()).toBe(false)
  })

  it('an off server makes no request and shows a dash', async () => {
    serve()
    const w = await mountRows([{ serverId: 5, running: false }])
    await flushPromises()
    await idle()
    expect(urls).toHaveLength(0)
    expect(w.text()).toBe('—')
  })

  it('a hidden row makes no request', async () => {
    serve()
    visible.value = false
    const w = await mountRows([{ serverId: 5, running: true }])
    await flushPromises()
    await idle()
    expect(urls).toHaveLength(0)
    expect(w.text()).toBe('—')
  })

  it('an error shows a dash and no banner', async () => {
    serve(503)
    const w = await mountRows([{ serverId: 5, running: true }])
    await vi.waitFor(() => expect(urls).toHaveLength(1))
    await flushPromises()
    await idle()
    expect(w.text()).toBe('—')
    expect(w.find('svg').exists()).toBe(false)
  })

  it('twenty-five visible running rows send at most 20 requests at once', async () => {
    let open!: () => void
    gate = new Promise<void>((r) => (open = r))
    serve()
    const rows = Array.from({ length: 25 }, (_, i) => ({ serverId: i + 1, running: true }))
    const w = await mountRows(rows)
    await vi.waitFor(() => expect(urls).toHaveLength(20))
    await idle()
    expect(urls).toHaveLength(20)
    // Rows waiting for a slot show a dash.
    expect(w.text()).toBe('—'.repeat(25))
    open()
    await vi.waitFor(() => expect(urls).toHaveLength(25))
    await vi.waitFor(() => expect(w.findAll('path[data-line]')).toHaveLength(25))
  })

  it('drops queued requests for rows that are removed or stop running', async () => {
    let release!: () => void
    gate = new Promise<void>((resolve) => (release = resolve))
    serve()
    const rows = ref(Array.from({ length: 23 }, (_, i) => ({ serverId: i + 1, running: true })))
    const w = await mountRows(rows)
    try {
      await vi.waitFor(() => expect(urls).toHaveLength(20))
      rows.value.splice(20, 1)
      rows.value.find((row) => row.serverId === 22)!.running = false
      await flushPromises()
    } finally {
      release()
    }
    await vi.waitFor(() => expect(w.findAll('path[data-line]')).toHaveLength(21))
    expect(urls).toHaveLength(21)
    expect(urls.at(-1)!.pathname).toMatch(/\/servers\/23\/metrics$/)
  })

  it('refreshes every 2 minutes, only on the timer', async () => {
    serve()
    const qc = testQueryClient()
    const router = testRouter([{ path: '/p/:projectId', component: { render: () => null } }])
    await router.push('/p/1')
    mount(RowSparkline, {
      props: { serverId: 5, running: true, metric: 'cpu' },
      global: { plugins: [router, [VueQueryPlugin, { queryClient: qc }]] },
    })
    await vi.waitFor(() => expect(urls).toHaveLength(1))
    const q = qc.getQueryCache().find({ queryKey: ['server-metrics', 1, 5, 'hour', 'row'] })
    const opts = q!.observers[0]!.options
    expect(opts.refetchInterval).toBe(120_000)
    expect(opts.staleTime).toBe(110_000)
    expect(opts.refetchOnWindowFocus).toBe(false)
    expect(opts.refetchIntervalInBackground).toBe(false)
  })
})

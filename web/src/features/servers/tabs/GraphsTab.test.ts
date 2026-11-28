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
import { flushPromises } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import { http } from 'msw'
import { json, server } from '@/test/server'
import { mountApp, testRouter } from '@/test/mount'
import type { Server } from '@/api/types'
import { makeServer } from '../fixtures'
import { chartHeader } from '../metrics/metrics'
import GraphsTab from './GraphsTab.vue'

vi.mock('../metrics/MetricChart.vue', () => ({
  default: defineComponent({
    props: ['title', 'rows', 'series', 'unit'],
    setup: (p) => () =>
      h(
        'div',
        {
          class: 'chart',
          'data-tones': p.series.map((s: { tone: string }) => s.tone).join(','),
          'data-header': chartHeader(
            p.rows,
            p.series.map((s: { label: string }) => s.label),
            p.unit,
            null,
          ).value,
        },
        `${p.title}: ${p.rows.length} points`,
      ),
  }),
}))

const sample = {
  metrics: {
    start: '2026-11-20T10:00:00Z',
    end: '2026-11-20T11:00:00Z',
    step: 60,
    time_series: {
      cpu: [
        [1, 5],
        [2, 6],
      ],
      'disk.read': [[1, 1]],
      'disk.write': [[1, 2]],
      'network.in': [[1, 3]],
      'network.out': [[1, 4]],
      'memory.used': [[1, 536870912]],
      'memory.total': [[1, 1073741824]],
    },
  },
}

let urls: URL[] = []
function serveMetrics(body: unknown = sample, status = 200) {
  urls = []
  server.use(
    http.get('*/v1/projects/1', () =>
      json(200, { id: 1, name: 'Home lab', description: '', created: '2026-11-01T00:00:00Z' }),
    ),
    http.get('*/v1/servers/5/metrics', ({ request }) => {
      urls.push(new URL(request.url))
      return json(status, body)
    }),
  )
}

async function mountTab(range?: string, s: Server = makeServer({ id: 5 })) {
  const router = testRouter([{ path: '/p/:projectId/servers/:id/graphs', component: GraphsTab }])
  await router.push(`/p/1/servers/5/graphs${range ? `?range=${range}` : ''}`)
  const res = mountApp(GraphsTab, { router, props: { server: s } })
  await flushPromises()
  await vi.waitFor(() => expect(urls.length).toBeGreaterThan(0))
  await flushPromises()
  return { ...res, router }
}

const types = (u: URL) => u.searchParams.getAll('type').flatMap((t) => t.split(','))

beforeEach(() => {
  document.body.innerHTML = ''
})
afterEach(() => {
  vi.useRealTimers()
  document.body.innerHTML = ''
})

describe('GraphsTab', () => {
  it('requests all types for the range', async () => {
    serveMetrics()
    await mountTab()
    expect(types(urls[0]!).sort()).toEqual(['cpu', 'disk', 'memory', 'network'])
    expect(urls[0]!.searchParams.get('timeframe')).toBe('hour')
  })

  it('range comes from the query', async () => {
    serveMetrics()
    const { wrapper, router } = await mountTab('day')
    expect(urls[0]!.searchParams.get('timeframe')).toBe('day')
    const btn = (t: string) => wrapper.findAll('button').find((b) => b.text() === t)!
    expect(btn('Day').attributes('aria-pressed')).toBe('true')
    await btn('Week').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.query.range).toBe('week')
    await vi.waitFor(() => expect(urls.at(-1)!.searchParams.get('timeframe')).toBe('week'))
  })

  it('four charts render', async () => {
    serveMetrics()
    const { wrapper } = await mountTab()
    const text = wrapper.findAll('.chart').map((c) => c.text())
    expect(text).toEqual([
      'CPU: 2 points',
      'Memory: 1 points',
      'Disk: 1 points',
      'Network: 1 points',
    ])
  })

  it('cpu is busy, in and read pair with out and write', async () => {
    serveMetrics()
    const { wrapper } = await mountTab()
    const tones = wrapper.findAll('.chart').map((c) => c.attributes('data-tones'))
    expect(tones).toEqual(['busy', 'run', 'busy,err', 'run,err'])
  })

  it('memory chart reads as a percent', async () => {
    serveMetrics()
    const { wrapper } = await mountTab()
    const mem = wrapper.findAll('.chart')[1]!
    expect(mem.attributes('data-tones')).toBe('run')
    expect(mem.attributes('data-header')).toBe('50%')
  })

  it('renders the Memory chart when memory keys are missing', async () => {
    const rest: Record<string, unknown> = { ...sample.metrics.time_series }
    delete rest['memory.used']
    delete rest['memory.total']
    serveMetrics({ metrics: { ...sample.metrics, time_series: rest } })
    const { wrapper } = await mountTab()
    expect(wrapper.findAll('.chart').map((c) => c.text())[1]).toBe('Memory: 0 points')
  })

  it('all-null shows empty', async () => {
    serveMetrics({
      metrics: {
        ...sample.metrics,
        time_series: { cpu: [[1, null]], 'disk.read': [[1, null]], 'network.in': [[1, null]] },
      },
    })
    const { wrapper } = await mountTab()
    expect(wrapper.text()).toContain('No data for this range')
    expect(wrapper.findAll('.chart')).toHaveLength(0)
  })

  it('hour refreshes every 60 s', async () => {
    serveMetrics()
    vi.useFakeTimers({ toFake: ['setInterval', 'setTimeout', 'clearInterval', 'clearTimeout'] })
    const router = testRouter([{ path: '/p/:projectId/servers/:id/graphs', component: GraphsTab }])
    await router.push('/p/1/servers/5/graphs')
    mountApp(GraphsTab, { router, props: { server: makeServer({ id: 5 }) } })
    await vi.waitFor(() => expect(urls.length).toBe(1))
    await vi.advanceTimersByTimeAsync(60_000)
    await vi.waitFor(() => expect(urls.length).toBe(2))
  })

  it('day does not refetch', async () => {
    serveMetrics()
    vi.useFakeTimers({ toFake: ['setInterval', 'setTimeout', 'clearInterval', 'clearTimeout'] })
    const router = testRouter([{ path: '/p/:projectId/servers/:id/graphs', component: GraphsTab }])
    await router.push('/p/1/servers/5/graphs?range=day')
    mountApp(GraphsTab, { router, props: { server: makeServer({ id: 5 }) } })
    await vi.waitFor(() => expect(urls.length).toBe(1))
    await vi.advanceTimersByTimeAsync(120_000)
    expect(urls.length).toBe(1)
  })

  it('off server shows the note', async () => {
    serveMetrics()
    const { wrapper } = await mountTab(undefined, makeServer({ id: 5, status: 'off' }))
    expect(wrapper.text()).toContain('The server is off. Graphs resume when it starts.')
  })

  it('409 shows no data yet', async () => {
    serveMetrics(
      { error: { code: 'conflict', message: 'Metrics start after the first boot.' } },
      409,
    )
    const { wrapper } = await mountTab()
    expect(wrapper.text()).toContain('No graph data yet')
    expect(wrapper.text()).toContain('Metrics start after the first boot.')
  })

  it('404 shows no data yet', async () => {
    serveMetrics({ error: { code: 'not_found', message: 'No metrics for this server yet.' } }, 404)
    const { wrapper } = await mountTab()
    expect(wrapper.text()).toContain('No graph data yet')
    expect(wrapper.text()).toContain('No metrics for this server yet.')
    expect(wrapper.text()).not.toContain("Couldn't load graphs")
  })
})

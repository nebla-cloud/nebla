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

import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent, h, ref, type Ref } from 'vue'
import { VueQueryPlugin } from '@tanstack/vue-query'
import { http } from 'msw'
import { json, server } from '@/test/server'
import { testQueryClient, testRouter } from '@/test/mount'
import { useLiveMetrics } from './useLiveMetrics'

const fixture = {
  metrics: {
    start: '2026-11-20T10:00:00Z',
    end: '2026-11-20T11:00:00Z',
    step: 60,
    time_series: {
      cpu: [
        [1, 12],
        [2, 18.2],
        [3, null],
      ],
      'network.in': [
        [1, 100],
        [2, 2048],
      ],
      'network.out': [[1, 512]],
      'disk.read': [[1, null]],
      'disk.write': [
        [1, 7],
        [2, 9],
      ],
      'memory.used': [
        [1, 512],
        [2, 1024],
        [3, null],
      ],
      'memory.total': [
        [1, 2048],
        [2, 2048],
        [3, 2048],
      ],
    },
  },
}

let urls: URL[] = []
function serve() {
  urls = []
  server.use(
    http.get('*/v1/projects/1', () =>
      json(200, { id: 1, name: 'Home lab', description: '', created: '2026-11-01T00:00:00Z' }),
    ),
    http.get('*/v1/servers/5/metrics', ({ request }) => {
      urls.push(new URL(request.url))
      return json(200, fixture)
    }),
  )
}

async function setup(enabled: Ref<boolean>, queryClient = testQueryClient()) {
  const router = testRouter([{ path: '/p/:projectId', component: { render: () => null } }])
  await router.push('/p/1')
  let result!: ReturnType<typeof useLiveMetrics>
  const C = defineComponent({
    setup() {
      result = useLiveMetrics(ref(5), enabled)
      return () => h('div')
    },
  })
  const wrapper = mount(C, {
    global: { plugins: [router, [VueQueryPlugin, { queryClient }]] },
  })
  return { result, wrapper }
}

afterEach(() => vi.useRealTimers())

describe('useLiveMetrics', () => {
  it('requests cpu, network, disk and memory for the last hour', async () => {
    serve()
    await setup(ref(true))
    await vi.waitFor(() => expect(urls).toHaveLength(1))
    const types = urls[0]!.searchParams.getAll('type').flatMap((t) => t.split(','))
    expect(types.sort()).toEqual(['cpu', 'disk', 'memory', 'network'])
    expect(urls[0]!.searchParams.get('timeframe')).toBe('hour')
  })

  it('latest values are the last non-null samples', async () => {
    serve()
    const { result } = await setup(ref(true))
    await vi.waitFor(() => expect(result.cpu.value).not.toBeNull())
    expect(result.cpu.value).toBe(18.2)
    expect(result.netIn.value).toBe(2048)
    expect(result.netOut.value).toBe(512)
    expect(result.diskRead.value).toBeNull()
    expect(result.diskWrite.value).toBe(9)
    expect(result.memory.value).toBe(50)
    expect(result.series.value.memory).toEqual([
      [1, 25],
      [2, 50],
      [3, null],
    ])
    expect(result.series.value.cpu).toHaveLength(3)
  })

  it('makes no request while disabled', async () => {
    serve()
    const { result } = await setup(ref(false))
    await flushPromises()
    await new Promise((r) => setTimeout(r, 20))
    expect(urls).toHaveLength(0)
    expect(result.cpu.value).toBeNull()
    expect(result.series.value).toEqual({})
  })

  it('a remount or window refocus within the interval does not refetch', async () => {
    serve()
    const qc = testQueryClient()
    const first = await setup(ref(true), qc)
    await vi.waitFor(() => expect(urls).toHaveLength(1))
    await flushPromises()
    first.wrapper.unmount()
    await setup(ref(true), qc)
    await flushPromises()
    window.dispatchEvent(new Event('visibilitychange'))
    window.dispatchEvent(new Event('focus'))
    await flushPromises()
    await new Promise((r) => setTimeout(r, 20))
    expect(urls).toHaveLength(1)
    const live = qc.getQueryCache().find({ queryKey: ['server-metrics', 1, 5, 'hour', 'live'] })
    const opts = live!.observers[0]!.options
    expect(opts.refetchOnWindowFocus).toBe(false)
    expect(opts.staleTime).toBe(55_000)
  })

  it('refreshes every 60 s', async () => {
    serve()
    vi.useFakeTimers({ toFake: ['setInterval', 'setTimeout', 'clearInterval', 'clearTimeout'] })
    await setup(ref(true))
    await vi.waitFor(() => expect(urls).toHaveLength(1))
    await vi.advanceTimersByTimeAsync(60_000)
    await vi.waitFor(() => expect(urls).toHaveLength(2))
  })
})

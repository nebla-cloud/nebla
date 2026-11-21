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
    },
  },
}

let urls: URL[] = []
let gate: Promise<void> | null = null
function serve(status = 200) {
  urls = []
  server.use(
    http.get('*/v1/projects/1', () =>
      json(200, { id: 1, name: 'Home lab', description: '', created: '2026-11-01T00:00:00Z' }),
    ),
    http.get('*/v1/servers/:id/metrics', async ({ request }) => {
      urls.push(new URL(request.url))
      if (gate) await gate
      return status === 200
        ? json(200, body)
        : json(status, { error: { code: 'unavailable', message: 'nope' } })
    }),
  )
}

async function mountRows(rows: MaybeRef<{ serverId: number; running: boolean }[]>) {
  const router = testRouter([{ path: '/p/:projectId', component: { render: () => null } }])
  await router.push('/p/1')
  const C = defineComponent({
    render: () =>
      h(
        'div',
        toValue(rows).map((r) => h(RowSparkline, { key: r.serverId, ...r })),
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
    expect(urls[0]!.searchParams.getAll('type').flatMap((t) => t.split(','))).toEqual(['cpu'])
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
      props: { serverId: 5, running: true },
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

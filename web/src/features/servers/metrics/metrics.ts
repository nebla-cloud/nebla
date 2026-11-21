import type uPlot from 'uplot'
import { api } from '@/api/client'
import { unwrap } from '@/api/errors'

export type Range = 'hour' | 'day' | 'week' | 'month'

export const RANGES: { value: Range; label: string }[] = [
  { value: 'hour', label: 'Hour' },
  { value: 'day', label: 'Day' },
  { value: 'week', label: 'Week' },
  { value: 'month', label: 'Month' },
]

export function parseRange(v: unknown): Range {
  return RANGES.some((r) => r.value === v) ? (v as Range) : 'hour'
}

export type TimeSeries = Record<string, [number, number | null][]>

export type MetricType = 'cpu' | 'disk' | 'network'

export async function fetchMetrics(
  serverId: number,
  timeframe: Range,
  type: MetricType[] = ['cpu', 'disk', 'network'],
  signal?: AbortSignal,
) {
  return unwrap(
    await api.GET('/servers/{id}/metrics', {
      signal,
      params: {
        path: { id: serverId },
        query: { type, timeframe },
      },
    }),
  ).metrics
}

// Union of all timestamps, one value array per key, null where a series has no sample.
export function toSeries(
  ts: Record<string, [number, number | null][]>,
  keys: string[],
): uPlot.AlignedData {
  const stamps = new Set<number>()
  for (const k of keys) for (const [t] of ts[k] ?? []) stamps.add(t)
  const xs = [...stamps].sort((a, b) => a - b)
  const cols = keys.map((k) => {
    const byTime = new Map<number, number | null>(ts[k] ?? [])
    return xs.map((t) => byTime.get(t) ?? null)
  })
  return [xs, ...cols] as uPlot.AlignedData
}

export type MetricRow = { t: number; v: (number | null)[] }

// Union of all timestamps (unix seconds), one value per key, null where a series has no sample.
export function toRows(ts: TimeSeries, keys: string[]): MetricRow[] {
  const stamps = new Set<number>()
  for (const k of keys) for (const [t] of ts[k] ?? []) stamps.add(t)
  const byKey = keys.map((k) => new Map<number, number | null>(ts[k] ?? []))
  return [...stamps]
    .sort((a, b) => a - b)
    .map((t) => ({ t, v: byKey.map((m) => m.get(t) ?? null) }))
}

export function nearestIndex(rows: MetricRow[], t: number): number {
  let best = -1
  let dist = Infinity
  rows.forEach((r, i) => {
    const d = Math.abs(r.t - t)
    if (d < dist) {
      best = i
      dist = d
    }
  })
  return best
}

// Rates floor at 1 KB/s so an idle chart isn't all noise.
export function yDomain(rows: MetricRow[], unit: 'percent' | 'rate'): [number, number] {
  if (unit === 'percent') return [0, 100]
  let peak = 0
  for (const r of rows) for (const v of r.v) if (v != null && v > peak) peak = v
  return [0, Math.max(peak, 1024)]
}

export type ChartHeader = { value: string; unit?: string; stats: string[] }

const DASH = '\u2014'

function column(rows: MetricRow[], i: number): number[] {
  return rows.map((r) => r.v[i]).filter((v): v is number => v != null)
}

function latest(rows: MetricRow[], i: number): number | null {
  for (let j = rows.length - 1; j >= 0; j--) {
    const v = rows[j]!.v[i]
    if (v != null) return v
  }
  return null
}

// What the chart header reads: values at idx, or each series' latest value when idx is null.
export function chartHeader(
  rows: MetricRow[],
  labels: string[],
  unit: 'percent' | 'rate',
  idx: number | null,
): ChartHeader {
  const fmt = (v: number | null) =>
    v == null ? DASH : unit === 'percent' ? formatPercent(v) : formatRate(v)
  const at = (i: number) => (idx == null ? latest(rows, i) : (rows[idx]?.v[i] ?? null))
  if (labels.length === 1) {
    const col = column(rows, 0)
    const stats = col.length
      ? [
          `min ${fmt(Math.min(...col))}`,
          `avg ${fmt(col.reduce((a, b) => a + b, 0) / col.length)}`,
          `max ${fmt(Math.max(...col))}`,
        ]
      : [`min ${DASH}`, `avg ${DASH}`, `max ${DASH}`]
    return { value: fmt(at(0)), stats }
  }
  const words = labels.map((l) => l.toLowerCase())
  const peak = (i: number) => {
    const col = column(rows, i)
    return col.length ? Math.max(...col) : null
  }
  return {
    value: fmt(at(0)),
    unit: `${words[0]} / ${fmt(at(1))} ${words[1]}`,
    stats: words.map((w, i) => `peak ${w} ${fmt(peak(i))}`),
  }
}

export function formatRate(bytesPerSec: number): string {
  const units = ['B/s', 'KB/s', 'MB/s', 'GB/s', 'TB/s']
  let v = Math.max(0, bytesPerSec)
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  if (i === 0) return `${Math.round(v)} ${units[0]}`
  const text = v < 10 ? v.toFixed(1) : String(Math.round(v))
  return `${text} ${units[i]}`
}

export function formatPercent(n: number): string {
  return `${Math.round(n)}%`
}

export function formatTime(sec: number, timeZone?: string): string {
  return new Intl.DateTimeFormat('en-GB', {
    day: 'numeric',
    month: 'short',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
    timeZone,
  }).format(sec * 1000)
}

export type Readout = { time: string; values: { label: string; value: string }[] }

// What the hover line shows for the sample under the cursor.
export function readout(
  data: uPlot.AlignedData,
  idx: number | null | undefined,
  labels: string[],
  fmt: (v: number) => string,
  timeZone?: string,
): Readout | null {
  const x = idx == null ? undefined : data[0][idx]
  if (x == null) return null
  return {
    time: formatTime(x, timeZone),
    values: labels.map((label, i) => {
      const v = data[i + 1]?.[idx!]
      return { label, value: v == null ? 'No data' : fmt(v) }
    }),
  }
}

export type Tone = 'busy' | 'run' | 'err'
export const TONE_VAR: Record<Tone, string> = {
  busy: '--nb-busy',
  run: '--nb-run',
  err: '--nb-err',
}

// Light-mode token values, for when the stylesheet isn't there to read (jsdom).
const FALLBACK: Record<string, string> = {
  '--ui-text-muted': '#5B6470',
  '--ui-border': '#DCE1E6',
  '--nb-busy': '#3A86D6',
  '--nb-run': '#1E9E74',
  '--nb-err': '#D9547D',
}

export function chartColors(): { axis: string; grid: string; tone: Record<Tone, string> } {
  const cs = getComputedStyle(document.documentElement)
  const read = (v: string) => cs.getPropertyValue(v).trim() || FALLBACK[v]!
  return {
    axis: read('--ui-text-muted'),
    grid: read('--ui-border'),
    tone: { busy: read(TONE_VAR.busy), run: read(TONE_VAR.run), err: read(TONE_VAR.err) },
  }
}

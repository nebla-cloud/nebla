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
) {
  return unwrap(
    await api.GET('/servers/{id}/metrics', {
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

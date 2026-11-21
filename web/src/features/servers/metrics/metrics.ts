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

// The smallest 1-2-2.5-5 step (in B/s, KB/s, ... units) giving at most five ticks up to peak.
function rateStep(peak: number): number {
  let unit = 1
  while (peak >= unit * 1024 && unit < 1024 ** 4) unit *= 1024
  const raw = peak / unit / 5
  // Below one whole unit, use quarters and halves so steps stay round (256 B/s, not 204.8).
  const mag = 10 ** Math.floor(Math.log10(raw))
  const steps = raw < 1 ? [0.25, 0.5, 1] : [1, 2, 2.5, 5, 10].map((m) => m * mag)
  return steps.find((m) => m >= raw - 1e-9)! * unit
}

// Rates floor at 1 KB/s so an idle chart isn't all noise, and round up to a whole tick.
export function yDomain(rows: MetricRow[], unit: 'percent' | 'rate'): [number, number] {
  if (unit === 'percent') return [0, 100]
  let peak = 0
  for (const r of rows) for (const v of r.v) if (v != null && v > peak) peak = v
  peak = Math.max(peak, 1024)
  const step = rateStep(peak)
  return [0, Math.ceil(peak / step - 1e-9) * step]
}

// Grid lines above zero, up to the domain's top.
export function yTicks(max: number, unit: 'percent' | 'rate'): number[] {
  const step = unit === 'percent' ? 25 : rateStep(max)
  const out: number[] = []
  for (let v = step; v <= max + 1e-6; v += step) out.push(v)
  return out
}

const TIME_STEPS = [60, 120, 300, 600, 900, 1800, 3600, 7200, 10800, 21600, 43200, 86400, 172800, 604800]

// Seconds to add to a UTC time to get the wall-clock time in timeZone.
function tzOffset(t: number, timeZone?: string): number {
  const parts = new Intl.DateTimeFormat('en-GB', {
    timeZone,
    hourCycle: 'h23',
    year: 'numeric',
    month: 'numeric',
    day: 'numeric',
    hour: 'numeric',
    minute: 'numeric',
    second: 'numeric',
  }).formatToParts(t * 1000)
  const n = (type: string) => Number(parts.find((p) => p.type === type)!.value)
  return (Date.UTC(n('year'), n('month') - 1, n('day'), n('hour'), n('minute'), n('second')) - t * 1000) / 1000
}

// At most five round wall-clock times (quarter hours, midnights, ...) between t0 and t1.
export function timeTicks(t0: number, t1: number, timeZone?: string): number[] {
  const span = t1 - t0
  if (span <= 0) return []
  const step = TIME_STEPS.find((s) => span / s <= 5) ?? TIME_STEPS.at(-1)!
  const out: number[] = []
  // A tick within a quarter step of either end would have its label cut off at the edge.
  const pad = step / 4
  // Step through wall-clock time and convert each tick back, so a clock change mid-range
  // doesn't shift later ticks off the hour or midnight.
  for (let wall = Math.ceil((t0 + tzOffset(t0, timeZone)) / step) * step; ; wall += step) {
    let t = wall - tzOffset(wall - tzOffset(t0, timeZone), timeZone)
    t = wall - tzOffset(t, timeZone)
    if (t > t1) break
    if (t - t0 >= pad && t1 - t >= pad) out.push(t)
  }
  return out
}

// Time of day for spans up to two days, otherwise the date.
export function formatTick(t: number, span: number, timeZone?: string): string {
  const opts: Intl.DateTimeFormatOptions =
    span <= 2 * 86400
      ? { hour: '2-digit', minute: '2-digit', hour12: false }
      : { day: 'numeric', month: 'short' }
  return new Intl.DateTimeFormat('en-GB', { ...opts, timeZone }).format(t * 1000)
}

// Runs of present samples for series i, as [start, end] fractions of the time range.
export function segments(rows: MetricRow[], i: number): [number, number][] {
  if (rows.length < 2) return []
  const t0 = rows[0]!.t
  const span = rows.at(-1)!.t - t0 || 1
  const out: [number, number][] = []
  let start: number | null = null
  rows.forEach((r, j) => {
    const present = r.v[i] != null
    if (present && start == null) start = r.t
    const runEnds = present && (j === rows.length - 1 || rows[j + 1]!.v[i] == null)
    if (runEnds && start != null) {
      if (r.t > start) out.push([(start - t0) / span, (r.t - t0) / span])
      start = null
    }
  })
  return out
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

// Axis labels share the top tick's unit, so a scale reads 0.25, 0.5 ... 1.25 MB/s.
export function formatAxisRate(v: number, max: number): string {
  const units = ['B/s', 'KB/s', 'MB/s', 'GB/s', 'TB/s']
  let i = 0
  while (max >= 1024 ** (i + 1) && i < units.length - 1) i++
  return `${+(v / 1024 ** i).toFixed(2)} ${units[i]}`
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

export type Tone = 'busy' | 'run' | 'err'
export const TONE_VAR: Record<Tone, string> = {
  busy: '--nb-busy',
  run: '--nb-run',
  err: '--nb-err',
}

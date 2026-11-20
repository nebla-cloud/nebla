import type uPlot from 'uplot'

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

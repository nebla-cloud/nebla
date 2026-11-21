import { validateLabels } from '@/features/servers/labels'

// Parses "k=v, k2=v2" into labels, or the first validation message.
export function parseLabels(s: string): { labels: Record<string, string> } | { error: string } {
  const rows = s
    .split(',')
    .map((p) => p.trim())
    .filter(Boolean)
    .map((p) => {
      const i = p.indexOf('=')
      return i === -1 ? { key: p, value: '' } : { key: p.slice(0, i), value: p.slice(i + 1) }
    })
  const v = validateLabels(rows)
  return v.ok ? { labels: v.labels } : { error: Object.values(v.errors)[0]! }
}

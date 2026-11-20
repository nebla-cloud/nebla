export type LabelRow = { key: string; value: string }

const MAX_LABELS = 64
const KEY_RE = /^[A-Za-z0-9]([A-Za-z0-9._/-]{0,61}[A-Za-z0-9])?$/
const VALUE_RE = /^([A-Za-z0-9]([A-Za-z0-9._-]{0,61}[A-Za-z0-9])?)?$/

export function formatLabel(k: string, v: string): string {
  return v === '' ? k : `${k}=${v}`
}

export function validateLabels(
  rows: LabelRow[],
): { ok: true; labels: Record<string, string> } | { ok: false; errors: Record<number, string> } {
  const errors: Record<number, string> = {}
  const labels: Record<string, string> = {}
  // Fully empty rows are ignored, so a spare blank row doesn't block a save.
  const filled = rows.filter((r) => r.key.trim() !== '' || r.value.trim() !== '').length
  if (filled > MAX_LABELS) {
    errors[MAX_LABELS] = `Use at most ${MAX_LABELS} labels.`
  }
  const seen = new Set<string>()
  rows.forEach((r, i) => {
    const key = r.key.trim()
    const value = r.value.trim()
    if (errors[i] || (key === '' && value === '')) return
    if (!KEY_RE.test(key)) {
      errors[i] =
        'Keys use letters, numbers, dots, dashes, underscores and slashes, up to 63 characters, starting and ending with a letter or number.'
    } else if (!VALUE_RE.test(value)) {
      errors[i] =
        'Values use letters, numbers, dots, dashes and underscores, up to 63 characters, starting and ending with a letter or number.'
    } else if (seen.has(key)) {
      errors[i] = 'This key is already used.'
    } else {
      seen.add(key)
      labels[key] = value
    }
  })
  return Object.keys(errors).length ? { ok: false, errors } : { ok: true, labels }
}

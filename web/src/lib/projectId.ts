export function parseProjectId(v: unknown): number | null {
  if (typeof v !== 'string' || !/^[1-9]\d*$/.test(v)) return null
  const n = Number(v)
  return Number.isSafeInteger(n) ? n : null
}

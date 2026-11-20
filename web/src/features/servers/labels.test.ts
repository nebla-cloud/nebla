import { describe, expect, it } from 'vitest'
import { formatLabel, validateLabels } from './labels'

describe('validateLabels', () => {
  it('accepts valid', () => {
    const r = validateLabels([
      { key: 'env', value: 'prod' },
      { key: 'team/web', value: 'a.b-c' },
      { key: 'backup', value: '' },
    ])
    expect(r).toEqual({ ok: true, labels: { env: 'prod', 'team/web': 'a.b-c', backup: '' } })
  })

  it('rejects bad keys', () => {
    const r = validateLabels([
      { key: 'bad key', value: '' },
      { key: '-x', value: '' },
      { key: 'a'.repeat(64), value: '' },
      { key: 'dup', value: '1' },
      { key: 'dup', value: '2' },
    ])
    expect(r.ok).toBe(false)
    if (!r.ok) expect(Object.keys(r.errors).map(Number)).toEqual([0, 1, 2, 4])
  })

  it('rejects bad values', () => {
    expect(validateLabels([{ key: 'a', value: '-x' }]).ok).toBe(false)
  })

  it('rejects 65 labels', () => {
    const rows = Array.from({ length: 65 }, (_, i) => ({ key: `k${i}`, value: '' }))
    expect(validateLabels(rows).ok).toBe(false)
    expect(validateLabels(rows.slice(0, 64)).ok).toBe(true)
  })

  it('formatLabel empty value', () => {
    expect(formatLabel('backup', '')).toBe('backup')
    expect(formatLabel('env', 'prod')).toBe('env=prod')
  })

  it('skips fully empty rows', () => {
    const r = validateLabels([
      { key: '', value: '' },
      { key: 'env', value: 'prod' },
      { key: '  ', value: ' ' },
    ])
    expect(r).toEqual({ ok: true, labels: { env: 'prod' } })
  })

  it('a value without a key is still an error', () => {
    const r = validateLabels([{ key: '', value: 'prod' }])
    expect(r.ok).toBe(false)
  })
})

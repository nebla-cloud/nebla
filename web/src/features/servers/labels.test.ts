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
